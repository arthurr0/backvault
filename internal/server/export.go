package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type exportSource struct {
	Name        string         `yaml:"name" json:"name"`
	Kind        string         `yaml:"kind" json:"kind"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Config      map[string]any `yaml:"config" json:"config"`
	Tags        []string       `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type exportDestination struct {
	Name        string         `yaml:"name" json:"name"`
	Kind        string         `yaml:"kind" json:"kind"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Config      map[string]any `yaml:"config" json:"config"`
	Tags        []string       `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type exportJob struct {
	Slug                    string           `yaml:"slug" json:"slug"`
	Name                    string           `yaml:"name" json:"name"`
	Description             string           `yaml:"description,omitempty" json:"description,omitempty"`
	Source                  string           `yaml:"source" json:"source"`
	Destinations            []string         `yaml:"destinations" json:"destinations"`
	Schedule                string           `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	Timezone                string           `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	Enabled                 bool             `yaml:"enabled" json:"enabled"`
	Compression             core.Compression `yaml:"compression,omitempty" json:"compression,omitempty"`
	CompressionLevel        int              `yaml:"compressionLevel,omitempty" json:"compressionLevel,omitempty"`
	Encryption              core.Encryption  `yaml:"encryption,omitempty" json:"encryption,omitempty"`
	EncryptionPassphrase    string           `yaml:"encryptionPassphrase,omitempty" json:"encryptionPassphrase,omitempty"`
	Retention               core.Retention   `yaml:"retention" json:"retention"`
	NotificationChannels    []string         `yaml:"notificationChannels,omitempty" json:"notificationChannels,omitempty"`
	NotifyOn                []string         `yaml:"notifyOn,omitempty" json:"notifyOn,omitempty"`
	TimeoutMinutes          int              `yaml:"timeoutMinutes,omitempty" json:"timeoutMinutes,omitempty"`
	Retries                 int              `yaml:"retries,omitempty" json:"retries,omitempty"`
	RetryDelaySeconds       int              `yaml:"retryDelaySeconds,omitempty" json:"retryDelaySeconds,omitempty"`
	PreCommand              string           `yaml:"preCommand,omitempty" json:"preCommand,omitempty"`
	PostCommand             string           `yaml:"postCommand,omitempty" json:"postCommand,omitempty"`
	VerifyAfterUpload       bool             `yaml:"verifyAfterUpload,omitempty" json:"verifyAfterUpload,omitempty"`
	ExpectedIntervalMinutes int              `yaml:"expectedIntervalMinutes,omitempty" json:"expectedIntervalMinutes,omitempty"`
	Tags                    []string         `yaml:"tags,omitempty" json:"tags,omitempty"`
}

type exportChannel struct {
	Name    string         `yaml:"name" json:"name"`
	Kind    string         `yaml:"kind" json:"kind"`
	Enabled bool           `yaml:"enabled" json:"enabled"`
	Events  []string       `yaml:"events,omitempty" json:"events,omitempty"`
	Config  map[string]any `yaml:"config" json:"config"`
}

type exportDocument struct {
	Version      int                 `yaml:"version" json:"version"`
	ExportedAt   time.Time           `yaml:"exportedAt" json:"exportedAt"`
	Sources      []exportSource      `yaml:"sources" json:"sources"`
	Destinations []exportDestination `yaml:"destinations" json:"destinations"`
	Jobs         []exportJob         `yaml:"jobs" json:"jobs"`
	Channels     []exportChannel     `yaml:"channels" json:"channels"`
}

type importChange struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
}

func toMap(cfg core.Config) map[string]any {
	out := map[string]any{}
	for k, v := range cfg {
		out[k] = v
	}
	return out
}

func (s *Server) buildExport(r *http.Request, includeSecrets bool) (exportDocument, error) {
	ctx := r.Context()
	doc := exportDocument{Version: 1, ExportedAt: time.Now().UTC()}

	sources, _, err := s.store.Sources.List(ctx, store.SourceFilter{Page: store.Page{Limit: store.MaxLimit}})
	if err != nil {
		return doc, err
	}
	sourceNames := map[string]string{}
	for _, src := range sources {
		sourceNames[src.ID] = src.Name
		cfg := src.Config
		if includeSecrets {
			if spec, _, err := sourceSpec(src.Kind); err == nil {
				cfg = s.secrets.DecryptConfigLenient(cfg, spec.SecretFields())
			}
		} else {
			cfg = maskSource(src).Config
		}
		doc.Sources = append(doc.Sources, exportSource{Name: src.Name, Kind: src.Kind, Description: src.Description, Config: toMap(cfg), Tags: src.Tags})
	}

	destinations, _, err := s.store.Destinations.List(ctx, store.DestinationFilter{Page: store.Page{Limit: store.MaxLimit}})
	if err != nil {
		return doc, err
	}
	destNames := map[string]string{}
	for _, d := range destinations {
		destNames[d.ID] = d.Name
		cfg := d.Config
		if includeSecrets {
			if spec, _, err := destinationSpec(d.Kind); err == nil {
				cfg = s.secrets.DecryptConfigLenient(cfg, spec.SecretFields())
			}
		} else {
			cfg = maskDestination(d).Config
		}
		doc.Destinations = append(doc.Destinations, exportDestination{Name: d.Name, Kind: d.Kind, Description: d.Description, Config: toMap(cfg), Tags: d.Tags})
	}

	channels, err := s.store.Channels.List(ctx)
	if err != nil {
		return doc, err
	}
	channelNames := map[string]string{}
	for _, c := range channels {
		channelNames[c.ID] = c.Name
		cfg := c.Config
		if includeSecrets {
			if spec, _, err := notifierSpec(c.Kind); err == nil {
				cfg = s.secrets.DecryptConfigLenient(cfg, spec.SecretFields())
			}
		} else {
			cfg = maskChannel(c).Config
		}
		doc.Channels = append(doc.Channels, exportChannel{Name: c.Name, Kind: c.Kind, Enabled: c.Enabled, Events: c.Events, Config: toMap(cfg)})
	}

	jobs, err := s.store.Jobs.All(ctx)
	if err != nil {
		return doc, err
	}
	for _, j := range jobs {
		item := exportJob{
			Slug: j.Slug, Name: j.Name, Description: j.Description,
			Source: sourceNames[j.SourceID], Enabled: j.Enabled,
			Schedule: j.Schedule, Timezone: j.Timezone,
			Compression: j.Compression, CompressionLevel: j.CompressionLevel,
			Encryption: j.Encryption, Retention: j.Retention,
			NotifyOn: j.NotifyOn, TimeoutMinutes: j.TimeoutMinutes,
			Retries: j.Retries, RetryDelaySeconds: j.RetryDelaySeconds,
			PreCommand: j.PreCommand, PostCommand: j.PostCommand,
			VerifyAfterUpload: j.VerifyAfterUpload, ExpectedIntervalMinutes: j.ExpectedIntervalMinutes,
			Tags: j.Tags,
		}
		for _, id := range j.DestinationIDs {
			if name, ok := destNames[id]; ok {
				item.Destinations = append(item.Destinations, name)
			}
		}
		for _, id := range j.NotificationChannelIDs {
			if name, ok := channelNames[id]; ok {
				item.NotificationChannels = append(item.NotificationChannels, name)
			}
		}
		if j.EncryptionPassphrase != "" {
			if includeSecrets {
				plain, err := s.secrets.Decrypt(j.EncryptionPassphrase)
				if err != nil {
					return doc, err
				}
				item.EncryptionPassphrase = plain
			} else {
				item.EncryptionPassphrase = core.SecretMask
			}
		}
		doc.Jobs = append(doc.Jobs, item)
	}
	return doc, nil
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	includeSecrets := queryBool(r, "includeSecrets")
	doc, err := s.buildExport(r, includeSecrets)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		s.fail(w, r, fmt.Errorf("encode export: %w", err))
		return
	}
	action := "export"
	if includeSecrets {
		action = "export.secrets"
	}
	s.audit(r, nil, action, "export", "", "", map[string]any{
		"sources": len(doc.Sources), "destinations": len(doc.Destinations),
		"jobs": len(doc.Jobs), "channels": len(doc.Channels),
	})
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"backvault-export.yaml\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		s.fail(w, r, newValidationError("could not read the request body"))
		return
	}
	var doc exportDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		s.fail(w, r, newValidationError(fmt.Sprintf("invalid YAML: %v", err)))
		return
	}
	dryRun := queryBool(r, "dryRun")
	changes := []importChange{}

	sourceIDs := map[string]string{}
	existingSources, _, err := s.store.Sources.List(ctx, store.SourceFilter{Page: store.Page{Limit: store.MaxLimit}})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, src := range existingSources {
		sourceIDs[src.Name] = src.ID
	}
	for _, item := range doc.Sources {
		spec, driver, err := sourceSpec(item.Kind)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		existingID, exists := sourceIDs[item.Name]
		var previous core.Config
		if exists {
			current, err := s.store.Sources.Get(ctx, existingID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			previous = current.Config
		}
		prepared, err := s.prepareConfig(spec, core.Config(item.Config), previous, driver.Validate)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		action := "create"
		if exists {
			action = "update"
		}
		changes = append(changes, importChange{Kind: "source", Name: item.Name, Action: action})
		if dryRun {
			continue
		}
		record := core.Source{ID: existingID, Name: item.Name, Kind: item.Kind, Description: item.Description, Config: prepared.Encrypted, Tags: item.Tags}
		var saved core.Source
		if exists {
			saved, err = s.store.Sources.Update(ctx, record)
		} else {
			saved, err = s.store.Sources.Create(ctx, record)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		sourceIDs[item.Name] = saved.ID
	}

	destIDs := map[string]string{}
	existingDests, _, err := s.store.Destinations.List(ctx, store.DestinationFilter{Page: store.Page{Limit: store.MaxLimit}})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, d := range existingDests {
		destIDs[d.Name] = d.ID
	}
	for _, item := range doc.Destinations {
		spec, driver, err := destinationSpec(item.Kind)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		existingID, exists := destIDs[item.Name]
		var previous core.Config
		if exists {
			current, err := s.store.Destinations.Get(ctx, existingID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			previous = current.Config
		}
		prepared, err := s.prepareConfig(spec, core.Config(item.Config), previous, driver.Validate)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		action := "create"
		if exists {
			action = "update"
		}
		changes = append(changes, importChange{Kind: "destination", Name: item.Name, Action: action})
		if dryRun {
			continue
		}
		record := core.Destination{ID: existingID, Name: item.Name, Kind: item.Kind, Description: item.Description, Config: prepared.Encrypted, Tags: item.Tags}
		var saved core.Destination
		if exists {
			saved, err = s.store.Destinations.Update(ctx, record)
		} else {
			saved, err = s.store.Destinations.Create(ctx, record)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		destIDs[item.Name] = saved.ID
	}

	channelIDs := map[string]string{}
	existingChannels, err := s.store.Channels.List(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, c := range existingChannels {
		channelIDs[c.Name] = c.ID
	}
	for _, item := range doc.Channels {
		spec, driver, err := notifierSpec(item.Kind)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		existingID, exists := channelIDs[item.Name]
		var previous core.Config
		if exists {
			current, err := s.store.Channels.Get(ctx, existingID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			previous = current.Config
		}
		prepared, err := s.prepareConfig(spec, core.Config(item.Config), previous, driver.Validate)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		action := "create"
		if exists {
			action = "update"
		}
		changes = append(changes, importChange{Kind: "channel", Name: item.Name, Action: action})
		if dryRun {
			continue
		}
		record := core.NotificationChannel{ID: existingID, Name: item.Name, Kind: item.Kind, Config: prepared.Encrypted, Enabled: item.Enabled, Events: item.Events}
		var saved core.NotificationChannel
		if exists {
			saved, err = s.store.Channels.Update(ctx, record)
		} else {
			saved, err = s.store.Channels.Create(ctx, record)
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		channelIDs[item.Name] = saved.ID
	}

	for _, item := range doc.Jobs {
		slug := item.Slug
		if slug == "" {
			slug = slugify(item.Name)
		}
		existing, err := s.store.Jobs.GetBySlug(ctx, slug)
		exists := err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.fail(w, r, err)
			return
		}
		note := ""
		req := jobRequest{
			Slug: slug, Name: item.Name, Description: item.Description,
			SourceID: sourceIDs[item.Source], Schedule: item.Schedule, Timezone: item.Timezone,
			Enabled: &item.Enabled, Compression: item.Compression, CompressionLevel: item.CompressionLevel,
			Encryption: item.Encryption, EncryptionPassphrase: item.EncryptionPassphrase,
			Retention: item.Retention, NotifyOn: item.NotifyOn,
			TimeoutMinutes: item.TimeoutMinutes, Retries: item.Retries, RetryDelaySeconds: item.RetryDelaySeconds,
			PreCommand: item.PreCommand, PostCommand: item.PostCommand,
			VerifyAfterUpload: item.VerifyAfterUpload, ExpectedIntervalMinutes: item.ExpectedIntervalMinutes,
			Tags: item.Tags,
		}
		for _, name := range item.Destinations {
			if id, ok := destIDs[name]; ok {
				req.DestinationIDs = append(req.DestinationIDs, id)
			} else {
				note = "unknown destination: " + name
			}
		}
		for _, name := range item.NotificationChannels {
			if id, ok := channelIDs[name]; ok {
				req.NotificationChannelIDs = append(req.NotificationChannelIDs, id)
			}
		}
		if req.SourceID == "" {
			note = "unknown source: " + item.Source
		}
		if dryRun {
			action := "create"
			if exists {
				action = "update"
			}
			changes = append(changes, importChange{Kind: "job", Name: slug, Action: action, Note: note})
			continue
		}
		var previous *core.Job
		if exists {
			previous = &existing
		}
		job, err := s.validateJob(ctx, req, previous)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if exists {
			if _, err := s.store.Jobs.Update(ctx, job); err != nil {
				s.fail(w, r, err)
				return
			}
			changes = append(changes, importChange{Kind: "job", Name: slug, Action: "update"})
			continue
		}
		if _, err := s.store.Jobs.Create(ctx, job); err != nil {
			s.fail(w, r, err)
			return
		}
		changes = append(changes, importChange{Kind: "job", Name: slug, Action: "create"})
	}

	if !dryRun {
		s.engine.ReloadSchedule()
		s.audit(r, nil, "import", "import", "", "", map[string]any{"changes": len(changes)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"dryRun": dryRun, "changes": changes, "total": len(changes)})
}
