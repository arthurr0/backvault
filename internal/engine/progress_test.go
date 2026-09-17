package engine

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type captureHandler struct {
	lines *[]string
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		b.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	*h.lines = append(*h.lines, b.String())
	return nil
}

func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h captureHandler) WithGroup(string) slog.Handler { return h }

func TestProgressReaderReportsPercentAndRate(t *testing.T) {
	var lines []string
	log := slog.New(captureHandler{lines: &lines})
	payload := bytes.Repeat([]byte("x"), 4096)
	p := newProgressReader(bytes.NewReader(payload), int64(len(payload)), 0, log, "Storage box")
	n, err := io.Copy(io.Discard, io.LimitReader(p, 2048))
	if err != nil || n != 2048 {
		t.Fatalf("copy: n=%d err=%v", n, err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := io.Copy(io.Discard, p); err != nil {
		t.Fatal(err)
	}
	if p.Done() != int64(len(payload)) {
		t.Fatalf("done %d, want %d", p.Done(), len(payload))
	}
	if len(lines) == 0 {
		t.Fatal("expected progress lines")
	}
	last := lines[len(lines)-1]
	for _, want := range []string{"upload progress", "destination=Storage box", "total=4.0 KiB", "percent=100.0", "rate="} {
		if !strings.Contains(last, want) {
			t.Errorf("line %q lacks %q", last, want)
		}
	}
}

func TestProgressReaderRespectsInterval(t *testing.T) {
	var lines []string
	log := slog.New(captureHandler{lines: &lines})
	p := newProgressReader(bytes.NewReader(bytes.Repeat([]byte("x"), 1<<16)), 1<<16, time.Hour, log, "d")
	if _, err := io.Copy(io.Discard, p); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no progress lines within the interval, got %d", len(lines))
	}
}
