package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/store"
)

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
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
	filter := store.RunFilter{
		Status: strings.TrimSpace(q.Get("status")),
		Kind:   strings.TrimSpace(q.Get("kind")),
		Since:  since,
		Until:  until,
		Page:   page,
	}
	if job := strings.TrimSpace(q.Get("job")); job != "" {
		resolved, err := s.store.Jobs.GetByIDOrSlug(r.Context(), job)
		if err == nil {
			filter.JobID = resolved.ID
		} else {
			filter.JobSlug = job
		}
	}
	items, total, err := s.store.Runs.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeList(w, items, total)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.Runs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.Runs.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if run.Status.Terminal() {
		s.writeError(w, r, http.StatusConflict, codeConflict, "run has already finished")
		return
	}
	if !s.engine.Cancel(run.ID) {
		s.writeError(w, r, http.StatusConflict, codeConflict, "run is not active on this server")
		return
	}
	s.audit(r, nil, "run.cancel", "run", run.ID, run.JobName, nil)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	run, err := s.store.Runs.Get(ctx, chi.URLParam(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	text, err := s.store.Runs.Log(ctx, run.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.fail(w, r, newValidationError("offset must be a non-negative integer").field("offset", "invalid"))
			return
		}
		offset = n
	}
	if offset > len(text) {
		offset = len(text)
	}
	body := text[offset:]
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Backvault-Log-Size", strconv.Itoa(len(text)))
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
