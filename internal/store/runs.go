package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type RunRepo struct{ s *Store }

const runColumns = `id, job_id, job_slug, job_name, kind, trigger_kind, status, queued_at, started_at, finished_at,
	duration_ms, bytes, raw_bytes, sha256, filename, error, stages, artifact_ids, attempt, meta, created_by`

func scanRun(sc interface{ Scan(...any) error }) (core.Run, error) {
	var (
		r        core.Run
		queued   sql.NullString
		started  sql.NullString
		finished sql.NullString
		stages   string
		artifact string
		meta     string
	)
	err := sc.Scan(&r.ID, &r.JobID, &r.JobSlug, &r.JobName, &r.Kind, &r.Trigger, &r.Status, &queued, &started, &finished,
		&r.DurationMS, &r.Bytes, &r.RawBytes, &r.SHA256, &r.Filename, &r.Error, &stages, &artifact, &r.Attempt, &meta, &r.CreatedBy)
	if err != nil {
		return r, err
	}
	r.QueuedAt = scanTime(queued)
	r.StartedAt = scanTimePtr(started)
	r.FinishedAt = scanTimePtr(finished)
	r.Stages = []core.Stage{}
	if err := decodeJSON(stages, &r.Stages); err != nil {
		return r, err
	}
	r.ArtifactIDs = decodeStrings(artifact)
	r.Meta = decodeMap(meta)
	return r, nil
}

func (r *RunRepo) Create(ctx context.Context, run core.Run) (core.Run, error) {
	if run.ID == "" {
		run.ID = NewID()
	}
	if run.QueuedAt.IsZero() {
		run.QueuedAt = time.Now().UTC()
	}
	if run.Stages == nil {
		run.Stages = []core.Stage{}
	}
	if run.ArtifactIDs == nil {
		run.ArtifactIDs = []string{}
	}
	_, err := r.s.execWrite(ctx, `INSERT INTO runs (`+runColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.JobID, run.JobSlug, run.JobName, string(run.Kind), string(run.Trigger), string(run.Status),
		formatTime(run.QueuedAt), nullTime(run.StartedAt), nullTime(run.FinishedAt),
		run.DurationMS, run.Bytes, run.RawBytes, run.SHA256, run.Filename, run.Error,
		mustJSON(run.Stages), mustJSON(run.ArtifactIDs), run.Attempt, mustJSON(run.Meta), run.CreatedBy)
	if err != nil {
		return run, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (r *RunRepo) Save(ctx context.Context, run core.Run) error {
	if run.Stages == nil {
		run.Stages = []core.Stage{}
	}
	if run.ArtifactIDs == nil {
		run.ArtifactIDs = []string{}
	}
	res, err := r.s.execWrite(ctx, `UPDATE runs SET job_id = ?, job_slug = ?, job_name = ?, kind = ?, trigger_kind = ?, status = ?,
		queued_at = ?, started_at = ?, finished_at = ?, duration_ms = ?, bytes = ?, raw_bytes = ?, sha256 = ?, filename = ?,
		error = ?, stages = ?, artifact_ids = ?, attempt = ?, meta = ?, created_by = ? WHERE id = ?`,
		run.JobID, run.JobSlug, run.JobName, string(run.Kind), string(run.Trigger), string(run.Status),
		formatTime(run.QueuedAt), nullTime(run.StartedAt), nullTime(run.FinishedAt),
		run.DurationMS, run.Bytes, run.RawBytes, run.SHA256, run.Filename, run.Error,
		mustJSON(run.Stages), mustJSON(run.ArtifactIDs), run.Attempt, mustJSON(run.Meta), run.CreatedBy, run.ID)
	if err != nil {
		return fmt.Errorf("save run: %w", err)
	}
	return affected(res, "run", run.ID)
}

func (r *RunRepo) Get(ctx context.Context, id string) (core.Run, error) {
	run, err := scanRun(r.s.queryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return run, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return run, fmt.Errorf("get run: %w", err)
	}
	return run, nil
}

type RunFilter struct {
	JobID    string
	JobSlug  string
	Status   string
	Statuses []string
	Kind     string
	Since    time.Time
	Until    time.Time
	Page     Page
}

func (f RunFilter) where() *whereBuilder {
	w := &whereBuilder{}
	if f.JobID != "" {
		w.add("job_id = ?", f.JobID)
	}
	if f.JobSlug != "" {
		w.add("job_slug = ?", f.JobSlug)
	}
	if f.Status != "" {
		w.add("status = ?", f.Status)
	}
	if len(f.Statuses) > 0 {
		placeholders := make([]string, len(f.Statuses))
		args := make([]any, len(f.Statuses))
		for i, s := range f.Statuses {
			placeholders[i] = "?"
			args[i] = s
		}
		w.add("status IN ("+strings.Join(placeholders, ", ")+")", args...)
	}
	if f.Kind != "" {
		w.add("kind = ?", f.Kind)
	}
	w.addTime("queued_at >= ?", f.Since)
	w.addTime("queued_at <= ?", f.Until)
	return w
}

func (r *RunRepo) List(ctx context.Context, f RunFilter) ([]core.Run, int, error) {
	w := f.where()
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM runs`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+runColumns+` FROM runs`+w.sql()+` ORDER BY queued_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []core.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, total, rows.Err()
}

func (r *RunRepo) CountByStatus(ctx context.Context, status core.RunStatus) (int, error) {
	var n int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM runs WHERE status = ?`, string(status)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count runs by status: %w", err)
	}
	return n, nil
}

func (r *RunRepo) Unfinished(ctx context.Context) ([]core.Run, error) {
	rows, err := r.s.query(ctx, `SELECT `+runColumns+` FROM runs WHERE status IN (?, ?) ORDER BY queued_at`, string(core.RunQueued), string(core.RunRunning))
	if err != nil {
		return nil, fmt.Errorf("list unfinished runs: %w", err)
	}
	defer rows.Close()
	out := []core.Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *RunRepo) ActiveForJob(ctx context.Context, jobID string) (core.Run, bool, error) {
	run, err := scanRun(r.s.queryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE job_id = ? AND status IN (?, ?) ORDER BY queued_at DESC LIMIT 1`,
		jobID, string(core.RunQueued), string(core.RunRunning)))
	if errors.Is(err, sql.ErrNoRows) {
		return run, false, nil
	}
	if err != nil {
		return run, false, fmt.Errorf("find active run: %w", err)
	}
	return run, true, nil
}

func (r *RunRepo) LastSuccessByJob(ctx context.Context) (map[string]time.Time, error) {
	rows, err := r.s.query(ctx, `SELECT job_id, MAX(finished_at) FROM runs WHERE status = ? AND finished_at IS NOT NULL GROUP BY job_id`, string(core.RunSuccess))
	if err != nil {
		return nil, fmt.Errorf("last success by job: %w", err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var ts sql.NullString
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, fmt.Errorf("scan last success: %w", err)
		}
		if t := scanTime(ts); !t.IsZero() {
			out[id] = t
		}
	}
	return out, rows.Err()
}

func (r *RunRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.s.execWrite(ctx, `DELETE FROM runs WHERE queued_at < ? AND status NOT IN (?, ?)`,
		formatTime(cutoff), string(core.RunQueued), string(core.RunRunning))
	if err != nil {
		return 0, fmt.Errorf("delete old runs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
