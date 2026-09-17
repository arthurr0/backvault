package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/store"
)

type jobRequest struct {
	Slug                    string           `json:"slug"`
	Name                    string           `json:"name"`
	Description             string           `json:"description"`
	SourceID                string           `json:"sourceId"`
	DestinationIDs          []string         `json:"destinationIds"`
	Schedule                string           `json:"schedule"`
	Timezone                string           `json:"timezone"`
	Enabled                 *bool            `json:"enabled"`
	Compression             core.Compression `json:"compression"`
	CompressionLevel        int              `json:"compressionLevel"`
	Encryption              core.Encryption  `json:"encryption"`
	EncryptionPassphrase    string           `json:"encryptionPassphrase"`
	Retention               core.Retention   `json:"retention"`
	NotificationChannelIDs  []string         `json:"notificationChannelIds"`
	NotifyOn                []string         `json:"notifyOn"`
	TimeoutMinutes          int              `json:"timeoutMinutes"`
	Retries                 int              `json:"retries"`
	RetryDelaySeconds       int              `json:"retryDelaySeconds"`
	PreCommand              string           `json:"preCommand"`
	PostCommand             string           `json:"postCommand"`
	VerifyAfterUpload       bool             `json:"verifyAfterUpload"`
	ExpectedIntervalMinutes int              `json:"expectedIntervalMinutes"`
	Tags                    []string         `json:"tags"`
}

func (s *Server) jobRef(r *http.Request) (core.Job, error) {
	return s.store.Jobs.GetByIDOrSlug(r.Context(), chi.URLParam(r, "id"))
}

func (s *Server) refreshJobSchedule(ctx context.Context, jobID string) {
	job, err := s.store.Jobs.Get(ctx, jobID)
	if err != nil {
		return
	}
	settings, err := s.store.Settings.Get(ctx)
	if err != nil {
		return
	}
	next, err := engine.NextRun(job, settings.DefaultTimezone, time.Now().UTC())
	if err != nil {
		next = nil
	}
	if err := s.store.JobStates.SetNextRun(ctx, job.ID, next); err != nil {
		s.log.Error("could not store next run time", "job", job.Slug, "error", err)
	}
	s.engine.ReloadSchedule()
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	filter := store.JobFilter{
		SourceID:      strings.TrimSpace(q.Get("sourceId")),
		DestinationID: strings.TrimSpace(q.Get("destinationId")),
		Tag:           strings.TrimSpace(q.Get("tag")),
		Q:             strings.TrimSpace(q.Get("q")),
		Page:          page,
	}
	if v := strings.TrimSpace(q.Get("enabled")); v != "" {
		enabled := v == "1" || strings.EqualFold(v, "true")
		filter.Enabled = &enabled
	}
	items, total, err := s.store.Jobs.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, maskJobs(items), total)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskJob(job))
}

