package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type JobState struct {
	JobID             string
	LastRun           *core.RunSummary
	LastSuccessAt     *time.Time
	LastDurationMS    int64
	NextRunAt         *time.Time
	Overdue           bool
	OverdueNotifiedAt *time.Time
	UpdatedAt         time.Time
}

type JobStateRepo struct{ s *Store }

const jobStateColumns = `job_id, last_run, last_success_at, last_duration_ms, next_run_at, overdue, overdue_notified_at, updated_at`

func scanJobState(sc interface{ Scan(...any) error }) (JobState, error) {
	var (
		st       JobState
		lastRun  sql.NullString
		lastOK   sql.NullString
		nextRun  sql.NullString
		overdue  int
		notified sql.NullString
		updated  sql.NullString
	)
	if err := sc.Scan(&st.JobID, &lastRun, &lastOK, &st.LastDurationMS, &nextRun, &overdue, &notified, &updated); err != nil {
		return st, err
	}
	if lastRun.Valid && lastRun.String != "" && lastRun.String != "null" {
		var summary core.RunSummary
		if err := decodeJSON(lastRun.String, &summary); err == nil && summary.ID != "" {
			st.LastRun = &summary
		}
	}
	st.LastSuccessAt = scanTimePtr(lastOK)
	st.NextRunAt = scanTimePtr(nextRun)
	st.Overdue = overdue != 0
	st.OverdueNotifiedAt = scanTimePtr(notified)
	st.UpdatedAt = scanTime(updated)
	return st, nil
}

func (r *JobStateRepo) Get(ctx context.Context, jobID string) (JobState, error) {
	st, err := scanJobState(r.s.queryRow(ctx, `SELECT `+jobStateColumns+` FROM job_state WHERE job_id = ?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return JobState{JobID: jobID}, nil
	}
	if err != nil {
		return st, fmt.Errorf("get job state: %w", err)
	}
	return st, nil
}

func (r *JobStateRepo) All(ctx context.Context) (map[string]JobState, error) {
	rows, err := r.s.query(ctx, `SELECT `+jobStateColumns+` FROM job_state`)
	if err != nil {
		return nil, fmt.Errorf("list job state: %w", err)
	}
	defer rows.Close()
	out := map[string]JobState{}
	for rows.Next() {
		st, err := scanJobState(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job state: %w", err)
		}
		out[st.JobID] = st
	}
	return out, rows.Err()
}

func (r *JobStateRepo) SetLastRun(ctx context.Context, jobID string, summary core.RunSummary, successAt *time.Time) error {
	_, err := r.s.execWrite(ctx, `INSERT INTO job_state (job_id, last_run, last_success_at, last_duration_ms, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET last_run = excluded.last_run,
			last_success_at = COALESCE(excluded.last_success_at, job_state.last_success_at),
			last_duration_ms = excluded.last_duration_ms,
			updated_at = excluded.updated_at`,
		jobID, mustJSON(summary), nullTime(successAt), summary.DurationMS, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("set job last run: %w", err)
	}
	return nil
}

func (r *JobStateRepo) SetNextRun(ctx context.Context, jobID string, next *time.Time) error {
	_, err := r.s.execWrite(ctx, `INSERT INTO job_state (job_id, next_run_at, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET next_run_at = excluded.next_run_at, updated_at = excluded.updated_at`,
		jobID, nullTime(next), formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("set next run: %w", err)
	}
	return nil
}

func (r *JobStateRepo) SetOverdue(ctx context.Context, jobID string, overdue bool, notifiedAt *time.Time) error {
	_, err := r.s.execWrite(ctx, `INSERT INTO job_state (job_id, overdue, overdue_notified_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET overdue = excluded.overdue, overdue_notified_at = excluded.overdue_notified_at, updated_at = excluded.updated_at`,
		jobID, boolToInt(overdue), nullTime(notifiedAt), formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("set overdue: %w", err)
	}
	return nil
}
