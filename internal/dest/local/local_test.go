package local

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/dest"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func open(t *testing.T, cfg core.Config) dest.Client {
	t.Helper()
	c, err := New().Open(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestValidateRejectsRelativePath(t *testing.T) {
	if err := New().Validate(core.Config{"path": "relative/dir"}); err == nil {
		t.Error("relative paths should be rejected")
	}
	if err := New().Validate(core.Config{"path": "/tmp/x", "dir_mode": "not-octal"}); err == nil {
		t.Error("invalid modes should be rejected")
	}
}

func TestPutGetStatListDelete(t *testing.T) {
	root := t.TempDir()
	c := open(t, core.Config{"path": root})
	if err := c.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}
	payload := strings.Repeat("backvault", 1000)
	if err := c.Put(t.Context(), "job/job-20240101-000000.tar", strings.NewReader(payload), int64(len(payload))); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := c.Get(t.Context(), "job/job-20240101-000000.tar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != payload {
		t.Error("round trip mismatch")
	}
	st, err := c.Stat(t.Context(), "job/job-20240101-000000.tar")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size != int64(len(payload)) {
		t.Errorf("stat size %d, want %d", st.Size, len(payload))
	}
	if st.Path != "job/job-20240101-000000.tar" {
		t.Errorf("stat path %q", st.Path)
	}
	items, err := c.List(t.Context(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var files []string
	for _, item := range items {
		if !item.IsDir {
			files = append(files, item.Path)
		}
	}
	if len(files) != 1 || files[0] != "job/job-20240101-000000.tar" {
		t.Errorf("list returned %v", files)
	}
	if err := c.Delete(t.Context(), "job/job-20240101-000000.tar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := c.Delete(t.Context(), "job/job-20240101-000000.tar"); err != nil {
		t.Errorf("delete of a missing object should return nil, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "job")); !os.IsNotExist(err) {
		t.Error("empty directory should have been pruned")
	}
}

func TestPutIsAtomicAndLeavesNoPartials(t *testing.T) {
	root := t.TempDir()
	c := open(t, core.Config{"path": root})
	if err := c.Put(t.Context(), "a/b/c.bin", strings.NewReader("data"), 4); err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "c.bin" {
		t.Errorf("unexpected directory contents %v", entries)
	}
}

func TestPutRejectsShortWrite(t *testing.T) {
	root := t.TempDir()
	c := open(t, core.Config{"path": root})
	err := c.Put(t.Context(), "short.bin", strings.NewReader("ab"), 10)
	if err == nil {
		t.Fatal("a short write should fail")
	}
	if _, statErr := os.Stat(filepath.Join(root, "short.bin")); !os.IsNotExist(statErr) {
		t.Error("a failed put should not leave the target file behind")
	}
}

func TestPathTraversalIsRefused(t *testing.T) {
	root := t.TempDir()
	c := open(t, core.Config{"path": root})
	if err := c.Put(t.Context(), "../escape.bin", strings.NewReader("x"), 1); err == nil {
		t.Error("path traversal should be refused")
	}
	if _, err := c.Stat(t.Context(), "../../etc/passwd"); err == nil {
		t.Error("path traversal should be refused")
	}
}

func TestOverwriteReplacesTheObject(t *testing.T) {
	root := t.TempDir()
	c := open(t, core.Config{"path": root})
	if err := c.Put(t.Context(), "x.bin", strings.NewReader("first"), 5); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(t.Context(), "x.bin", strings.NewReader("second"), 6); err != nil {
		t.Fatal(err)
	}
	rc, err := c.Get(t.Context(), "x.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "second" {
		t.Errorf("got %q, want second", got)
	}
}

func TestTestCreatesTheDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "backups")
	c := open(t, core.Config{"path": root})
	if err := c.Test(t.Context()); err != nil {
		t.Fatalf("test: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory was not created: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("test left files behind: %v", entries)
	}
}

func TestListOfMissingPrefixIsEmpty(t *testing.T) {
	c := open(t, core.Config{"path": t.TempDir()})
	items, err := c.List(t.Context(), "nope")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected no items, got %v", items)
	}
}
