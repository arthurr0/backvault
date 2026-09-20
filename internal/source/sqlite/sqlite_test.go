package sqlite

import (
	"bytes"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func makeDB(t *testing.T, rows int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("create table notes (id integer primary key, body text)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec("insert into notes (body) values (?)", "note"); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func countRows(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("select count(*) from notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("path should be required")
	}
	if err := d.Validate(core.Config{"path": "app.db"}); err == nil {
		t.Error("relative paths should be rejected")
	}
	if err := d.Validate(core.Config{"path": "/tmp/app.db", "method": "nope"}); err == nil {
		t.Error("unknown method should be rejected")
	}
}

func TestTest(t *testing.T) {
	path := makeDB(t, 3)
	if err := New().Test(t.Context(), core.Config{"path": path}, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	if err := New().Test(t.Context(), core.Config{"path": filepath.Join(t.TempDir(), "missing.db")}, testLogger()); err == nil {
		t.Error("a missing file should fail the test")
	}
}

func backupTo(t *testing.T, cfg core.Config) []byte {
	t.Helper()
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if st.Extension != "sqlite" {
		t.Errorf("extension %q", st.Extension)
	}
	data, err := io.ReadAll(st.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if st.Size != int64(len(data)) {
		t.Errorf("declared size %d, read %d", st.Size, len(data))
	}
	return data
}

func TestBackupWithVacuum(t *testing.T) {
	path := makeDB(t, 25)
	data := backupTo(t, core.Config{"path": path, "method": "vacuum"})
	if !bytes.HasPrefix(data, []byte(magic)) {
		t.Fatal("backup is not a sqlite file")
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, copyPath); n != 25 {
		t.Errorf("restored rows %d, want 25", n)
	}
}

func TestBackupWithSqlite3Binary(t *testing.T) {
	if _, err := os.Stat("/usr/bin/sqlite3"); err != nil {
		t.Skip("sqlite3 binary is not installed")
	}
	path := makeDB(t, 7)
	data := backupTo(t, core.Config{"path": path, "method": "sqlite3"})
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, copyPath); n != 7 {
		t.Errorf("restored rows %d, want 7", n)
	}
}

func TestBackupRemovesTempFiles(t *testing.T) {
	path := makeDB(t, 2)
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "backvault-sqlite-*"))
	_ = backupTo(t, core.Config{"path": path, "method": "vacuum"})
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "backvault-sqlite-*"))
	if len(after) != len(before) {
		t.Errorf("temp directories leaked: %v", after)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	path := makeDB(t, 11)
	data := backupTo(t, core.Config{"path": path, "method": "vacuum"})
	target := filepath.Join(t.TempDir(), "restored.db")
	d := New()
	if err := d.Restore(t.Context(), core.Config{"path": path}, bytes.NewReader(data), source.RestoreOptions{
		TargetPath: target, Extension: "sqlite",
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if n := countRows(t, target); n != 11 {
		t.Errorf("restored rows %d, want 11", n)
	}
}

func TestRestoreRefusesToOverwriteWithoutFlag(t *testing.T) {
	path := makeDB(t, 1)
	data := backupTo(t, core.Config{"path": path, "method": "vacuum"})
	d := New()
	err := d.Restore(t.Context(), core.Config{"path": path}, bytes.NewReader(data), source.RestoreOptions{
		TargetPath: path,
	}, testLogger())
	if err == nil {
		t.Fatal("restoring over an existing file without overwrite should fail")
	}
	if err := d.Restore(t.Context(), core.Config{"path": path}, bytes.NewReader(data), source.RestoreOptions{
		TargetPath: path, Overwrite: true,
	}, testLogger()); err != nil {
		t.Fatalf("overwrite restore: %v", err)
	}
}

func TestRestoreRejectsNonSqliteStream(t *testing.T) {
	target := filepath.Join(t.TempDir(), "restored.db")
	err := New().Restore(t.Context(), core.Config{}, bytes.NewReader([]byte("not a database")), source.RestoreOptions{
		TargetPath: target,
	}, testLogger())
	if err == nil {
		t.Fatal("a non-sqlite stream should be refused")
	}
}

func TestRemoteCopyScript(t *testing.T) {
	got := copyScript("sqlite3", "/var/lib/my app/app.db", "auto")
	want := `umask 077; f=$(mktemp) || exit 1; sqlite3 '/var/lib/my app/app.db' ".backup '$f'" && cat "$f"; rc=$?; rm -f "$f"; exit $rc`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	vacuum := copyScript("sqlite3", "/app.db", "vacuum")
	if !strings.Contains(vacuum, `rm -f "$f"; sqlite3 /app.db "VACUUM INTO '$f'" && cat "$f"`) {
		t.Errorf("got %q", vacuum)
	}
}

func TestRemoteRestoreScript(t *testing.T) {
	got := restoreScript("/var/lib/app/app.db")
	for _, part := range []string{
		"umask 077; mkdir -p /var/lib/app && f=$(mktemp /var/lib/app/.backvault-restore-XXXXXX) || exit 1;",
		`cat > "$f" && chmod 644 "$f" && rm -f /var/lib/app/app.db-wal /var/lib/app/app.db-shm && mv "$f" /var/lib/app/app.db`,
		`rc=$?; rm -f "$f"; exit $rc`,
	} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %q in %q", part, got)
		}
	}
}
