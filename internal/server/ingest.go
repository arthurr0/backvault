package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/engine"
)

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := chi.URLParam(r, "jobSlug")
	if err := s.requireJobAccess(r, slug); err != nil {
		s.writeError(w, r, http.StatusForbidden, codeForbidden, err.Error())
		return
	}
	job, err := s.store.Jobs.GetBySlug(ctx, slug)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Body == nil {
		s.fail(w, r, newValidationError("request body is required"))
		return
	}
	defer r.Body.Close()

	opts := engine.IngestOptions{
		Filename:  strings.TrimSpace(r.Header.Get("X-Backvault-Filename")),
		SHA256:    strings.TrimSpace(r.Header.Get("X-Backvault-Sha256")),
		Packed:    queryBool(r, "packed"),
		Size:      r.ContentLength,
		CreatedBy: actorLabel(r),
	}
	run, artifacts, err := s.engine.Ingest(ctx, job, r.Body, opts)
	if err != nil {
		if run.ID != "" {
			writeJSON(w, http.StatusUnprocessableEntity, errorEnvelope{Error: apiError{
				Code:    codeValidation,
				Message: run.Error,
				Fields:  map[string]string{"runId": run.ID},
			}})
			return
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"run": run, "artifacts": artifacts})
}
