package redis

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	if err := New().Validate(core.Config{}); err != nil {
		t.Errorf("an empty config should fall back to the defaults: %v", err)
	}
	if err := New().Validate(core.Config{"host": "127.0.0.1"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	if connArgs(core.Config{})[1] != "127.0.0.1" {
		t.Error("the default host should be used when none is set")
	}
}

func TestPasswordGoesThroughTheEnvironment(t *testing.T) {
	cfg := core.Config{"host": "cache", "port": 6380, "password": "hunter2", "tls": true, "tls_skip_verify": true}
	args := connArgs(cfg)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "hunter2") {
		t.Errorf("password leaked onto the command line: %q", joined)
	}
	if !strings.Contains(joined, "--tls") || !strings.Contains(joined, "--insecure") {
		t.Errorf("tls flags missing: %q", joined)
	}
	if got := env(cfg); len(got) != 1 || got[0] != "REDISCLI_AUTH=hunter2" {
		t.Errorf("environment %v", got)
	}
}

func TestSpecHasNoRestore(t *testing.T) {
	spec := New().Spec()
	if spec.Has(core.CapRestore) {
		t.Error("redis should not declare a restore capability")
	}
}
