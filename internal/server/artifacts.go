package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/store"
)

type restoreRequest struct {
	Mode           string      `json:"mode"`
	TargetPath     string      `json:"targetPath"`
	Extract        bool        `json:"extract"`
	TargetSourceID string      `json:"targetSourceId"`
	Passphrase     string      `json:"passphrase"`
	Params         core.Config `json:"params"`
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	since, err := parseTimeParam(r, "since")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	until, err := parseTimeParam(r, "until")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := r.URL.Query()
	filter := store.ArtifactFilter{
		DestinationID: strings.TrimSpace(q.Get("destination")),
		Status:        strings.TrimSpace(q.Get("status")),
		RunID:         strings.TrimSpace(q.Get("run")),
		Q:             strings.TrimSpace(q.Get("q")),
		Since:         since,
		Until:         until,
		Page:          page,
	}
	if job := strings.TrimSpace(q.Get("job")); job != "" {
		resolved, err := s.store.Jobs.GetByIDOrSlug(r.Context(), job)
		if err == nil {
			filter.JobID = resolved.ID
		} else {
			filter.JobSlug = job
		}
	}
	items, total, err := s.store.Artifacts.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, total)
}

func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	artifact, err := s.store.Artifacts.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, artifact)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	artifact, err := s.store.Artifacts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.store.Destinations.Get(ctx, artifact.DestinationID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.fail(w, r, err)
		return
	}
	if err == nil {
		client, err := s.engine.OpenDestination(ctx, d, s.log)
		if err != nil {
			s.writeError(w, r, http.StatusBadGateway, codeInternal, err.Error())
			return
		}
		defer client.Close()
		if err := client.Delete(ctx, artifact.Path); err != nil {
			s.writeError(w, r, http.StatusBadGateway, codeInternal, "could not delete the object: "+err.Error())
			return
		}
	}
	if err := s.store.Artifacts.SetStatus(ctx, artifact.ID, core.ArtifactDeleted, time.Now().UTC()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "artifact.delete", "artifact", artifact.ID, artifact.Filename, map[string]any{"destination": artifact.DestinationName})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	artifact, err := s.store.Artifacts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if artifact.Status != core.ArtifactPresent {
		s.writeError(w, r, http.StatusConflict, codeConflict, "artifact status is "+string(artifact.Status))
		return
	}
	opts := engine.DownloadOptions{
		Raw:        queryBool(r, "raw"),
		Passphrase: r.URL.Query().Get("passphrase"),
	}
	reader, name, err := s.engine.OpenArtifact(ctx, artifact, opts, s.log)
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, codeInternal, err.Error())
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(name)+"\"")
	w.Header().Set("X-Backvault-Sha256", artifact.SHA256)
	if opts.Raw || (artifact.Compression == core.CompressionNone && artifact.Encryption == core.EncryptionNone) {
		w.Header().Set("Content-Length", strconv.FormatInt(artifact.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		s.log.Warn("artifact download interrupted", "artifact", artifact.ID, "error", err)
	}
}

func (s *Server) handleRestoreArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	artifact, err := s.store.Artifacts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req restoreRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	mode := engine.RestoreMode(strings.TrimSpace(req.Mode))
	switch mode {
	case engine.RestoreToPath:
		if strings.TrimSpace(req.TargetPath) == "" {
			s.fail(w, r, newValidationError("invalid restore request").field("targetPath", "required"))
			return
		}
	case engine.RestoreToSource:
		if req.TargetSourceID != "" {
			if _, err := s.store.Sources.Get(ctx, req.TargetSourceID); err != nil {
				s.fail(w, r, newValidationError("invalid restore request").field("targetSourceId", "source does not exist"))
				return
			}
		}
	default:
		s.fail(w, r, newValidationError("invalid restore request").field("mode", "must be path or source"))
		return
	}
	run, err := s.engine.EnqueueRestore(ctx, engine.RestoreRequest{
		Artifact:       artifact,
		Mode:           mode,
		TargetPath:     req.TargetPath,
		Extract:        req.Extract,
		TargetSourceID: req.TargetSourceID,
		Passphrase:     req.Passphrase,
		Params:         req.Params,
		CreatedBy:      actorLabel(r),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "artifact.restore", "artifact", artifact.ID, artifact.Filename, map[string]any{"mode": string(mode), "runId": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (s *Server) handleVerifyArtifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	artifact, err := s.store.Artifacts.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	run, err := s.engine.EnqueueVerify(ctx, artifact, actorLabel(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "artifact.verify", "artifact", artifact.ID, artifact.Filename, map[string]any{"runId": run.ID})
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	if name == "" {
		return "artifact"
	}
	return name
}
