package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type SettingsRepo struct{ s *Store }

func DefaultSettings() core.Settings {
	return core.Settings{
		SiteName:            "Backvault",
		BaseURL:             "",
		DefaultTimezone:     "UTC",
		MaxConcurrentRuns:   2,
		DefaultRetention:    core.Retention{KeepLast: 7, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 6},
		RunHistoryDays:      90,
		AuditHistoryDays:    365,
		OverdueCheckMinutes: 15,
		DefaultNotifyOn:     []string{core.EventRunFailed, core.EventRunWarning, core.EventJobOverdue},
	}
}

func (r *SettingsRepo) Get(ctx context.Context) (core.Settings, error) {
	var raw string
	err := r.s.queryRow(ctx, `SELECT data FROM settings WHERE id = 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return DefaultSettings(), fmt.Errorf("get settings: %w", err)
	}
	s := DefaultSettings()
	if err := decodeJSON(raw, &s); err != nil {
		return DefaultSettings(), err
	}
	return normalizeSettings(s), nil
}

func (r *SettingsRepo) Save(ctx context.Context, s core.Settings) (core.Settings, error) {
	s = normalizeSettings(s)
	data, err := encodeJSON(s)
	if err != nil {
		return s, err
	}
	if _, err := r.s.execWrite(ctx, `INSERT INTO settings (id, data, updated_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		data, formatTime(time.Now().UTC())); err != nil {
		return s, fmt.Errorf("save settings: %w", err)
	}
	return s, nil
}

func normalizeSettings(s core.Settings) core.Settings {
	if s.SiteName == "" {
		s.SiteName = "Backvault"
	}
	if s.DefaultTimezone == "" {
		s.DefaultTimezone = "UTC"
	}
	if s.MaxConcurrentRuns <= 0 {
		s.MaxConcurrentRuns = 2
	}
	if s.OverdueCheckMinutes <= 0 {
		s.OverdueCheckMinutes = 15
	}
	if s.RunHistoryDays < 0 {
		s.RunHistoryDays = 0
	}
	if s.AuditHistoryDays < 0 {
		s.AuditHistoryDays = 0
	}
	if s.DefaultNotifyOn == nil {
		s.DefaultNotifyOn = []string{}
	}
	return s
}
