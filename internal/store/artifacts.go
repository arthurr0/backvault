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

type ArtifactRepo struct{ s *Store }

const artifactColumns = `id, job_id, job_slug, job_name, run_id, destination_id, destination_name, destination_kind,
	path, filename, size, sha256, compression, encryption, source_kind, extension, status, created_at, verified_at, deleted_at, meta`

func scanArtifact(sc interface{ Scan(...any) error }) (core.Artifact, error) {
	var (
		a        core.Artifact
		created  sql.NullString
		verified sql.NullString
		deleted  sql.NullString
		meta     string
	)
	err := sc.Scan(&a.ID, &a.JobID, &a.JobSlug, &a.JobName, &a.RunID, &a.DestinationID, &a.DestinationName, &a.DestinationKind,
		&a.Path, &a.Filename, &a.Size, &a.SHA256, &a.Compression, &a.Encryption, &a.SourceKind, &a.Extension, &a.Status,
		&created, &verified, &deleted, &meta)
	if err != nil {
		return a, err
	}
	a.CreatedAt = scanTime(created)
	a.VerifiedAt = scanTimePtr(verified)
	a.DeletedAt = scanTimePtr(deleted)
	a.Meta = decodeMap(meta)
	return a, nil
}

func (r *ArtifactRepo) Create(ctx context.Context, a core.Artifact) (core.Artifact, error) {
	if a.ID == "" {
		a.ID = NewID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = core.ArtifactPresent
	}
	_, err := r.s.execWrite(ctx, `INSERT INTO artifacts (`+artifactColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.JobID, a.JobSlug, a.JobName, a.RunID, a.DestinationID, a.DestinationName, a.DestinationKind,
		a.Path, a.Filename, a.Size, a.SHA256, string(a.Compression), string(a.Encryption), a.SourceKind, a.Extension, string(a.Status),
		formatTime(a.CreatedAt), nullTime(a.VerifiedAt), nullTime(a.DeletedAt), mustJSON(a.Meta))
	if err != nil {
		return a, fmt.Errorf("create artifact: %w", err)
	}
	return a, nil
}

func (r *ArtifactRepo) Get(ctx context.Context, id string) (core.Artifact, error) {
	a, err := scanArtifact(r.s.queryRow(ctx, `SELECT `+artifactColumns+` FROM artifacts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("artifact %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return a, fmt.Errorf("get artifact: %w", err)
	}
	return a, nil
}

func (r *ArtifactRepo) SetStatus(ctx context.Context, id string, status core.ArtifactStatus, at time.Time) error {
	var deleted any
	if status == core.ArtifactDeleted || status == core.ArtifactPruned {
		deleted = formatTime(at)
	}
	res, err := r.s.execWrite(ctx, `UPDATE artifacts SET status = ?, deleted_at = COALESCE(?, deleted_at) WHERE id = ?`, string(status), deleted, id)
	if err != nil {
		return fmt.Errorf("set artifact status: %w", err)
	}
	return affected(res, "artifact", id)
}

func (r *ArtifactRepo) SetVerified(ctx context.Context, id string, at time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE artifacts SET verified_at = ? WHERE id = ?`, formatTime(at), id); err != nil {
		return fmt.Errorf("set artifact verified: %w", err)
	}
	return nil
}

func (r *ArtifactRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM artifacts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return affected(res, "artifact", id)
}

type ArtifactFilter struct {
	JobID         string
	JobSlug       string
	DestinationID string
	Status        string
	RunID         string
	Since         time.Time
	Until         time.Time
	Q             string
	Page          Page
}

func (f ArtifactFilter) where() *whereBuilder {
	w := &whereBuilder{}
	if f.JobID != "" {
		w.add("job_id = ?", f.JobID)
	}
	if f.JobSlug != "" {
		w.add("job_slug = ?", f.JobSlug)
	}
	if f.DestinationID != "" {
		w.add("destination_id = ?", f.DestinationID)
	}
	if f.Status != "" {
		w.add("status = ?", f.Status)
	}
	if f.RunID != "" {
		w.add("run_id = ?", f.RunID)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		w.add("filename LIKE ?", "%"+q+"%")
	}
	w.addTime("created_at >= ?", f.Since)
	w.addTime("created_at <= ?", f.Until)
	return w
}

func (r *ArtifactRepo) List(ctx context.Context, f ArtifactFilter) ([]core.Artifact, int, error) {
	w := f.where()
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM artifacts`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count artifacts: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+artifactColumns+` FROM artifacts`+w.sql()+` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	out := []core.Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan artifact: %w", err)
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (r *ArtifactRepo) ForJobDestination(ctx context.Context, jobID, destinationID string, status core.ArtifactStatus) ([]core.Artifact, error) {
	rows, err := r.s.query(ctx, `SELECT `+artifactColumns+` FROM artifacts WHERE job_id = ? AND destination_id = ? AND status = ? ORDER BY created_at DESC`,
		jobID, destinationID, string(status))
	if err != nil {
		return nil, fmt.Errorf("list job artifacts: %w", err)
	}
	defer rows.Close()
	out := []core.Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan artifact: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *ArtifactRepo) ForRun(ctx context.Context, runID string) ([]core.Artifact, error) {
	items, _, err := r.List(ctx, ArtifactFilter{RunID: runID, Page: Page{Limit: MaxLimit}})
	return items, err
}

func (r *ArtifactRepo) Count(ctx context.Context) (int, int64, error) {
	var (
		n     int
		bytes int64
	)
	if err := r.s.queryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0) FROM artifacts WHERE status = ?`, string(core.ArtifactPresent)).Scan(&n, &bytes); err != nil {
		return 0, 0, fmt.Errorf("count artifacts: %w", err)
	}
	return n, bytes, nil
}

type ArtifactStat struct {
	DestinationID string
	Status        core.ArtifactStatus
	Count         int
	Bytes         int64
}

func (r *ArtifactRepo) StatsByDestination(ctx context.Context) ([]ArtifactStat, error) {
	rows, err := r.s.query(ctx, `SELECT destination_id, status, COUNT(*), COALESCE(SUM(size), 0) FROM artifacts GROUP BY destination_id, status`)
	if err != nil {
		return nil, fmt.Errorf("artifact stats by destination: %w", err)
	}
	defer rows.Close()
	out := []ArtifactStat{}
	for rows.Next() {
		var (
			stat   ArtifactStat
			status string
		)
		if err := rows.Scan(&stat.DestinationID, &status, &stat.Count, &stat.Bytes); err != nil {
			return nil, fmt.Errorf("scan artifact stats: %w", err)
		}
		stat.Status = core.ArtifactStatus(status)
		out = append(out, stat)
	}
	return out, rows.Err()
}
