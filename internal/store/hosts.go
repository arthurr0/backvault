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

type HostRepo struct{ s *Store }

const hostColumns = `id, name, description, address, port, "user", auth, private_key, key_passphrase, password, public_key, host_key, sudo, connect_timeout, tags, created_at, updated_at, last_test_at, last_test_ok, last_test_error, last_seen_os, tools`

func scanHost(sc interface{ Scan(...any) error }) (core.Host, error) {
	var (
		h        core.Host
		auth     string
		sudo     int64
		tags     string
		tools    string
		created  sql.NullString
		updated  sql.NullString
		testedAt sql.NullString
		testedOK sql.NullInt64
	)
	err := sc.Scan(&h.ID, &h.Name, &h.Description, &h.Address, &h.Port, &h.User, &auth,
		&h.PrivateKey, &h.KeyPassphrase, &h.Password, &h.PublicKey, &h.HostKey, &sudo, &h.ConnectTimeout,
		&tags, &created, &updated, &testedAt, &testedOK, &h.LastTestError, &h.LastSeenOS, &tools)
	if err != nil {
		return h, err
	}
	h.Auth = core.HostAuth(auth)
	h.Sudo = sudo != 0
	h.Tags = decodeStrings(tags)
	h.Tools = decodeStrings(tools)
	h.CreatedAt = scanTime(created)
	h.UpdatedAt = scanTime(updated)
	h.LastTestAt = scanTimePtr(testedAt)
	h.LastTestOK = scanBoolPtr(testedOK)
	return h, nil
}

func normalizeHost(h core.Host) core.Host {
	h.Name = strings.TrimSpace(h.Name)
	h.Address = strings.TrimSpace(h.Address)
	h.User = strings.TrimSpace(h.User)
	h.HostKey = strings.TrimSpace(h.HostKey)
	if h.Auth == "" {
		h.Auth = core.HostAuthKey
	}
	if h.Tags == nil {
		h.Tags = []string{}
	}
	if h.Tools == nil {
		h.Tools = []string{}
	}
	return h
}

func (r *HostRepo) Create(ctx context.Context, h core.Host) (core.Host, error) {
	now := time.Now().UTC()
	h = normalizeHost(h)
	if h.ID == "" {
		h.ID = NewID()
	}
	h.CreatedAt = now
	h.UpdatedAt = now
	_, err := r.s.execWrite(ctx, `INSERT INTO hosts (`+hostColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.Name, h.Description, h.Address, h.Port, h.User, string(h.Auth),
		h.PrivateKey, h.KeyPassphrase, h.Password, h.PublicKey, h.HostKey, boolToInt(h.Sudo), h.ConnectTimeout,
		mustJSON(h.Tags), formatTime(h.CreatedAt), formatTime(h.UpdatedAt),
		nullTime(h.LastTestAt), nullBool(h.LastTestOK), h.LastTestError, h.LastSeenOS, mustJSON(h.Tools))
	if err != nil {
		if isUniqueViolation(err) {
			return h, fmt.Errorf("host name %q already exists: %w", h.Name, ErrConflict)
		}
		return h, fmt.Errorf("create host: %w", err)
	}
	return h, nil
}

func (r *HostRepo) Update(ctx context.Context, h core.Host) (core.Host, error) {
	h = normalizeHost(h)
	h.UpdatedAt = time.Now().UTC()
	res, err := r.s.execWrite(ctx, `UPDATE hosts SET name = ?, description = ?, address = ?, port = ?, "user" = ?, auth = ?, private_key = ?, key_passphrase = ?, password = ?, public_key = ?, host_key = ?, sudo = ?, connect_timeout = ?, tags = ?, updated_at = ? WHERE id = ?`,
		h.Name, h.Description, h.Address, h.Port, h.User, string(h.Auth),
		h.PrivateKey, h.KeyPassphrase, h.Password, h.PublicKey, h.HostKey, boolToInt(h.Sudo), h.ConnectTimeout,
		mustJSON(h.Tags), formatTime(h.UpdatedAt), h.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return h, fmt.Errorf("host name %q already exists: %w", h.Name, ErrConflict)
		}
		return h, fmt.Errorf("update host: %w", err)
	}
	return h, affected(res, "host", h.ID)
}

func (r *HostRepo) Get(ctx context.Context, id string) (core.Host, error) {
	h, err := scanHost(r.s.queryRow(ctx, `SELECT `+hostColumns+` FROM hosts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return h, fmt.Errorf("host %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return h, fmt.Errorf("get host: %w", err)
	}
	counts, err := r.s.sourceCountsByHost(ctx)
	if err != nil {
		return h, err
	}
	h.SourceCount = counts[h.ID]
	return h, nil
}

func (r *HostRepo) GetByName(ctx context.Context, name string) (core.Host, error) {
	h, err := scanHost(r.s.queryRow(ctx, `SELECT `+hostColumns+` FROM hosts WHERE name = ?`, strings.TrimSpace(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return h, fmt.Errorf("host %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return h, fmt.Errorf("get host by name: %w", err)
	}
	counts, err := r.s.sourceCountsByHost(ctx)
	if err != nil {
		return h, err
	}
	h.SourceCount = counts[h.ID]
	return h, nil
}

type HostFilter struct {
	Q    string
	Tag  string
	Page Page
}

func (r *HostRepo) List(ctx context.Context, f HostFilter) ([]core.Host, int, error) {
	w := &whereBuilder{}
	if q := strings.TrimSpace(f.Q); q != "" {
		w.add(`(name LIKE ? OR description LIKE ? OR address LIKE ?)`, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	if tag := strings.TrimSpace(f.Tag); tag != "" {
		w.add("tags LIKE ?", "%\""+tag+"\"%")
	}
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM hosts`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count hosts: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+hostColumns+` FROM hosts`+w.sql()+` ORDER BY name LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list hosts: %w", err)
	}
	defer rows.Close()
	out := []core.Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan host: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	counts, err := r.s.sourceCountsByHost(ctx)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i].SourceCount = counts[out[i].ID]
	}
	return out, total, nil
}

func (r *HostRepo) Delete(ctx context.Context, id string) error {
	counts, err := r.s.sourceCountsByHost(ctx)
	if err != nil {
		return err
	}
	if counts[id] > 0 {
		return fmt.Errorf("host %s is used by sources: %w", id, ErrConflict)
	}
	res, err := r.s.execWrite(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete host: %w", err)
	}
	return affected(res, "host", id)
}

func (r *HostRepo) SetTestResult(ctx context.Context, id string, ok bool, message, os string, tools []string) error {
	if tools == nil {
		tools = []string{}
	}
	res, err := r.s.execWrite(ctx, `UPDATE hosts SET last_test_at = ?, last_test_ok = ?, last_test_error = ?, last_seen_os = ?, tools = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), boolToInt(ok), message, os, mustJSON(tools), id)
	if err != nil {
		return fmt.Errorf("store host test result: %w", err)
	}
	return affected(res, "host", id)
}

func (s *Store) sourceCountsByHost(ctx context.Context) (map[string]int, error) {
	rows, err := s.query(ctx, `SELECT host_id, COUNT(*) FROM sources WHERE host_id <> '' GROUP BY host_id`)
	if err != nil {
		return nil, fmt.Errorf("count sources by host: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan source count: %w", err)
		}
		out[id] = n
	}
	return out, rows.Err()
}
