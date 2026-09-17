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

type JobRepo struct{ s *Store }

const jobColumns = `id, slug, name, description, source_id, destination_ids, schedule, timezone, enabled,
	compression, compression_level, encryption, encryption_passphrase, retention,
	notification_channel_ids, notify_on, timeout_minutes, retries, retry_delay_seconds,
	pre_command, post_command, verify_after_upload, expected_interval_minutes, tags, created_at, updated_at`

func scanJob(sc interface{ Scan(...any) error }) (core.Job, error) {
	var (
		j         core.Job
		destIDs   string
		retention string
		channels  string
		notifyOn  string
		tags      string
		enabled   int
		verify    int
		created   sql.NullString
		updated   sql.NullString
	)
	err := sc.Scan(&j.ID, &j.Slug, &j.Name, &j.Description, &j.SourceID, &destIDs, &j.Schedule, &j.Timezone, &enabled,
		&j.Compression, &j.CompressionLevel, &j.Encryption, &j.EncryptionPassphrase, &retention,
		&channels, &notifyOn, &j.TimeoutMinutes, &j.Retries, &j.RetryDelaySeconds,
		&j.PreCommand, &j.PostCommand, &verify, &j.ExpectedIntervalMinutes, &tags, &created, &updated)
	if err != nil {
		return j, err
	}
	j.DestinationIDs = decodeStrings(destIDs)
	j.NotificationChannelIDs = decodeStrings(channels)
	j.NotifyOn = decodeStrings(notifyOn)
	j.Tags = decodeStrings(tags)
	j.Enabled = enabled != 0
	j.VerifyAfterUpload = verify != 0
	if err := decodeJSON(retention, &j.Retention); err != nil {
		return j, err
	}
	j.CreatedAt = scanTime(created)
	j.UpdatedAt = scanTime(updated)
	return j, nil
}

func jobArgs(j core.Job) []any {
	return []any{
		j.ID, j.Slug, j.Name, j.Description, j.SourceID, mustJSON(j.DestinationIDs), j.Schedule, j.Timezone, boolToInt(j.Enabled),
		string(j.Compression), j.CompressionLevel, string(j.Encryption), j.EncryptionPassphrase, mustJSON(j.Retention),
		mustJSON(j.NotificationChannelIDs), mustJSON(j.NotifyOn), j.TimeoutMinutes, j.Retries, j.RetryDelaySeconds,
		j.PreCommand, j.PostCommand, boolToInt(j.VerifyAfterUpload), j.ExpectedIntervalMinutes, mustJSON(j.Tags),
		formatTime(j.CreatedAt), formatTime(j.UpdatedAt),
	}
}

func normalizeJobSlices(j *core.Job) {
	if j.DestinationIDs == nil {
		j.DestinationIDs = []string{}
	}
	if j.NotificationChannelIDs == nil {
		j.NotificationChannelIDs = []string{}
	}
	if j.NotifyOn == nil {
		j.NotifyOn = []string{}
	}
	if j.Tags == nil {
		j.Tags = []string{}
	}
}

func (r *JobRepo) Create(ctx context.Context, j core.Job) (core.Job, error) {
	now := time.Now().UTC()
	if j.ID == "" {
		j.ID = NewID()
	}
	j.CreatedAt = now
	j.UpdatedAt = now
	normalizeJobSlices(&j)
	_, err := r.s.execWrite(ctx, `INSERT INTO jobs (`+jobColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, jobArgs(j)...)
	if err != nil {
		if isUniqueViolation(err) {
			return j, fmt.Errorf("job slug %q already exists: %w", j.Slug, ErrConflict)
		}
		if isForeignKeyViolation(err) {
			return j, fmt.Errorf("source %s does not exist: %w", j.SourceID, ErrNotFound)
		}
		return j, fmt.Errorf("create job: %w", err)
	}
	return j, nil
}

