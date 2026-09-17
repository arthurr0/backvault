package server

import (
	"net/http"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/engine"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings.Get(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	current, err := s.store.Settings.Get(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	incoming := current
	if err := decodeBody(r, &incoming); err != nil {
		s.fail(w, r, err)
		return
	}
	ve := newValidationError("invalid settings")
	if incoming.MaxConcurrentRuns < 1 || incoming.MaxConcurrentRuns > 64 {
		ve.field("maxConcurrentRuns", "must be between 1 and 64")
	}
	if incoming.OverdueCheckMinutes < 1 || incoming.OverdueCheckMinutes > 1440 {
		ve.field("overdueCheckMinutes", "must be between 1 and 1440")
	}
	if incoming.RunHistoryDays < 0 || incoming.AuditHistoryDays < 0 {
		ve.field("runHistoryDays", "must not be negative")
	}
	if _, err := engine.LoadLocation(incoming.DefaultTimezone); err != nil {
		ve.field("defaultTimezone", err.Error())
	}
	if incoming.BaseURL != "" && !isHTTPURL(incoming.BaseURL) {
		ve.field("baseUrl", "must start with http:// or https://")
	}
	r2 := incoming.DefaultRetention
	if r2.KeepLast < 0 || r2.KeepHourly < 0 || r2.KeepDaily < 0 || r2.KeepWeekly < 0 || r2.KeepMonthly < 0 || r2.KeepYearly < 0 || r2.MaxAgeDays < 0 {
		ve.field("defaultRetention", "values must not be negative")
	}
	for _, ev := range incoming.DefaultNotifyOn {
		if !containsString(core.AllEvents, ev) {
			ve.field("defaultNotifyOn", "unknown event: "+ev)
		}
	}
	if !ve.empty() {
		s.fail(w, r, ve)
		return
	}
	saved, err := s.store.Settings.Save(ctx, incoming)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.engine.ReloadSettings(ctx); err != nil {
		s.log.Error("could not reload engine settings", "error", err)
	}
	s.engine.ReloadSchedule()
	s.audit(r, nil, "settings.update", "settings", "settings", saved.SiteName, nil)
	writeJSON(w, http.StatusOK, saved)
}

func isHTTPURL(v string) bool {
	return len(v) > 7 && (v[:7] == "http://" || (len(v) > 8 && v[:8] == "https://"))
}