func (s *Server) validateJob(ctx context.Context, req jobRequest, existing *core.Job) (core.Job, error) {
	ve := newValidationError("invalid job")
	job := core.Job{}
	if existing != nil {
		job = *existing
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		ve.field("name", "required")
	}
	job.Name = name
	job.Description = req.Description

	if strings.TrimSpace(req.SourceID) == "" {
		ve.field("sourceId", "required")
	} else {
		src, err := s.store.Sources.Get(ctx, req.SourceID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				ve.field("sourceId", "source does not exist")
			} else {
				return job, err
			}
		} else {
			job.SourceID = src.ID
		}
	}

	if len(req.DestinationIDs) == 0 {
		ve.field("destinationIds", "at least one destination is required")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, id := range req.DestinationIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.store.Destinations.Get(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				ve.field("destinationIds", "destination "+id+" does not exist")
				continue
			}
			return job, err
		}
		ids = append(ids, id)
	}
	job.DestinationIDs = ids

	schedule := strings.TrimSpace(req.Schedule)
	if schedule != "" {
		if _, err := engine.ParseSchedule(schedule); err != nil {
			ve.field("schedule", err.Error())
		}
	}
	job.Schedule = schedule

	timezone := strings.TrimSpace(req.Timezone)
	if timezone != "" {
		if _, err := engine.LoadLocation(timezone); err != nil {
			ve.field("timezone", err.Error())
		}
	}
	job.Timezone = timezone

	switch req.Compression {
	case "", core.CompressionNone:
		job.Compression = core.CompressionNone
	case core.CompressionGzip, core.CompressionZstd:
		job.Compression = req.Compression
	default:
		ve.field("compression", "must be none, gzip or zstd")
	}
	if req.CompressionLevel < 0 || req.CompressionLevel > 19 {
		ve.field("compressionLevel", "must be between 0 and 19")
	}
	job.CompressionLevel = req.CompressionLevel

	switch req.Encryption {
	case "", core.EncryptionNone:
		job.Encryption = core.EncryptionNone
	case core.EncryptionAge:
		job.Encryption = core.EncryptionAge
	default:
		ve.field("encryption", "must be none or age")
	}

	passphrase := req.EncryptionPassphrase
	switch {
	case passphrase == core.SecretMask:
		if existing == nil {
			ve.field("encryptionPassphrase", "required")
		}
	case passphrase == "":
		job.EncryptionPassphrase = ""
	default:
		encrypted, err := s.secrets.Encrypt(passphrase)
		if err != nil {
			return job, err
		}
		job.EncryptionPassphrase = encrypted
	}
	if job.Encryption == core.EncryptionAge && job.EncryptionPassphrase == "" {
		ve.field("encryptionPassphrase", "required when encryption is age")
	}
	if job.Encryption == core.EncryptionNone {
		job.EncryptionPassphrase = ""
	}

	retention := req.Retention
	if retention.KeepLast < 0 || retention.KeepHourly < 0 || retention.KeepDaily < 0 ||
		retention.KeepWeekly < 0 || retention.KeepMonthly < 0 || retention.KeepYearly < 0 || retention.MaxAgeDays < 0 {
		ve.field("retention", "values must not be negative")
	}
	job.Retention = retention

	for _, ev := range req.NotifyOn {
		if !containsString(core.AllEvents, ev) {
			ve.field("notifyOn", "unknown event: "+ev)
		}
	}
	job.NotifyOn = req.NotifyOn

	for _, id := range req.NotificationChannelIDs {
		if _, err := s.store.Channels.Get(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				ve.field("notificationChannelIds", "channel "+id+" does not exist")
				continue
			}
			return job, err
		}
	}
	job.NotificationChannelIDs = req.NotificationChannelIDs

	if req.TimeoutMinutes < 0 {
		ve.field("timeoutMinutes", "must not be negative")
	}
	if req.Retries < 0 || req.Retries > 20 {
		ve.field("retries", "must be between 0 and 20")
	}
	if req.RetryDelaySeconds < 0 {
		ve.field("retryDelaySeconds", "must not be negative")
	}
	if req.ExpectedIntervalMinutes < 0 {
		ve.field("expectedIntervalMinutes", "must not be negative")
	}
	job.TimeoutMinutes = req.TimeoutMinutes
	job.Retries = req.Retries
	job.RetryDelaySeconds = req.RetryDelaySeconds
	job.ExpectedIntervalMinutes = req.ExpectedIntervalMinutes
	job.PreCommand = req.PreCommand
	job.PostCommand = req.PostCommand
	job.VerifyAfterUpload = req.VerifyAfterUpload
	job.Tags = req.Tags
	job.Enabled = true
	if existing != nil {
		job.Enabled = existing.Enabled
	}
	if req.Enabled != nil {
		job.Enabled = *req.Enabled
	}

	exceptID := ""
	if existing != nil {
		exceptID = existing.ID
	}
	requested := strings.TrimSpace(req.Slug)
	if requested != "" {
		if !validSlug(requested) {
			ve.field("slug", "must contain only lowercase letters, digits and dashes")
		} else {
			taken, err := s.store.Jobs.SlugExists(ctx, requested, exceptID)
			if err != nil {
				return job, err
			}
			if taken {
				ve.field("slug", "already in use")
			}
			job.Slug = requested
		}
	} else if existing == nil || existing.Slug == "" {
		slug, err := uniqueSlug(ctx, slugify(name), exceptID, s.store.Jobs.SlugExists)
		if err != nil {
			return job, err
		}
		job.Slug = slug
	}

	if !ve.empty() {
		return job, ve
	}
	return job, nil
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req jobRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	job, err := s.validateJob(r.Context(), req, nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	created, err := s.store.Jobs.Create(r.Context(), job)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.refreshJobSchedule(r.Context(), created.ID)
	s.audit(r, nil, "job.create", "job", created.ID, created.Name, map[string]any{"slug": created.Slug})
	full, err := s.store.Jobs.Get(r.Context(), created.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, maskJob(full))
}