func (r *JobRepo) Update(ctx context.Context, j core.Job) (core.Job, error) {
	j.UpdatedAt = time.Now().UTC()
	normalizeJobSlices(&j)
	res, err := r.s.execWrite(ctx, `UPDATE jobs SET slug = ?, name = ?, description = ?, source_id = ?, destination_ids = ?,
		schedule = ?, timezone = ?, enabled = ?, compression = ?, compression_level = ?, encryption = ?, encryption_passphrase = ?,
		retention = ?, notification_channel_ids = ?, notify_on = ?, timeout_minutes = ?, retries = ?, retry_delay_seconds = ?,
		pre_command = ?, post_command = ?, verify_after_upload = ?, expected_interval_minutes = ?, tags = ?, updated_at = ? WHERE id = ?`,
		j.Slug, j.Name, j.Description, j.SourceID, mustJSON(j.DestinationIDs),
		j.Schedule, j.Timezone, boolToInt(j.Enabled), string(j.Compression), j.CompressionLevel, string(j.Encryption), j.EncryptionPassphrase,
		mustJSON(j.Retention), mustJSON(j.NotificationChannelIDs), mustJSON(j.NotifyOn), j.TimeoutMinutes, j.Retries, j.RetryDelaySeconds,
		j.PreCommand, j.PostCommand, boolToInt(j.VerifyAfterUpload), j.ExpectedIntervalMinutes, mustJSON(j.Tags), formatTime(j.UpdatedAt), j.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return j, fmt.Errorf("job slug %q already exists: %w", j.Slug, ErrConflict)
		}
		if isForeignKeyViolation(err) {
			return j, fmt.Errorf("source %s does not exist: %w", j.SourceID, ErrNotFound)
		}
		return j, fmt.Errorf("update job: %w", err)
	}
	if err := affected(res, "job", j.ID); err != nil {
		return j, err
	}
	if _, err := r.s.execWrite(ctx, `UPDATE artifacts SET job_slug = ?, job_name = ? WHERE job_id = ?`, j.Slug, j.Name, j.ID); err != nil {
		return j, fmt.Errorf("refresh artifact job labels: %w", err)
	}
	if _, err := r.s.execWrite(ctx, `UPDATE runs SET job_slug = ?, job_name = ? WHERE job_id = ?`, j.Slug, j.Name, j.ID); err != nil {
		return j, fmt.Errorf("refresh run job labels: %w", err)
	}
	return j, nil
}

func (r *JobRepo) SetEnabled(ctx context.Context, id string, enabled bool) error {
	res, err := r.s.execWrite(ctx, `UPDATE jobs SET enabled = ?, updated_at = ? WHERE id = ?`, boolToInt(enabled), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("set job enabled: %w", err)
	}
	return affected(res, "job", id)
}

