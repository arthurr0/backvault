package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/auth"
	"github.com/arthurr0/backvault/internal/core"
)

type tokenRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	JobSlugs  []string   `json:"jobSlugs"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Tokens.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, len(items))
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := decodeBody(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid token")
	if strings.TrimSpace(req.Name) == "" {
		ve.field("name", "required")
	}
	if err := auth.ValidateScopes(req.Scopes); err != nil {
		ve.field("scopes", err.Error())
	}
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now().UTC()) {
		ve.field("expiresAt", "must be in the future")
	}
	for _, slug := range req.JobSlugs {
		if _, err := s.store.Jobs.GetBySlug(r.Context(), slug); err != nil {
			ve.field("jobSlugs", "unknown job: "+slug)
		}
	}
	if !ve.empty() {
		s.fail(w, r, ve)
		return
	}
	created, err := s.auth.CreateToken(r.Context(), core.APIToken{
		Name:      strings.TrimSpace(req.Name),
		Scopes:    req.Scopes,
		JobSlugs:  req.JobSlugs,
		ExpiresAt: req.ExpiresAt,
		CreatedBy: actorLabel(r),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "token.create", "token", created.Token.ID, created.Token.Name, map[string]any{"scopes": created.Token.Scopes})
	writeJSON(w, http.StatusCreated, map[string]any{"token": created.Token, "secret": created.Secret})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok, err := s.store.Tokens.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.Tokens.Delete(ctx, tok.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.audit(r, nil, "token.delete", "token", tok.ID, tok.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}
