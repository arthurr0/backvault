package server

import (
	"net/http"
	"time"

	"github.com/arthurr0/backvault/internal/version"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": version.Version,
		"time":    time.Now().UTC(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 3*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "database": err.Error()})
		return
	}
	scheduler := s.engine != nil && s.engine.SchedulerRunning()
	status := http.StatusOK
	if !scheduler {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status":    map[bool]string{true: "ok", false: "unavailable"}[scheduler],
		"database":  "ok",
		"scheduler": scheduler,
	})
}
