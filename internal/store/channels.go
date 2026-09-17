package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type ChannelRepo struct{ s *Store }

const channelColumns = `id, name, kind, config, enabled, events, created_at, updated_at, last_sent_at, last_error`

func scanChannel(sc interface{ Scan(...any) error }) (core.NotificationChannel, error) {
	var (
		c        core.NotificationChannel
		cfg      string
		enabled  int
		events   string
		created  sql.NullString
		updated  sql.NullString
		lastSent sql.NullString
	)
	if err := sc.Scan(&c.ID, &c.Name, &c.Kind, &cfg, &enabled, &events, &created, &updated, &lastSent, &c.LastError); err != nil {
		return c, err
	}
	c.Config = core.Config{}
	if err := decodeJSON(cfg, &c.Config); err != nil {
		return c, err
	}
	c.Enabled = enabled != 0
	c.Events = decodeStrings(events)
	c.CreatedAt = scanTime(created)
	c.UpdatedAt = scanTime(updated)
	c.LastSentAt = scanTimePtr(lastSent)
	return c, nil
}

func (r *ChannelRepo) Create(ctx context.Context, c core.NotificationChannel) (core.NotificationChannel, error) {
	now := time.Now().UTC()
	if c.ID == "" {
		c.ID = NewID()
	}
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Events == nil {
		c.Events = []string{}
	}
	cfg, err := encodeJSON(c.Config)
	if err != nil {
		return c, err
	}
	_, err = r.s.execWrite(ctx, `INSERT INTO notification_channels (`+channelColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Kind, cfg, boolToInt(c.Enabled), mustJSON(c.Events),
		formatTime(c.CreatedAt), formatTime(c.UpdatedAt), nullTime(c.LastSentAt), c.LastError)
	if err != nil {
		if isUniqueViolation(err) {
			return c, fmt.Errorf("channel name %q already exists: %w", c.Name, ErrConflict)
		}
		return c, fmt.Errorf("create channel: %w", err)
	}
	return c, nil
}

func (r *ChannelRepo) Update(ctx context.Context, c core.NotificationChannel) (core.NotificationChannel, error) {
	c.UpdatedAt = time.Now().UTC()
	if c.Events == nil {
		c.Events = []string{}
	}
	cfg, err := encodeJSON(c.Config)
	if err != nil {
		return c, err
	}
	res, err := r.s.execWrite(ctx, `UPDATE notification_channels SET name = ?, kind = ?, config = ?, enabled = ?, events = ?, updated_at = ? WHERE id = ?`,
		c.Name, c.Kind, cfg, boolToInt(c.Enabled), mustJSON(c.Events), formatTime(c.UpdatedAt), c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return c, fmt.Errorf("channel name %q already exists: %w", c.Name, ErrConflict)
		}
		return c, fmt.Errorf("update channel: %w", err)
	}
	return c, affected(res, "channel", c.ID)
}

func (r *ChannelRepo) SetSendResult(ctx context.Context, id string, at time.Time, sendErr string) error {
	if _, err := r.s.execWrite(ctx, `UPDATE notification_channels SET last_sent_at = ?, last_error = ? WHERE id = ?`, formatTime(at), sendErr, id); err != nil {
		return fmt.Errorf("store channel send result: %w", err)
	}
	return nil
}

func (r *ChannelRepo) Get(ctx context.Context, id string) (core.NotificationChannel, error) {
	c, err := scanChannel(r.s.queryRow(ctx, `SELECT `+channelColumns+` FROM notification_channels WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("channel %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return c, fmt.Errorf("get channel: %w", err)
	}
	return c, nil
}

func (r *ChannelRepo) List(ctx context.Context) ([]core.NotificationChannel, error) {
	rows, err := r.s.query(ctx, `SELECT `+channelColumns+` FROM notification_channels ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list channels: %w", err)
	}
	defer rows.Close()
	out := []core.NotificationChannel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan channel: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ChannelRepo) Delete(ctx context.Context, id string) error {
	res, err := r.s.execWrite(ctx, `DELETE FROM notification_channels WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	return affected(res, "channel", id)
}
