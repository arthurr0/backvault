package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type destinationRequest struct {
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`
	Description string      `json:"description"`
	Config      core.Config `json:"config"`
	Tags        []string    `json:"tags"`
}

func (s *Server) handleListDestinations(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.DestinationFilter{
		Kind: strings.TrimSpace(r.URL.Query().Get("kind")),
		Q:    strings.TrimSpace(r.URL.Query().Get("q")),
		Page: page,
	}
	items, total, err := s.store.Destinations.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	masked := make([]core.Destination, len(items))
	for i, item := range items {
		masked[i] = maskDestination(item)
	}
	writeList(w, masked, total)
}

func (s *Server) handleGetDestination(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Destinations.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskDestination(d))
}

func (s *Server) handleCreateDestination(w http.ResponseWriter, r *http.Request) {
	var req destinationRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid destination")
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
	spec, driver, err := destinationSpec(req.Kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	prepared, err := s.prepareConfig(spec, req.Config, nil, driver.Validate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.store.Destinations.Create(r.Context(), core.Destination{
		Name:        strings.TrimSpace(req.Name),
		Kind:        req.Kind,
		Description: req.Description,
		Config:      prepared.Encrypted,
		Tags:        req.Tags,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "destination.create", "destination", d.ID, d.Name, map[string]any{"kind": d.Kind})
	writeJSON(w, http.StatusCreated, maskDestination(d))
}

func (s *Server) handleUpdateDestination(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.store.Destinations.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req destinationRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.fail(w, r, newValidationError("invalid destination").field("name", "required"))
		return
	}
	kind := req.Kind
	if strings.TrimSpace(kind) == "" {
		kind = existing.Kind
	}
	spec, driver, err := destinationSpec(kind)
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
	existing.Description = req.Description
	existing.Config = prepared.Encrypted
	existing.Tags = req.Tags
	updated, err := s.store.Destinations.Update(ctx, existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "destination.update", "destination", updated.ID, updated.Name, map[string]any{"kind": updated.Kind})
	writeJSON(w, http.StatusOK, maskDestination(updated))
}

func (s *Server) handleDeleteDestination(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.store.Destinations.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if d.JobCount > 0 {
		s.writeError(w, r, http.StatusConflict, codeConflict, "destination is used by jobs")
		return
	}
	if err := s.store.Destinations.Delete(ctx, d.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "destination.delete", "destination", d.ID, d.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestDestinationConfig(w http.ResponseWriter, r *http.Request) {
	var req driverTestRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	spec, _, err := destinationSpec(req.Kind)
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
	writeJSON(w, http.StatusOK, s.engine.TestDestinationConfig(r.Context(), req.Kind, plain, s.log))
}

func (s *Server) handleTestDestination(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.store.Destinations.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := s.engine.TestDestinationConfig(ctx, d.Kind, d.Config, s.log)
	if err := s.store.Destinations.SetTestResult(ctx, d.ID, result.OK, result.Message, time.Now().UTC()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "destination.test", "destination", d.ID, d.Name, map[string]any{"ok": result.OK})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleBrowseDestination(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.store.Destinations.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items, err := s.engine.BrowseDestination(ctx, d, strings.TrimSpace(r.URL.Query().Get("prefix")), s.log)
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, codeInternal, err.Error())
		return
	}
	writeList(w, items, len(items))
}
