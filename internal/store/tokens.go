package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type TokenRepo struct{ s *Store }

const tokenColumns = `id, name, prefix, scopes, job_slugs, created_at, created_by, last_used_at, expires_at`

func scanToken(sc interface{ Scan(...any) error }) (core.APIToken, error) {
	var (
		t        core.APIToken
		scopes   string
		slugs    string
		created  sql.NullString
		lastUsed sql.NullString
		expires  sql.NullString
	)
	if err := sc.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &slugs, &created, &t.CreatedBy, &lastUsed, &expires); err != nil {
		return t, err
	}
	t.Scopes = decodeStrings(scopes)
	t.JobSlugs = decodeStrings(slugs)
	t.CreatedAt = scanTime(created)
	t.LastUsedAt = scanTimePtr(lastUsed)
	t.ExpiresAt = scanTimePtr(expires)
	return t, nil
}

func (r *TokenRepo) Create(ctx context.Context, t core.APIToken, hash string) (core.APIToken, error) {
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	if t.Scopes == nil {
		t.Scopes = []string{}
	}
	if t.JobSlugs == nil {
		t.JobSlugs = []string{}
	}
	_, err := r.s.execWrite(ctx, `INSERT INTO api_tokens (id, name, prefix, hash, scopes, job_slugs, created_at, created_by, last_used_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Prefix, hash, mustJSON(t.Scopes), mustJSON(t.JobSlugs), formatTime(t.CreatedAt), t.CreatedBy, nullTime(t.LastUsedAt), nullTime(t.ExpiresAt))
	if err != nil {
		return t, fmt.Errorf("create token: %w", err)
	}
	return t, nil
}

func (r *TokenRepo) GetByHash(ctx context.Context, hash string) (core.APIToken, error) {
	t, err := scanToken(r.s.queryRow(ctx, `SELECT `+tokenColumns+` FROM api_tokens WHERE hash = ?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("token: %w", ErrNotFound)
	}
	if err != nil {
		return t, fmt.Errorf("get token: %w", err)
	}
	return t, nil
}

func (r *TokenRepo) List(ctx context.Context) ([]core.APIToken, error) {
	rows, err := r.s.query(ctx, `SELECT `+tokenColumns+` FROM api_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	out := []core.APIToken{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TokenRepo) Get(ctx context.Context, id string) (core.APIToken, error) {
	t, err := scanToken(r.s.queryRow(ctx, `SELECT `+tokenColumns+` FROM api_tokens WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("token %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return t, fmt.Errorf("get token: %w", err)
	}
	return t, nil
}

func (r *TokenRepo) TouchUsed(ctx context.Context, id string, at time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, formatTime(at), id); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

func (r *TokenRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM api_tokens WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return affected(res, "token", id)
}
