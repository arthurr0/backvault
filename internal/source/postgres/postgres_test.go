package postgres

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
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
