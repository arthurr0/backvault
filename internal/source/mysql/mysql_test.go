package mysql

import (
	"os"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("required fields should be reported")
	}
	base := core.Config{"host": "127.0.0.1", "user": "root", "databases": []string{"app"}}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	if err := d.Validate(core.Config{"host": "127.0.0.1", "user": "root"}); err == nil {
		t.Error("a database should be required")
	}
	if err := d.Validate(core.Config{"host": "127.0.0.1", "user": "root", "all_databases": true}); err != nil {
		t.Errorf("all_databases config rejected: %v", err)
	}
}

func TestDefaultsFileHoldsCredentialsWithTightPermissions(t *testing.T) {
	cfg := core.Config{"host": "db.internal", "port": 3307, "user": "backup", "password": `pa"ss\word`}
	path, cleanup, err := defaultsFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("defaults file mode %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"[client]", "host=db.internal", "port=3307", "user=backup", `password="pa\"ss\\word"`} {
		if !strings.Contains(text, want) {
			t.Errorf("defaults file is missing %q:\n%s", want, text)
		}
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the defaults file was not removed")
	}
}

func TestSocketOverridesHost(t *testing.T) {
	path, cleanup, err := defaultsFile(core.Config{"socket": "/run/mysqld/mysqld.sock", "user": "root"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "socket=/run/mysqld/mysqld.sock") {
		t.Errorf("socket not used:\n%s", data)
	}
	if strings.Contains(string(data), "host=") {
		t.Errorf("host should be omitted when a socket is set:\n%s", data)
	}
}

func TestScope(t *testing.T) {
	if scope(core.Config{"all_databases": true}) != "all databases" {
		t.Error("all databases scope")
	}
	if scope(core.Config{"databases": []string{"a", "b"}}) != "a, b" {
		t.Error("database list scope")
	}
}

func TestRemoteDefaultsScript(t *testing.T) {
	cfg := core.Config{
		"host": "10.0.0.7", "port": 3307, "user": "backup", "password": "hunter2hunter2",
		"databases": []string{"app production"},
	}
	script := remoteDefaultsScript("mysqldump", dumpArgs(cfg), defaultsContent(cfg))
	if !strings.HasPrefix(script, "umask 077; f=$(mktemp) || exit 1; printf '%s' ") {
		t.Errorf("the defaults file must be created with umask 077: %s", script)
	}
	if !strings.Contains(script, `mysqldump --defaults-extra-file="$f" --single-transaction --routines --triggers --events 'app production'`) {
		t.Errorf("unexpected dump command: %s", script)
	}
	if !strings.HasSuffix(script, `; rc=$?; rm -f "$f"; exit $rc`) {
		t.Errorf("the defaults file must be removed: %s", script)
	}
	argv := strings.Split(script, `mysqldump --defaults-extra-file="$f" `)[1]
	if strings.Contains(argv, "hunter2hunter2") {
		t.Error("the password must never appear in the arguments")
	}
	if !strings.Contains(defaultsContent(cfg), `password="hunter2hunter2"`) {
		t.Error("the password belongs in the defaults file")
	}
}

func TestRemoteDefaultsScriptWithoutArguments(t *testing.T) {
	script := remoteDefaultsScript("mysql", nil, "[client]\n")
	if !strings.Contains(script, `; mysql --defaults-extra-file="$f"; rc=$?;`) {
		t.Errorf("got %s", script)
	}
}
