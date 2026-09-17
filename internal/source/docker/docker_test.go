package docker

import (
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
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
