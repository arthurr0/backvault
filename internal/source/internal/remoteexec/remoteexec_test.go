package remoteexec

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestShellLineQuotesArgv(t *testing.T) {
	got := ShellLine(Command{Argv: []string{"tar", "-C", "/srv/my data", "-cf", "-", "."}})
	want := `tar -C '/srv/my data' -cf - .`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestShellLineAddsDirAndEnv(t *testing.T) {
	got := ShellLine(Command{
		Argv: []string{"pg_dump", "-d", "app"},
		Env:  []string{"PGPASSWORD=hunter two", "PGSSLMODE=require"},
		Dir:  "/var/lib/app data",
	})
	want := `cd '/var/lib/app data' && env 'PGPASSWORD=hunter two' PGSSLMODE=require pg_dump -d app`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestShellLineWrapsScriptInShell(t *testing.T) {
	got := ShellLine(Command{Script: "pg_dump -Fc app | cat", Shell: "/bin/sh", Dir: "/tmp"})
	want := `cd /tmp && /bin/sh -c 'pg_dump -Fc app | cat'`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestShellLineRunsBareScriptVerbatim(t *testing.T) {
	got := ShellLine(Command{Script: "tar -C /data -cf - ."})
	if got != "tar -C /data -cf - ." {
		t.Errorf("got %q", got)
	}
}

func TestShellLineDefaultsToAShellWhenEnvIsSet(t *testing.T) {
	got := ShellLine(Command{Script: "echo $TOKEN", Env: []string{"TOKEN=abc"}})
	want := `env TOKEN=abc /bin/sh -c 'echo $TOKEN'`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLogLineHidesSecrets(t *testing.T) {
	c := Command{
		Argv:   []string{"mongodump", "--uri=mongodb://user:hunter2hunter2@db/x"},
		Env:    []string{"PGPASSWORD=hunter2hunter2"},
		Redact: []string{"hunter2hunter2"},
	}
	line := LogLine(c)
	if strings.Contains(line, "hunter2hunter2") {
		t.Fatalf("secret leaked into the log line: %s", line)
	}
	if !strings.Contains(line, "PGPASSWORD=***") {
		t.Errorf("environment value should be masked: %s", line)
	}
	if !strings.Contains(ShellLine(c), "hunter2hunter2") {
		t.Error("the executed line still has to carry the real value")
	}
}

func TestTempFileScript(t *testing.T) {
	got := TempFileScript("[client]\npassword=\"x\"\n", `mysqldump --defaults-extra-file="$f" app`)
	want := `umask 077; f=$(mktemp) || exit 1; printf '%s' '[client]
password="x"
' > "$f"; mysqldump --defaults-extra-file="$f" app; rc=$?; rm -f "$f"; exit $rc`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScratchScript(t *testing.T) {
	got := ScratchScript(`sqlite3 /db ".backup '$f'" && cat "$f"`)
	want := `umask 077; f=$(mktemp) || exit 1; sqlite3 /db ".backup '$f'" && cat "$f"; rc=$?; rm -f "$f"; exit $rc`
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestLookupScript(t *testing.T) {
	got := LookupScript("", []string{"docker", "podman"})
	want := `for n in docker podman; do c=$(command -v "$n" 2>/dev/null) && { echo "$c"; exit 0; }; done; exit 1`
	if got != want {
		t.Errorf("got %q", got)
	}
	withPath := LookupScript("/opt/pg/bin", []string{"pg_dump"})
	if !strings.HasPrefix(withPath, `p=/opt/pg/bin; if [ -d "$p" ]; then for n in pg_dump;`) {
		t.Errorf("binary path branch missing: %s", withPath)
	}
	if !strings.Contains(withPath, `elif [ -x "$p" ]; then echo "$p"; exit 0; fi;`) {
		t.Errorf("full path branch missing: %s", withPath)
	}
}

func TestEnvPairsAreSorted(t *testing.T) {
	got := EnvPairs(map[string]string{"B": "2", "A": "1", "": "skipped"})
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Errorf("got %v", got)
	}
}

func TestValidateRequiredSkipsLocalOnlyFieldsOnAHost(t *testing.T) {
	spec := core.DriverSpec{Fields: []core.Field{
		{Name: "host", Label: "Host", Required: true, LocalOnly: true},
		{Name: "command", Label: "Command", Required: true},
	}}
	cfg := core.Config{"command": "true"}
	if err := ValidateRequired(spec, cfg); err == nil {
		t.Error("the local connection field is required without a host")
	}
	if err := ValidateRequired(spec, cfg.WithHost(&core.Host{Name: "web01"})); err != nil {
		t.Errorf("with a host the local field must be ignored: %v", err)
	}
}

func TestHostHelpers(t *testing.T) {
	cfg := core.Config{}.WithHost(&core.Host{Address: "10.0.0.5"})
	if !IsRemote(cfg) || HostLabel(cfg) != "10.0.0.5" {
		t.Errorf("host label %q", HostLabel(cfg))
	}
	if IsRemote(core.Config{}) {
		t.Error("a config without a host is local")
	}
}
