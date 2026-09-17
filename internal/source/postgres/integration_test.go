package postgres

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

const pgPassword = "backvaultpgpass"

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func requirePostgres(t *testing.T) {
	t.Helper()
	requireDocker(t)
	requireTools(t, []string{"psql"}, []string{"pg_dump"}, []string{"pg_dumpall"}, []string{"pg_restore"})
}

func postgresImage() string {
	if img := os.Getenv("BACKVAULT_TEST_POSTGRES_IMAGE"); img != "" {
		return img
	}
	return "postgres:18-alpine"
}

func startPostgres(t *testing.T) int {
	t.Helper()
	port := freePort(t)
	startContainer(t, "backvault-postgres-",
		"-p", publish(port, 5432),
		"-e", "POSTGRES_PASSWORD="+pgPassword,
		"-e", "POSTGRES_DB=app",
		postgresImage())
	waitForPostgres(t, port)
	return port
}

func waitForPostgres(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		cmd := exec.Command("psql", "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres",
			"-d", "postgres", "-w", "-tAX", "-c", "select 1")
		cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
		out, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return
		}
		last = string(out)
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres did not become ready: %s", last)
}

func psql(t *testing.T, port int, db, statement string) string {
	t.Helper()
	cmd := exec.Command("psql", "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres",
		"-d", db, "-w", "-tAX", "-v", "ON_ERROR_STOP=1", "-c", statement)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("psql %q: %v: %s", statement, err, out)
	}
	return strings.TrimSpace(string(out))
}

func seed(t *testing.T, port int) {
	t.Helper()
	psql(t, port, "app", "create table notes (id serial primary key, body text not null)")
	psql(t, port, "app", "insert into notes (body) select 'note ' || g from generate_series(1, 500) g")
	psql(t, port, "app", "create table sessions (id serial primary key, token text)")
	psql(t, port, "app", "insert into sessions (token) values ('abc'), ('def')")
}

func baseConfig(port int) core.Config {
	return core.Config{
		"host": "127.0.0.1", "port": port, "user": "postgres",
		"password": pgPassword, "database": "app", "sslmode": "disable",
	}
}

func dump(t *testing.T, cfg core.Config) ([]byte, string) {
	t.Helper()
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	data, err := io.ReadAll(st.Reader)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close dump: %v", err)
	}
	return data, st.Extension
}

func TestPostgresTestConnection(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	cfg := baseConfig(port)
	if err := New().Test(t.Context(), cfg, testLogger()); err != nil {
		t.Fatalf("test: %v", err)
	}
	bad := cfg.Clone()
	bad["password"] = "wrong"
	if err := New().Test(t.Context(), bad, testLogger()); err == nil {
		t.Error("a wrong password should fail the connection test")
	}
	missing := cfg.Clone()
	missing["database"] = "nope"
	if err := New().Test(t.Context(), missing, testLogger()); err == nil {
		t.Error("a missing database should fail the connection test")
	}
}

func TestPostgresCustomFormatDumpAndRestore(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	seed(t, port)

	data, ext := dump(t, baseConfig(port))
	if ext != "dump" {
		t.Fatalf("extension %q, want dump", ext)
	}
	if !bytes.HasPrefix(data, []byte("PGDMP")) {
		t.Fatalf("the dump does not look like a custom format archive: %q", data[:min(16, len(data))])
	}

	psql(t, port, "postgres", "create database restored")
	cfg := baseConfig(port)
	if err := New().Restore(t.Context(), cfg, bytes.NewReader(data), source.RestoreOptions{
		Extension: "dump",
		Params:    core.Config{"database": "restored"},
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := psql(t, port, "restored", "select count(*) from notes"); got != "500" {
		t.Errorf("restored notes %q, want 500", got)
	}
	if got := psql(t, port, "restored", "select count(*) from sessions"); got != "2" {
		t.Errorf("restored sessions %q, want 2", got)
	}
}

func TestPostgresPlainFormatDumpAndRestore(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	seed(t, port)

	cfg := baseConfig(port)
	cfg["format"] = "plain"
	data, ext := dump(t, cfg)
	if ext != "sql" {
		t.Fatalf("extension %q, want sql", ext)
	}
	if !bytes.Contains(data, []byte("CREATE TABLE public.notes")) {
		t.Error("the plain dump does not contain the table definition")
	}

	psql(t, port, "postgres", "create database restored_plain")
	if err := New().Restore(t.Context(), cfg, bytes.NewReader(data), source.RestoreOptions{
		Extension: "sql",
		Params:    core.Config{"database": "restored_plain"},
	}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := psql(t, port, "restored_plain", "select count(*) from notes"); got != "500" {
		t.Errorf("restored notes %q, want 500", got)
	}
}

func TestPostgresExcludeTablesAndSchemaOnly(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	seed(t, port)

	cfg := baseConfig(port)
	cfg["format"] = "plain"
	cfg["exclude_tables"] = []string{"public.sessions"}
	data, _ := dump(t, cfg)
	if bytes.Contains(data, []byte("CREATE TABLE public.sessions")) {
		t.Error("the excluded table is still in the dump")
	}
	if !bytes.Contains(data, []byte("CREATE TABLE public.notes")) {
		t.Error("the included table is missing from the dump")
	}

	schemaOnly := baseConfig(port)
	schemaOnly["format"] = "plain"
	schemaOnly["schema_only"] = true
	data, _ = dump(t, schemaOnly)
	if bytes.Contains(data, []byte("COPY public.notes")) {
		t.Error("a schema-only dump should not contain any rows")
	}
	if !bytes.Contains(data, []byte("CREATE TABLE public.notes")) {
		t.Error("a schema-only dump should contain the table definition")
	}
}

func TestPostgresAllDatabases(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	seed(t, port)

	cfg := baseConfig(port)
	cfg["all_databases"] = true
	data, ext := dump(t, cfg)
	if ext != "sql" {
		t.Fatalf("extension %q, want sql", ext)
	}
	if !bytes.Contains(data, []byte("CREATE ROLE")) {
		t.Error("a cluster dump should contain the roles")
	}
	if !bytes.Contains(data, []byte("public.notes")) {
		t.Error("a cluster dump should contain the application tables")
	}
}

func TestPostgresFailedDumpIsReportedOnClose(t *testing.T) {
	requirePostgres(t)
	port := startPostgres(t)
	cfg := baseConfig(port)
	cfg["database"] = "does_not_exist"
	st, err := New().Backup(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	_, _ = io.ReadAll(st.Reader)
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("a failed dump must report an error from Close")
	}
	if strings.Contains(err.Error(), pgPassword) {
		t.Errorf("the password leaked into the error: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
