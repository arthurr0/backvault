package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type AuditRepo struct{ s *Store }

const auditColumns = `id, time, actor_id, actor_label, action, object_type, object_id, object_name, details, ip`

func scanAudit(sc interface{ Scan(...any) error }) (core.AuditEntry, error) {
	var (
		e       core.AuditEntry
		ts      sql.NullString
		details string
	)
	if err := sc.Scan(&e.ID, &ts, &e.ActorID, &e.ActorLabel, &e.Action, &e.ObjectType, &e.ObjectID, &e.ObjectName, &details, &e.IP); err != nil {
		return e, err
	}
	e.Time = scanTime(ts)
	_ = decodeJSON(details, &e.Details)
	return e, nil
}

func (r *AuditRepo) Record(ctx context.Context, e core.AuditEntry) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	_, err := r.s.execWrite(ctx, `INSERT INTO audit_log (`+auditColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, formatTime(e.Time), e.ActorID, e.ActorLabel, e.Action, e.ObjectType, e.ObjectID, e.ObjectName, mustJSON(e.Details), e.IP)
	if err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}

type AuditFilter struct {
	Actor      string
	Action     string
	ObjectType string
	Since      time.Time
	Page       Page
}

func (r *AuditRepo) List(ctx context.Context, f AuditFilter) ([]core.AuditEntry, int, error) {
	w := &whereBuilder{}
	if f.Actor != "" {
		w.add("(actor_id = ? OR actor_label LIKE ?)", f.Actor, "%"+f.Actor+"%")
	}
	if f.Action != "" {
		w.add("action = ?", f.Action)
	}
	if f.ObjectType != "" {
		w.add("object_type = ?", f.ObjectType)
	}
	w.addTime("time >= ?", f.Since)
	var total int
	if err := r.s.queryRow(ctx, `SELECT COUNT(*) FROM audit_log`+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit entries: %w", err)
	}
	p := f.Page.normalized()
	args := append(append([]any{}, w.args...), p.Limit, p.Offset)
	rows, err := r.s.query(ctx, `SELECT `+auditColumns+` FROM audit_log`+w.sql()+` ORDER BY time DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()
	out := []core.AuditEntry{}
	for rows.Next() {
		e, err := scanAudit(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan audit entry: %w", err)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (r *AuditRepo) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.s.execWrite(ctx, `DELETE FROM audit_log WHERE time < ?`, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("delete old audit entries: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}
