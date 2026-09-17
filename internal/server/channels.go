package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
)

type channelRequest struct {
	Name    string      `json:"name"`
	Kind    string      `json:"kind"`
	Config  core.Config `json:"config"`
	Enabled *bool       `json:"enabled"`
	Events  []string    `json:"events"`
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Channels.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	masked := make([]core.NotificationChannel, len(items))
	for i, item := range items {
		masked[i] = maskChannel(item)
	}
	writeList(w, masked, len(masked))
}

func (s *Server) handleGetChannel(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.Channels.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskChannel(c))
}

func (s *Server) validateChannelEvents(events []string) error {
	ve := newValidationError("invalid channel")
	for _, ev := range events {
		if !containsString(core.AllEvents, ev) {
			ve.field("events", "unknown event: "+ev)
		}
	}
	if ve.empty() {
		return nil
	}
	return ve
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid channel")
	if strings.TrimSpace(req.Name) == "" {
		ve.field("name", "required")
	}
	if strings.TrimSpace(req.Kind) == "" {
		ve.field("kind", "required")
	}
	if !ve.empty() {
		s.fail(w, r, ve)
		return
	}
	if err := s.validateChannelEvents(req.Events); err != nil {
		s.fail(w, r, err)
		return
	}
	spec, driver, err := notifierSpec(req.Kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	prepared, err := s.prepareConfig(spec, req.Config, nil, driver.Validate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	c, err := s.store.Channels.Create(r.Context(), core.NotificationChannel{
		Name:    strings.TrimSpace(req.Name),
		Kind:    req.Kind,
		Config:  prepared.Encrypted,
		Enabled: enabled,
		Events:  req.Events,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "channel.create", "channel", c.ID, c.Name, map[string]any{"kind": c.Kind})
	writeJSON(w, http.StatusCreated, maskChannel(c))
}

func (s *Server) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.store.Channels.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req channelRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.fail(w, r, newValidationError("invalid channel").field("name", "required"))
		return
	}
	if err := s.validateChannelEvents(req.Events); err != nil {
		s.fail(w, r, err)
		return
	}
	kind := req.Kind
	if strings.TrimSpace(kind) == "" {
		kind = existing.Kind
	}
	spec, driver, err := notifierSpec(kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	previous := existing.Config
	if kind != existing.Kind {
		previous = nil
	}
	prepared, err := s.prepareConfig(spec, req.Config, previous, driver.Validate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Kind = kind
	existing.Config = prepared.Encrypted
	existing.Events = req.Events
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	updated, err := s.store.Channels.Update(ctx, existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "channel.update", "channel", updated.ID, updated.Name, map[string]any{"kind": updated.Kind})
	writeJSON(w, http.StatusOK, maskChannel(updated))
}

func (s *Server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := s.store.Channels.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.Channels.Delete(ctx, c.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "channel.delete", "channel", c.ID, c.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestChannelConfig(w http.ResponseWriter, r *http.Request) {
	var req driverTestRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	spec, _, err := notifierSpec(req.Kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cfg := req.Config
	if cfg == nil {
		cfg = core.Config{}
	}
	plain, err := s.secrets.DecryptConfig(cfg, spec.SecretFields())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.engine.TestChannelConfig(r.Context(), req.Kind, plain, s.log))
}

func (s *Server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := s.store.Channels.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := s.engine.TestChannelConfig(ctx, c.Kind, c.Config, s.log)
	s.audit(r, nil, "channel.test", "channel", c.ID, c.Name, map[string]any{"ok": result.OK})
	writeJSON(w, http.StatusOK, result)
}
