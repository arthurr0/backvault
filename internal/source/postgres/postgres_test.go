package postgres

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("required fields should be reported")
	}
	base := core.Config{"host": "localhost", "user": "postgres", "database": "app"}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	noDB := core.Config{"host": "localhost", "user": "postgres"}
	if err := d.Validate(noDB); err == nil {
		t.Error("database should be required when not dumping all databases")
	}
	all := core.Config{"host": "localhost", "user": "postgres", "all_databases": true}
	if err := d.Validate(all); err != nil {
		t.Errorf("all_databases config rejected: %v", err)
	}
	bad := base.Clone()
	bad["format"] = "parquet"
	if err := d.Validate(bad); err == nil {
		t.Error("unknown format should be rejected")
	}
}

func TestConnArgsAndEnv(t *testing.T) {
	cfg := core.Config{"host": "db.internal", "port": 5433, "user": "backup", "password": "s3cret", "sslmode": "require"}
	args := strings.Join(connArgs(cfg), " ")
	if args != "-h db.internal -p 5433 -U backup" {
		t.Errorf("connection args %q", args)
	}
	env := strings.Join(env(cfg), " ")
	if !strings.Contains(env, "PGPASSWORD=s3cret") {
		t.Errorf("password is not passed through the environment: %q", env)
	}
	if !strings.Contains(env, "PGSSLMODE=require") {
		t.Errorf("sslmode is not passed through the environment: %q", env)
	}
	for _, a := range connArgs(cfg) {
		if strings.Contains(a, "s3cret") {
			t.Error("the password must never appear on the command line")
		}
	}
	if r := redact(cfg); len(r) != 1 || r[0] != "s3cret" {
		t.Errorf("redaction list %v", r)
	}
}

func TestSpecExtensions(t *testing.T) {
	spec := New().Spec()
	if spec.Kind != "postgres" {
		t.Errorf("kind %q", spec.Kind)
	}
	if !spec.Has(core.CapRestore) {
		t.Error("postgres should declare the restore capability")
	}
	wantTools := map[string]bool{"pg_dump": true, "pg_dumpall": true, "pg_restore": true, "psql": true}
	for _, tool := range spec.Tools {
		delete(wantTools, tool)
	}
	if len(wantTools) != 0 {
		t.Errorf("spec is missing tools %v", wantTools)
	}
	if scope(core.Config{"all_databases": true}) != "all databases" {
		t.Error("scope should describe a cluster dump")
	}
}

func TestRemoteDumpLine(t *testing.T) {
	cfg := core.Config{
		"host": "10.0.0.5", "port": 5433, "user": "backup", "password": "hunter2hunter2",
		"database": "app production", "sslmode": "require", "exclude_tables": []string{"public.sessions"},
		"extra_args": []string{"--no-owner"},
	}
	args, ext := dumpArgs(cfg)
	if ext != "dump" {
		t.Errorf("extension %q", ext)
	}
	cmd := remoteexec.Command{Argv: append([]string{"pg_dump"}, args...), Env: env(cfg), Redact: redact(cfg)}
	line := remoteexec.ShellLine(cmd)
	want := `env PGPASSWORD=hunter2hunter2 PGSSLMODE=require PGCONNECT_TIMEOUT=15 PGCLIENTENCODING=UTF8 ` +
		`pg_dump -h 10.0.0.5 -p 5433 -U backup -w -d 'app production' -F c --exclude-table=public.sessions --no-owner`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
	if logged := remoteexec.LogLine(cmd); strings.Contains(logged, "hunter2hunter2") {
		t.Errorf("the password leaked into the log line: %s", logged)
	}
	if strings.Contains(strings.Join(args, " "), "hunter2hunter2") {
		t.Error("the password must never be an argument")
	}
}

func TestRemoteDumpAllLine(t *testing.T) {
	cfg := core.Config{"host": "db", "user": "postgres", "all_databases": true, "schema_only": true}
	args, ext := dumpArgs(cfg)
	if ext != "sql" {
		t.Errorf("extension %q", ext)
	}
	line := remoteexec.ShellLine(remoteexec.Command{Argv: append([]string{"pg_dumpall"}, args...), Env: env(cfg)})
	want := `env PGCONNECT_TIMEOUT=15 PGCLIENTENCODING=UTF8 pg_dumpall -h db -p 5432 -U postgres -w --schema-only`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}
