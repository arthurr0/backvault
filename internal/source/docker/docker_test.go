package docker

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
	"github.com/arthurr0/backvault/internal/source/internal/remoteexec"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{"mode": "volume"}); err == nil {
		t.Error("a volume name should be required")
	}
	if err := d.Validate(core.Config{"mode": "volume", "volume": "data"}); err != nil {
		t.Errorf("valid volume config rejected: %v", err)
	}
	if err := d.Validate(core.Config{"mode": "exec", "container": "app"}); err == nil {
		t.Error("a command should be required in exec mode")
	}
	if err := d.Validate(core.Config{"mode": "exec", "container": "app", "command": "true"}); err != nil {
		t.Errorf("valid exec config rejected: %v", err)
	}
	if err := d.Validate(core.Config{"mode": "swarm", "volume": "v"}); err == nil {
		t.Error("an unknown mode should be rejected")
	}
}

func TestRestoreIsVolumeOnly(t *testing.T) {
	err := New().Restore(t.Context(), core.Config{"mode": "exec", "container": "app", "command": "true"},
		nil, source.RestoreOptions{}, nil)
	if err == nil {
		t.Error("restore should be refused in exec mode")
	}
}

func TestEnvCarriesDockerHost(t *testing.T) {
	if got := env(core.Config{"docker_host": "tcp://10.0.0.1:2375"}); len(got) != 1 || got[0] != "DOCKER_HOST=tcp://10.0.0.1:2375" {
		t.Errorf("environment %v", got)
	}
	if got := env(core.Config{}); got != nil {
		t.Errorf("environment %v, want nil", got)
	}
}

func TestRemoteVolumeArgv(t *testing.T) {
	cfg := core.Config{"mode": "volume", "volume": "app data", "image": "alpine:3.20"}
	line := remoteexec.ShellLine(remoteexec.Command{Argv: backupArgv("docker", cfg), Env: env(cfg)})
	want := `docker run --rm --network none -v 'app data:/data:ro' alpine:3.20 tar -C /data -cf - .`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestRemoteExecArgvAndDockerHost(t *testing.T) {
	cfg := core.Config{
		"mode": "exec", "container": "app-db-1", "command": "pg_dump -Fc app",
		"exec_user": "postgres", "docker_host": "unix:///run/podman/podman.sock",
	}
	line := remoteexec.ShellLine(remoteexec.Command{Argv: backupArgv("podman", cfg), Env: env(cfg)})
	want := `env DOCKER_HOST=unix:///run/podman/podman.sock podman exec --user postgres app-db-1 /bin/sh -c 'pg_dump -Fc app'`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestRemoteRestoreArgv(t *testing.T) {
	cfg := core.Config{"mode": "volume", "volume": "data"}
	line := remoteexec.ShellLine(remoteexec.Command{Argv: restoreArgv("docker", "data", false, cfg)})
	want := `docker run --rm -i --network none -v data:/data alpine:3.20 /bin/sh -c 'tar -C /data -xf -'`
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
	overwrite := remoteexec.ShellLine(remoteexec.Command{Argv: restoreArgv("docker", "data", true, cfg)})
	if !strings.Contains(overwrite, "rm -rf /data/..?*") {
		t.Errorf("overwrite should clear the volume: %q", overwrite)
	}
}
