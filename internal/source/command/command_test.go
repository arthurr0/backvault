package command

import (
	"bytes"
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

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("command should be required")
	}
	if err := d.Validate(core.Config{"command": "echo hi", "env": []string{"NOEQUALS"}}); err == nil {
		t.Error("malformed env should be rejected")
	}
	if err := d.Validate(core.Config{"command": "echo hi", "working_dir": "/definitely/not/here"}); err == nil {
		t.Error("missing working directory should be rejected")
	}
	if err := d.Validate(core.Config{"command": "echo hi"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestBackupStreamsStdout(t *testing.T) {
	st, err := New().Backup(t.Context(), core.Config{
		"command": "printf 'backvault-output'", "extension": "txt",
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(st.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if string(data) != "backvault-output" {
		t.Errorf("got %q", data)
	}
	if st.Extension != "txt" {
		t.Errorf("extension %q", st.Extension)
	}
}

func TestBackupUsesEnvAndWorkingDir(t *testing.T) {
	dir := t.TempDir()
	st, err := New().Backup(t.Context(), core.Config{
		"command":     "printf '%s:%s' \"$BACKVAULT_TEST\" \"$(pwd)\"",
		"env":         []string{"BACKVAULT_TEST=value"},
		"working_dir": dir,
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(st.Reader)
	if err := st.Reader.Close(); err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	if !strings.HasPrefix(string(data), "value:") {
		t.Errorf("environment not applied: %q", data)
	}
	if !strings.HasSuffix(string(data), resolved) && !strings.HasSuffix(string(data), dir) {
		t.Errorf("working directory not applied: %q", data)
	}
}

func TestBackupFailureIsReportedOnClose(t *testing.T) {
	st, err := New().Backup(t.Context(), core.Config{
		"command": "printf partial; echo 'boom' >&2; exit 9",
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(st.Reader); err != nil {
		t.Fatal(err)
	}
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("close should report the failure")
	}
	if !strings.Contains(err.Error(), "code 9") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error is missing detail: %v", err)
	}
}

func TestEnvValuesAreScrubbedFromErrors(t *testing.T) {
	st, err := New().Backup(t.Context(), core.Config{
		"command": "echo \"leaking $SECRET\" >&2; exit 1",
		"env":     []string{"SECRET=hunter2hunter2"},
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(st.Reader)
	err = st.Reader.Close()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "hunter2hunter2") {
		t.Errorf("secret leaked: %v", err)
	}
}

func TestTestCommand(t *testing.T) {
	d := New()
	if err := d.Test(t.Context(), core.Config{"command": "true"}, testLogger()); err != nil {
		t.Errorf("without a test command the shell check should pass: %v", err)
	}
	if err := d.Test(t.Context(), core.Config{"command": "true", "test_command": "exit 4"}, testLogger()); err == nil {
		t.Error("a failing test command should fail")
	}
	if err := d.Test(t.Context(), core.Config{"command": "true", "test_command": "true"}, testLogger()); err != nil {
		t.Errorf("a passing test command should succeed: %v", err)
	}
	if err := d.Test(t.Context(), core.Config{"command": "true", "shell": "/definitely/not/a/shell"}, testLogger()); err == nil {
		t.Error("a missing shell should fail")
	}
}

func TestRestoreCommandReceivesStdin(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.txt")
	d := New()
	if err := d.Restore(t.Context(), core.Config{"command": "true"}, bytes.NewReader([]byte("payload")),
		source.RestoreOptions{}, testLogger()); err == nil {
		t.Error("restore without a restore command should fail")
	}
	cfg := core.Config{"command": "true", "restore_command": "cat > " + target}
	if err := d.Restore(t.Context(), cfg, bytes.NewReader([]byte("payload")), source.RestoreOptions{}, testLogger()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Errorf("restored content %q", got)
	}
}
