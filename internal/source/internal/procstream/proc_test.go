package procstream

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type capture struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	logs *slog.Logger
}

func newCapture() *capture {
	c := &capture{}
	c.logs = slog.New(slog.NewTextHandler(&syncWriter{c: c}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return c
}

type syncWriter struct{ c *capture }

func (w *syncWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.buf.Write(p)
}

func (c *capture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func TestStartStreamsStdoutAndCloseSucceeds(t *testing.T) {
	c := newCapture()
	p, err := Start(t.Context(), c.logs, Command{Name: "/bin/sh", Args: []string{"-c", "printf hello"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	out, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) != "hello" {
		t.Fatalf("got %q, want hello", out)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestCloseReturnsErrorOnNonZeroExit(t *testing.T) {
	c := newCapture()
	p, err := Start(t.Context(), c.logs, Command{
		Name: "/bin/sh",
		Args: []string{"-c", "printf partial; echo 'disk is on fire' >&2; exit 3"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := io.ReadAll(p); err != nil {
		t.Fatalf("read: %v", err)
	}
	err = p.Close()
	if err == nil {
		t.Fatal("close should report the non-zero exit code")
	}
	if !strings.Contains(err.Error(), "code 3") {
		t.Errorf("error %q does not mention the exit code", err)
	}
	if !strings.Contains(err.Error(), "disk is on fire") {
		t.Errorf("error %q does not include the last stderr lines", err)
	}
	if !strings.Contains(c.text(), "disk is on fire") {
		t.Error("stderr was not forwarded to the logger")
	}
}

func TestCloseBeforeEOFStopsTheProcess(t *testing.T) {
	c := newCapture()
	p, err := Start(t.Context(), c.logs, Command{Name: "/bin/sh", Args: []string{"-c", "yes backvault"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	buf := make([]byte, 16)
	if _, err := io.ReadFull(p, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := p.Close(); err == nil {
		t.Fatal("close should report that the dump did not complete")
	}
}

func TestContextCancellationKillsTheProcess(t *testing.T) {
	c := newCapture()
	ctx, cancel := context.WithCancel(t.Context())
	p, err := Start(ctx, c.logs, Command{Name: "/bin/sh", Args: []string{"-c", "sleep 30"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	cancel()
	if _, err := io.ReadAll(p); err != nil && err != io.EOF {
		t.Logf("read returned %v", err)
	}
	if err := p.Close(); err == nil {
		t.Fatal("close should report the cancellation")
	}
}

func TestSecretsAreScrubbedFromLogsAndErrors(t *testing.T) {
	c := newCapture()
	secret := "sup3rs3cret"
	p, err := Start(t.Context(), c.logs, Command{
		Name:   "/bin/sh",
		Args:   []string{"-c", "echo 'password=" + secret + "' >&2; exit 1"},
		Redact: []string{secret},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_, _ = io.ReadAll(p)
	err = p.Close()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("secret leaked into the error: %v", err)
	}
	if strings.Contains(c.text(), secret) {
		t.Error("secret leaked into the log")
	}
}

func TestCleanupRunsAfterClose(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCapture()
	p, err := Start(t.Context(), c.logs, Command{
		Name:    "/bin/sh",
		Args:    []string{"-c", "printf ok"},
		Cleanup: []func(){func() { _ = os.Remove(marker) }},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	_, _ = io.ReadAll(p)
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("cleanup did not run")
	}
}

func TestLookupPrefersBinaryPath(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "pg_dump")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Lookup(dir, "pg_dump")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != fake {
		t.Errorf("got %q, want %q", got, fake)
	}
	if got, err = Lookup(fake, "pg_dump"); err != nil || got != fake {
		t.Errorf("lookup by file: %q %v", got, err)
	}
	if _, err := Lookup("", "backvault-does-not-exist"); err == nil {
		t.Error("expected an error for a missing binary")
	}
}

func TestFileStreamRemovesTempDirOnClose(t *testing.T) {
	tmp, err := NewTempDir("test")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.File("data")
	if err := os.WriteFile(path, []byte("backvault"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, size, err := FileStream(path, tmp.Remove)
	if err != nil {
		t.Fatal(err)
	}
	if size != 9 {
		t.Errorf("size %d, want 9", size)
	}
	data, _ := io.ReadAll(r)
	if string(data) != "backvault" {
		t.Errorf("got %q", data)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp.Path); !os.IsNotExist(err) {
		t.Error("temp dir was not removed")
	}
}