func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req jobRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	job, err := s.validateJob(ctx, req, &existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.store.Jobs.Update(ctx, job); err != nil {
		s.fail(w, r, err)
		return
	}
	s.refreshJobSchedule(ctx, job.ID)
	s.audit(r, nil, "job.update", "job", job.ID, job.Name, map[string]any{"slug": job.Slug})
	full, err := s.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskJob(full))
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	deleteArtifacts := queryBool(r, "deleteArtifacts")
	if err := s.store.Jobs.Delete(ctx, job.ID, deleteArtifacts); err != nil {
		s.fail(w, r, err)
		return
	}
	s.engine.ReloadSchedule()
	s.audit(r, nil, "job.delete", "job", job.ID, job.Name, map[string]any{"deleteArtifacts": deleteArtifacts})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setJobEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	ctx := r.Context()
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.Jobs.SetEnabled(ctx, job.ID, enabled); err != nil {
		s.fail(w, r, err)
		return
	}
	s.refreshJobSchedule(ctx, job.ID)
	action := "job.disable"
	if enabled {
		action = "job.enable"
	}
	s.audit(r, nil, action, "job", job.ID, job.Name, nil)
	updated, err := s.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskJob(updated))
}

func (s *Server) handleEnableJob(w http.ResponseWriter, r *http.Request) {
	s.setJobEnabled(w, r, true)
}

func (s *Server) handleDisableJob(w http.ResponseWriter, r *http.Request) {
	s.setJobEnabled(w, r, false)
}

func (s *Server) handleRunJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.requireJobAccess(r, job.Slug); err != nil {
		s.writeError(w, r, http.StatusForbidden, codeForbidden, err.Error())
		return
	}
	run, err := s.engine.EnqueueBackup(r.Context(), job, core.TriggerAPI, actorLabel(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "job.run", "job", job.ID, job.Name, map[string]any{"runId": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (s *Server) handlePruneJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	run, err := s.engine.EnqueuePrune(r.Context(), job, actorLabel(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "job.prune", "job", job.ID, job.Name, map[string]any{"runId": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (s *Server) handleDuplicateJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	copySlug, err := uniqueSlug(ctx, job.Slug+"-copy", "", s.store.Jobs.SlugExists)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	clone := job
	clone.ID = ""
	clone.Slug = copySlug
	clone.Name = job.Name + " (copy)"
	clone.Enabled = false
	created, err := s.store.Jobs.Create(ctx, clone)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.refreshJobSchedule(ctx, created.ID)
	s.audit(r, nil, "job.duplicate", "job", created.ID, created.Name, map[string]any{"from": job.Slug})
	full, err := s.store.Jobs.Get(ctx, created.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, maskJob(full))
}

func (s *Server) handleJobRuns(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.RunFilter{JobID: job.ID, Page: page}
	filter.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	filter.Kind = strings.TrimSpace(r.URL.Query().Get("kind"))
	items, total, err := s.store.Runs.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, total)
}

func (s *Server) handleJobArtifacts(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.ArtifactFilter{JobID: job.ID, Page: page}
	filter.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	filter.DestinationID = strings.TrimSpace(r.URL.Query().Get("destination"))
	items, total, err := s.store.Artifacts.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, total)
}

func (s *Server) handleSchedulePreview(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobRef(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	count := 5
	if v := strings.TrimSpace(r.URL.Query().Get("count")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			count = n
		}
	}
	settings, err := s.store.Settings.Get(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items, err := engine.NextRuns(job, settings.DefaultTimezone, time.Now().UTC(), count)
	if err != nil {
		s.fail(w, r, newValidationError(err.Error()).field("schedule", err.Error()))
		return
	}
	if items == nil {
		items = []time.Time{}
	}
	writeList(w, items, len(items))
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}
