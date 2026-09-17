package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type LogChunk struct {
	Seq  int64
	Text string
}

func (r *RunRepo) AppendLog(ctx context.Context, runID string, text string) (int64, error) {
	var seq int64
	err := r.s.tx(ctx, func(tx *sql.Tx) error {
		var next sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM run_logs WHERE run_id = ?`, runID).Scan(&next); err != nil {
			return fmt.Errorf("next log sequence: %w", err)
		}
		seq = next.Int64 + 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO run_logs (run_id, seq, chunk) VALUES (?, ?, ?)`, runID, seq, text); err != nil {
			return fmt.Errorf("append run log: %w", err)
		}
		return nil
	})
	return seq, err
}

func (r *RunRepo) LogChunks(ctx context.Context, runID string, afterSeq int64) ([]LogChunk, error) {
	rows, err := r.s.query(ctx, `SELECT seq, chunk FROM run_logs WHERE run_id = ? AND seq > ? ORDER BY seq`, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("read run log: %w", err)
	}
	defer rows.Close()
	out := []LogChunk{}
	for rows.Next() {
		var c LogChunk
		if err := rows.Scan(&c.Seq, &c.Text); err != nil {
			return nil, fmt.Errorf("scan run log: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *RunRepo) Log(ctx context.Context, runID string) (string, error) {
	chunks, err := r.LogChunks(ctx, runID, 0)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(c.Text)
	}
	return sb.String(), nil
}
