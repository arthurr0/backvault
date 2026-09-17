package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	stats, err := s.store.Stats.Dashboard(r.Context(), time.Now().UTC(), days, 15)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
