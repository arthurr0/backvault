package store

import (
	"context"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type CleanupResult struct {
	Runs     int64
	Audit    int64
	Sessions int64
}

func (s *Store) Cleanup(ctx context.Context, settings core.Settings, now time.Time) (CleanupResult, error) {
	var res CleanupResult
	if settings.RunHistoryDays > 0 {
		n, err := s.Runs.DeleteOlderThan(ctx, now.AddDate(0, 0, -settings.RunHistoryDays))
		if err != nil {
			return res, err
		}
		res.Runs = n
	}
	if settings.AuditHistoryDays > 0 {
		n, err := s.Audit.DeleteOlderThan(ctx, now.AddDate(0, 0, -settings.AuditHistoryDays))
		if err != nil {
			return res, err
		}
		res.Audit = n
	}
	n, err := s.Sessions.DeleteExpired(ctx, now)
	if err != nil {
		return res, err
	}
	res.Sessions = n
	return res, nil
}
