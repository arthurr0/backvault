package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

type sourceRequest struct {
	Name        string      `json:"name"`
	Kind        string      `json:"kind"`
	Description string      `json:"description"`
	Config      core.Config `json:"config"`
	Tags        []string    `json:"tags"`
}

type driverTestRequest struct {
	Kind   string      `json:"kind"`
	Config core.Config `json:"config"`
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	page, err := pageFromRequest(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	filter := store.SourceFilter{
		Kind: strings.TrimSpace(r.URL.Query().Get("kind")),
		Q:    strings.TrimSpace(r.URL.Query().Get("q")),
		Page: page,
	}
	items, total, err := s.store.Sources.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	masked := make([]core.Source, len(items))
	for i, item := range items {
		masked[i] = maskSource(item)
	}
	writeList(w, masked, total)
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	src, err := s.store.Sources.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, maskSource(src))
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req sourceRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid source")
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
	spec, driver, err := sourceSpec(req.Kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	prepared, err := s.prepareConfig(spec, req.Config, nil, driver.Validate)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	src, err := s.store.Sources.Create(r.Context(), core.Source{
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
	s.audit(r, nil, "source.create", "source", src.ID, src.Name, map[string]any{"kind": src.Kind})
	writeJSON(w, http.StatusCreated, maskSource(src))
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	existing, err := s.store.Sources.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req sourceRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.fail(w, r, newValidationError("invalid source").field("name", "required"))
		return
	}
	kind := req.Kind
	if strings.TrimSpace(kind) == "" {
		kind = existing.Kind
	}
	spec, driver, err := sourceSpec(kind)
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
	updated, err := s.store.Sources.Update(ctx, existing)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "source.update", "source", updated.ID, updated.Name, map[string]any{"kind": updated.Kind})
	writeJSON(w, http.StatusOK, maskSource(updated))
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.Sources.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if src.JobCount > 0 {
		s.writeError(w, r, http.StatusConflict, codeConflict, "source is used by jobs")
		return
	}
	if err := s.store.Sources.Delete(ctx, src.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "source.delete", "source", src.ID, src.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestSourceConfig(w http.ResponseWriter, r *http.Request) {
	var req driverTestRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	spec, _, err := sourceSpec(req.Kind)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cfg := req.Config
	if cfg == nil {
		cfg = core.Config{}
	}
	if id := strings.TrimSpace(chi.URLParam(r, "id")); id != "" {
		if existing, err := s.store.Sources.Get(r.Context(), id); err == nil {
			cfg = cfg.MergeSecrets(existing.Config, spec.SecretFields())
		}
	}
	plain, err := s.secrets.DecryptConfig(cfg, spec.SecretFields())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := s.engine.TestSourceConfig(r.Context(), req.Kind, plain, s.log)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTestSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.Sources.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result := s.engine.TestSourceConfig(ctx, src.Kind, src.Config, s.log)
	if err := s.store.Sources.SetTestResult(ctx, src.ID, result.OK, result.Message, time.Now().UTC()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "source.test", "source", src.ID, src.Name, map[string]any{"ok": result.OK})
	writeJSON(w, http.StatusOK, result)
}
