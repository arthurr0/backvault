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

type DestinationRepo struct{ s *Store }

const destinationColumns = `id, name, kind, description, config, tags, created_at, updated_at, last_test_at, last_test_ok, last_test_error`

func scanDestination(sc interface{ Scan(...any) error }) (core.Destination, error) {
	var (
		d        core.Destination
		cfg      string
		tags     string
		created  sql.NullString
		updated  sql.NullString
		testedAt sql.NullString
		testedOK sql.NullInt64
	)
	if err := sc.Scan(&d.ID, &d.Name, &d.Kind, &d.Description, &cfg, &tags, &created, &updated, &testedAt, &testedOK, &d.LastTestError); err != nil {
		return d, err
	}
	d.Config = core.Config{}
	if err := decodeJSON(cfg, &d.Config); err != nil {
		return d, err
	}
	d.Tags = decodeStrings(tags)
	d.CreatedAt = scanTime(created)
	d.UpdatedAt = scanTime(updated)
	d.LastTestAt = scanTimePtr(testedAt)
	d.LastTestOK = scanBoolPtr(testedOK)
	return d, nil
}

func (r *DestinationRepo) Create(ctx context.Context, d core.Destination) (core.Destination, error) {
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = NewID()
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	if d.Tags == nil {
		d.Tags = []string{}
	}
	cfg, err := encodeJSON(d.Config)
	if err != nil {
		return d, err
	}
	_, err = r.s.execWrite(ctx, `INSERT INTO destinations (`+destinationColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.Kind, d.Description, cfg, mustJSON(d.Tags),
		formatTime(d.CreatedAt), formatTime(d.UpdatedAt), nullTime(d.LastTestAt), nullBool(d.LastTestOK), d.LastTestError)
	if err != nil {
		if isUniqueViolation(err) {
			return d, fmt.Errorf("destination name %q already exists: %w", d.Name, ErrConflict)
		}
		return d, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

func (r *DestinationRepo) Update(ctx context.Context, d core.Destination) (core.Destination, error) {
	d.UpdatedAt = time.Now().UTC()
	if d.Tags == nil {
		d.Tags = []string{}
	}
	cfg, err := encodeJSON(d.Config)
	if err != nil {
		return d, err
	}
	res, err := r.s.execWrite(ctx, `UPDATE destinations SET name = ?, kind = ?, description = ?, config = ?, tags = ?, updated_at = ? WHERE id = ?`,
		d.Name, d.Kind, d.Description, cfg, mustJSON(d.Tags), formatTime(d.UpdatedAt), d.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return d, fmt.Errorf("destination name %q already exists: %w", d.Name, ErrConflict)
		}
		return d, fmt.Errorf("update destination: %w", err)
	}
	if err := affected(res, "destination", d.ID); err != nil {
		return d, err
	}
	if _, err := r.s.execWrite(ctx, `UPDATE artifacts SET destination_name = ?, destination_kind = ? WHERE destination_id = ?`, d.Name, d.Kind, d.ID); err != nil {
		return d, fmt.Errorf("refresh artifact destination labels: %w", err)
	}
	return d, nil
}

func (r *DestinationRepo) SetTestResult(ctx context.Context, id string, ok bool, message string, at time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE destinations SET last_test_at = ?, last_test_ok = ?, last_test_error = ? WHERE id = ?`,
		formatTime(at), boolToInt(ok), message, id); err != nil {
		return fmt.Errorf("store destination test result: %w", err)
	}
	return nil
}

func (r *DestinationRepo) Get(ctx context.Context, id string) (core.Destination, error) {
	d, err := scanDestination(r.s.queryRow(ctx, `SELECT `+destinationColumns+` FROM destinations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("destination %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return d, fmt.Errorf("get destination: %w", err)
	}
	usage, err := r.s.destinationUsage(ctx)
	if err != nil {
		return d, err
	}
	counts, err := r.s.jobCountsByDestination(ctx)
	if err != nil {
		return d, err
	}
	if u, ok := usage[d.ID]; ok {
		d.UsedBytes = u.Bytes
		d.ArtifactCount = u.Artifacts
	}
	d.JobCount = counts[d.ID]
	return d, nil
}

type DestinationFilter struct {
	Kind string
	Q    string
	Page Page
}

func (r *DestinationRepo) List(ctx context.Context, f DestinationFilter) ([]core.Destination, int, error) {
	w := &whereBuilder{}
	if f.Kind != "" {
		w.add("kind = ?", f.Kind)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		w.add("(name LIKE ? OR description LIKE ?)", "%"+q+"%", "%"+q+"%")
	}
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM destinations`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count destinations: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+destinationColumns+` FROM destinations`+w.sql()+` ORDER BY name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list destinations: %w", err)
	}
	defer rows.Close()
	out := []core.Destination{}
	for rows.Next() {
		d, err := scanDestination(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan destination: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	usage, err := r.s.destinationUsage(ctx)
	if err != nil {
		return nil, 0, err
	}
	counts, err := r.s.jobCountsByDestination(ctx)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if u, ok := usage[out[i].ID]; ok {
			out[i].UsedBytes = u.Bytes
			out[i].ArtifactCount = u.Artifacts
		}
		out[i].JobCount = counts[out[i].ID]
	}
	return out, total, nil
}

func (r *DestinationRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM destinations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return affected(res, "destination", id)
}

type usageRow struct {
	Bytes     int64
	Artifacts int
}

func (s *Store) destinationUsage(ctx context.Context) (map[string]usageRow, error) {
	rows, err := s.query(ctx, `SELECT destination_id, COALESCE(SUM(size), 0), COUNT(*) FROM artifacts WHERE status = ? GROUP BY destination_id`, string(core.ArtifactPresent))
	if err != nil {
		return nil, fmt.Errorf("destination usage: %w", err)
	}
	defer rows.Close()
	out := map[string]usageRow{}
	for rows.Next() {
		var id string
		var u usageRow
		if err := rows.Scan(&id, &u.Bytes, &u.Artifacts); err != nil {
			return nil, fmt.Errorf("scan destination usage: %w", err)
		}
		out[id] = u
	}
	return out, rows.Err()
}

func (s *Store) jobCountsByDestination(ctx context.Context) (map[string]int, error) {
	rows, err := s.query(ctx, `SELECT destination_ids FROM jobs`)
	if err != nil {
		return nil, fmt.Errorf("count jobs by destination: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan job destinations: %w", err)
		}
		for _, id := range decodeStrings(raw) {
			out[id]++
		}
	}
	return out, rows.Err()
}
