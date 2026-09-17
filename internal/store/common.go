package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

const timeLayout = "2006-01-02T15:04:05.000000000Z"

func NewID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.Reader).String()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return formatTime(*t)
}

func parseTime(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{timeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse time %q", v)
}

func scanTime(v sql.NullString) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	t, err := parseTime(v.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func scanTimePtr(v sql.NullString) *time.Time {
	t := scanTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

func encodeJSON(v any) (string, error) {
	if v == nil {
		return "null", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encode json: %w", err)
	}
	return string(b), nil
}

func mustJSON(v any) string {
	s, err := encodeJSON(v)
	if err != nil {
		return "null"
	}
	return s
}

func decodeJSON(raw string, dst any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func decodeStrings(raw string) []string {
	var out []string
	_ = decodeJSON(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func decodeMap(raw string) map[string]string {
	var out map[string]string
	_ = decodeJSON(raw, &out)
	return out
}

type Page struct {
	Limit  int
	Offset int
}

func (p Page) normalized() Page {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return Page{Limit: limit, Offset: offset}
}

type whereBuilder struct {
	clauses []string
	args    []any
}

func (w *whereBuilder) add(clause string, args ...any) {
	w.clauses = append(w.clauses, clause)
	w.args = append(w.args, args...)
}

func (w *whereBuilder) addTime(clause string, t time.Time) {
	if t.IsZero() {
		return
	}
	w.add(clause, formatTime(t))
}

func (w *whereBuilder) sql() string {
	if len(w.clauses) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.clauses, " AND ")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullBool(v *bool) any {
	if v == nil {
		return nil
	}
	return boolToInt(*v)
}

func scanBoolPtr(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}
