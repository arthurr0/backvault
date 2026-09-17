package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	IP         string
	UserAgent  string
}

type SessionRepo struct{ s *Store }

func (r *SessionRepo) Create(ctx context.Context, sess Session) error {
	_, err := r.s.execWrite(ctx, `INSERT INTO sessions (id, user_id, created_at, expires_at, last_seen_at, ip, user_agent) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, formatTime(sess.CreatedAt), formatTime(sess.ExpiresAt), formatTime(sess.LastSeenAt), sess.IP, sess.UserAgent)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepo) Get(ctx context.Context, id string) (Session, error) {
	var (
		sess    Session
		created sql.NullString
		expires sql.NullString
		seen    sql.NullString
	)
	err := r.s.queryRow(ctx, `SELECT id, user_id, created_at, expires_at, last_seen_at, ip, user_agent FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.UserID, &created, &expires, &seen, &sess.IP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, fmt.Errorf("session: %w", ErrNotFound)
	}
	if err != nil {
		return sess, fmt.Errorf("get session: %w", err)
	}
	sess.CreatedAt = scanTime(created)
	sess.ExpiresAt = scanTime(expires)
	sess.LastSeenAt = scanTime(seen)
	return sess, nil
}

func (r *SessionRepo) Touch(ctx context.Context, id string, seen, expires time.Time) error {
	if _, err := r.s.execWrite(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, formatTime(seen), formatTime(expires), id); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *SessionRepo) Delete(ctx context.Context, id string) error {
	if _, err := r.s.execWrite(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *SessionRepo) DeleteForUser(ctx context.Context, userID string) error {
	if _, err := r.s.execWrite(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

func (r *SessionRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.s.execWrite(ctx, `DELETE FROM sessions WHERE expires_at < ?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