func (r *JobRepo) Get(ctx context.Context, id string) (core.Job, error) {
	j, err := scanJob(r.s.queryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("job %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return j, fmt.Errorf("get job: %w", err)
	}
	list := []core.Job{j}
	if err := r.s.enrichJobs(ctx, list); err != nil {
		return j, err
	}
	return list[0], nil
}

func (r *JobRepo) GetBySlug(ctx context.Context, slug string) (core.Job, error) {
	j, err := scanJob(r.s.queryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return j, fmt.Errorf("job %s: %w", slug, ErrNotFound)
	}
	if err != nil {
		return j, fmt.Errorf("get job by slug: %w", err)
	}
	list := []core.Job{j}
	if err := r.s.enrichJobs(ctx, list); err != nil {
		return j, err
	}
	return list[0], nil
}

func (r *JobRepo) GetByIDOrSlug(ctx context.Context, ref string) (core.Job, error) {
	j, err := r.Get(ctx, ref)
	if err == nil {
		return j, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return j, err
	}
	return r.GetBySlug(ctx, ref)
}

func (r *JobRepo) SlugExists(ctx context.Context, slug, exceptID string) (bool, error) {
	var n int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM jobs WHERE slug = ? AND id <> ?`, slug, exceptID).Scan(&n); err != nil {
		return false, fmt.Errorf("check job slug: %w", err)
	}
	return n > 0, nil
}

type JobFilter struct {
	Enabled       *bool
	SourceID      string
	DestinationID string
	Tag           string
	Q             string
	Page          Page
}

func (r *JobRepo) List(ctx context.Context, f JobFilter) ([]core.Job, int, error) {
	w := &whereBuilder{}
	if f.Enabled != nil {
		w.add("enabled = ?", boolToInt(*f.Enabled))
	}
	if f.SourceID != "" {
		w.add("source_id = ?", f.SourceID)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		w.add("(name LIKE ? OR slug LIKE ? OR description LIKE ?)", "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	if f.Tag != "" {
		w.add("tags LIKE ?", "%\""+f.Tag+"\"%")
	}
	if f.DestinationID != "" {
		w.add("destination_ids LIKE ?", "%\""+f.DestinationID+"\"%")
	}
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM jobs`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count jobs: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+jobColumns+` FROM jobs`+w.sql()+` ORDER BY name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	out := []core.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := r.s.enrichJobs(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *JobRepo) All(ctx context.Context) ([]core.Job, error) {
	rows, err := r.s.query(ctx, `SELECT `+jobColumns+` FROM jobs ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list all jobs: %w", err)
	}
	defer rows.Close()
	out := []core.Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.s.enrichJobs(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *JobRepo) Delete(ctx context.Context, id string, deleteArtifacts bool) error {
	return r.s.tx(ctx, func(tx *sql.Tx) error {
		if deleteArtifacts {
			if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE job_id = ?`, id); err != nil {
				return fmt.Errorf("delete job artifacts: %w", err)
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("delete job: %w", err)
		}
		return affected(res, "job", id)
	})
}

func (s *Store) enrichJobs(ctx context.Context, jobs []core.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	sources, err := s.sourceLabels(ctx)
	if err != nil {
		return err
	}
	destinations, err := s.destinationLabels(ctx)
	if err != nil {
		return err
	}
	states, err := s.JobStates.All(ctx)
	if err != nil {
		return err
	}
	totals, err := s.artifactTotalsByJob(ctx)
	if err != nil {
		return err
	}
	for i := range jobs {
		j := &jobs[i]
		if lbl, ok := sources[j.SourceID]; ok {
			j.SourceName = lbl.Name
			j.SourceKind = lbl.Kind
		}
		names := make([]string, 0, len(j.DestinationIDs))
		for _, id := range j.DestinationIDs {
			if lbl, ok := destinations[id]; ok {
				names = append(names, lbl.Name)
			}
		}
		j.DestinationNames = names
		if st, ok := states[j.ID]; ok {
			j.LastRun = st.LastRun
			j.NextRunAt = st.NextRunAt
			j.Overdue = st.Overdue
		}
		if t, ok := totals[j.ID]; ok {
			j.ArtifactCount = t.Artifacts
			j.TotalBytes = t.Bytes
		}
	}
	return nil
}

type label struct {
	Name string
	Kind string
}

func (s *Store) sourceLabels(ctx context.Context) (map[string]label, error) {
	rows, err := s.query(ctx, `SELECT id, name, kind FROM sources`)
	if err != nil {
		return nil, fmt.Errorf("source labels: %w", err)
	}
	defer rows.Close()
	out := map[string]label{}
	for rows.Next() {
		var id string
		var l label
		if err := rows.Scan(&id, &l.Name, &l.Kind); err != nil {
			return nil, fmt.Errorf("scan source label: %w", err)
		}
		out[id] = l
	}
	return out, rows.Err()
}

func (s *Store) destinationLabels(ctx context.Context) (map[string]label, error) {
	rows, err := s.query(ctx, `SELECT id, name, kind FROM destinations`)
	if err != nil {
		return nil, fmt.Errorf("destination labels: %w", err)
	}
	defer rows.Close()
	out := map[string]label{}
	for rows.Next() {
		var id string
		var l label
		if err := rows.Scan(&id, &l.Name, &l.Kind); err != nil {
			return nil, fmt.Errorf("scan destination label: %w", err)
		}
		out[id] = l
	}
	return out, rows.Err()
}

func (s *Store) artifactTotalsByJob(ctx context.Context) (map[string]usageRow, error) {
	rows, err := s.query(ctx, `SELECT job_id, COALESCE(SUM(size), 0), COUNT(*) FROM artifacts WHERE status = ? GROUP BY job_id`, string(core.ArtifactPresent))
	if err != nil {
		return nil, fmt.Errorf("artifact totals: %w", err)
	}
	defer rows.Close()
	out := map[string]usageRow{}
	for rows.Next() {
		var id string
		var u usageRow
		if err := rows.Scan(&id, &u.Bytes, &u.Artifacts); err != nil {
			return nil, fmt.Errorf("scan artifact totals: %w", err)
		}
		out[id] = u
	}
	return out, rows.Err()
}
