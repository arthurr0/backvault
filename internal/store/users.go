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

type UserRecord struct {
	core.User
	PasswordHash string
}

type UserRepo struct{ s *Store }

const userColumns = `id, email, name, role, password_hash, created_at, last_login_at`

func scanUser(sc interface{ Scan(...any) error }) (UserRecord, error) {
	var (
		rec      UserRecord
		created  sql.NullString
		lastSeen sql.NullString
	)
	if err := sc.Scan(&rec.ID, &rec.Email, &rec.Name, &rec.Role, &rec.PasswordHash, &created, &lastSeen); err != nil {
		return rec, err
	}
	rec.CreatedAt = scanTime(created)
	rec.LastLoginAt = scanTimePtr(lastSeen)
	return rec, nil
}

func (r *UserRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func (r *UserRepo) CountAdmins(ctx context.Context) (int, error) {
	var n int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = ?`, string(core.RoleAdmin)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

func (r *UserRepo) Create(ctx context.Context, u core.User, passwordHash string) (core.User, error) {
	if u.ID == "" {
		u.ID = NewID()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	u.Email = normalizeEmail(u.Email)
	_, err := r.s.execWrite(ctx, `INSERT INTO users (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Name, string(u.Role), passwordHash, formatTime(u.CreatedAt), nullTime(u.LastLoginAt))
	if err != nil {
		if isUniqueViolation(err) {
			return u, fmt.Errorf("user %s: %w", u.Email, ErrConflict)
		}
		return u, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (r *UserRepo) Get(ctx context.Context, id string) (UserRecord, error) {
	rec, err := scanUser(r.s.queryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return rec, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return rec, fmt.Errorf("get user: %w", err)
	}
	return rec, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (UserRecord, error) {
	rec, err := scanUser(r.s.queryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, normalizeEmail(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return rec, fmt.Errorf("user %s: %w", email, ErrNotFound)
	}
	if err != nil {
		return rec, fmt.Errorf("get user by email: %w", err)
	}
	return rec, nil
}

func (r *UserRepo) List(ctx context.Context) ([]core.User, error) {
	rows, err := r.s.query(ctx, `SELECT `+userColumns+` FROM users ORDER BY email`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	out := []core.User{}
	for rows.Next() {
		rec, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		out = append(out, rec.User)
	}
	return out, rows.Err()
}

func (r *UserRepo) Update(ctx context.Context, u core.User) error {
	res, err := r.s.execWrite(ctx, `UPDATE users SET email = ?, name = ?, role = ? WHERE id = ?`,
		normalizeEmail(u.Email), u.Name, string(u.Role), u.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("user %s: %w", u.Email, ErrConflict)
		}
		return fmt.Errorf("update user: %w", err)
	}
	return affected(res, "user", u.ID)
}

func (r *UserRepo) SetPassword(ctx context.Context, id, hash string) error {
	res, err := r.s.execWrite(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return affected(res, "user", id)
}

func (r *UserRepo) TouchLogin(ctx context.Context, id string, at time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, formatTime(at), id); err != nil {
		return fmt.Errorf("touch login: %w", err)
	}
	return nil
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return affected(res, "user", id)
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func affected(res sql.Result, kind, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s %s: %w", kind, id, ErrNotFound)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "constraint failed: unique")
}

func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "foreign key constraint")
}
