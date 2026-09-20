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

type SourceRepo struct{ s *Store }

const sourceInsertColumns = `id, name, kind, description, config, host_id, tags, created_at, updated_at, last_test_at, last_test_ok, last_test_error`

const sourceSelect = `SELECT s.id, s.name, s.kind, s.description, s.config, s.host_id, s.tags, s.created_at, s.updated_at, s.last_test_at, s.last_test_ok, s.last_test_error, COALESCE(h.name, '') FROM sources s LEFT JOIN hosts h ON h.id = s.host_id`

func scanSource(sc interface{ Scan(...any) error }) (core.Source, error) {
	var (
		src      core.Source
		cfg      string
		tags     string
		created  sql.NullString
		updated  sql.NullString
		testedAt sql.NullString
		testedOK sql.NullInt64
	)
	if err := sc.Scan(&src.ID, &src.Name, &src.Kind, &src.Description, &cfg, &src.HostID, &tags, &created, &updated, &testedAt, &testedOK, &src.LastTestError, &src.HostName); err != nil {
		return src, err
	}
	src.Config = core.Config{}
	if err := decodeJSON(cfg, &src.Config); err != nil {
		return src, err
	}
	src.Tags = decodeStrings(tags)
	src.CreatedAt = scanTime(created)
	src.UpdatedAt = scanTime(updated)
	src.LastTestAt = scanTimePtr(testedAt)
	src.LastTestOK = scanBoolPtr(testedOK)
	return src, nil
}

func (r *SourceRepo) Create(ctx context.Context, src core.Source) (core.Source, error) {
	now := time.Now().UTC()
	if src.ID == "" {
		src.ID = NewID()
	}
	src.CreatedAt = now
	src.UpdatedAt = now
	if src.Tags == nil {
		src.Tags = []string{}
	}
	cfg, err := encodeJSON(src.Config)
	if err != nil {
		return src, err
	}
	_, err = r.s.execWrite(ctx, `INSERT INTO sources (`+sourceInsertColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.ID, src.Name, src.Kind, src.Description, cfg, src.HostID, mustJSON(src.Tags),
		formatTime(src.CreatedAt), formatTime(src.UpdatedAt), nullTime(src.LastTestAt), nullBool(src.LastTestOK), src.LastTestError)
	if err != nil {
		if isUniqueViolation(err) {
			return src, fmt.Errorf("source name %q already exists: %w", src.Name, ErrConflict)
		}
		return src, fmt.Errorf("create source: %w", err)
	}
	return src, nil
}

func (r *SourceRepo) Update(ctx context.Context, src core.Source) (core.Source, error) {
	src.UpdatedAt = time.Now().UTC()
	if src.Tags == nil {
		src.Tags = []string{}
	}
	cfg, err := encodeJSON(src.Config)
	if err != nil {
		return src, err
	}
	res, err := r.s.execWrite(ctx, `UPDATE sources SET name = ?, kind = ?, description = ?, config = ?, host_id = ?, tags = ?, updated_at = ? WHERE id = ?`,
		src.Name, src.Kind, src.Description, cfg, src.HostID, mustJSON(src.Tags), formatTime(src.UpdatedAt), src.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return src, fmt.Errorf("source name %q already exists: %w", src.Name, ErrConflict)
		}
		return src, fmt.Errorf("update source: %w", err)
	}
	return src, affected(res, "source", src.ID)
}

func (r *SourceRepo) SetTestResult(ctx context.Context, id string, ok bool, message string, at time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE sources SET last_test_at = ?, last_test_ok = ?, last_test_error = ? WHERE id = ?`,
		formatTime(at), boolToInt(ok), message, id); err != nil {
		return fmt.Errorf("store source test result: %w", err)
	}
	return nil
}

func (r *SourceRepo) Get(ctx context.Context, id string) (core.Source, error) {
	src, err := scanSource(r.s.queryRow(ctx, sourceSelect+` WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return src, fmt.Errorf("source %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return src, fmt.Errorf("get source: %w", err)
	}
	counts, err := r.s.jobCountsBySource(ctx)
	if err != nil {
		return src, err
	}
	src.JobCount = counts[src.ID]
	return src, nil
}

type SourceFilter struct {
	Kind   string
	HostID string
	Q      string
	Page   Page
}

func (r *SourceRepo) List(ctx context.Context, f SourceFilter) ([]core.Source, int, error) {
	w := &whereBuilder{}
	if f.Kind != "" {
		w.add("s.kind = ?", f.Kind)
	}
	if f.HostID != "" {
		w.add("s.host_id = ?", f.HostID)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		w.add("(s.name LIKE ? OR s.description LIKE ?)", "%"+q+"%", "%"+q+"%")
	}
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM sources s`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sources: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, sourceSelect+w.sql()+` ORDER BY s.name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()
	out := []core.Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan source: %w", err)
		}
		out = append(out, src)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	counts, err := r.s.jobCountsBySource(ctx)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].JobCount = counts[out[i].ID]
	}
	return out, total, nil
}

func (r *SourceRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("source %s is used by jobs: %w", id, ErrConflict)
		}
		return fmt.Errorf("delete source: %w", err)
	}
	return affected(res, "source", id)
}

func (s *Store) jobCountsBySource(ctx context.Context) (map[string]int, error) {
	rows, err := s.query(ctx, `SELECT source_id, COUNT(*) FROM jobs GROUP BY source_id`)
	if err != nil {
		return nil, fmt.Errorf("count jobs by source: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan job count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}
