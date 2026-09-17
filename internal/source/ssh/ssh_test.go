package ssh

import (
	"errors"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("required fields should be reported")
	}
	base := core.Config{"host": "db01", "user": "root", "command": "tar -cf - /data", "auth": "password", "password": "x"}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid password config rejected: %v", err)
	}
	noSecret := base.Clone()
	delete(noSecret, "password")
	if err := d.Validate(noSecret); err == nil {
		t.Error("password auth without a password should be rejected")
	}
	keyCfg := core.Config{"host": "db01", "user": "root", "command": "true", "auth": "key"}
	if err := d.Validate(keyCfg); err == nil {
		t.Error("key auth without a key should be rejected")
	}
	badFingerprint := base.Clone()
	badFingerprint["host_key"] = "MD5:aa:bb"
	if err := d.Validate(badFingerprint); err == nil {
		t.Error("a non SHA256 fingerprint should be rejected")
	}
}

func TestAuthMethods(t *testing.T) {
	if _, err := authMethods(core.Config{"auth": "password", "password": "x"}); err != nil {
		t.Errorf("password auth: %v", err)
	}
	if _, err := authMethods(core.Config{"auth": "key", "private_key": "not a key"}); err == nil {
		t.Error("an invalid key should be rejected")
	}
	if _, err := authMethods(core.Config{"auth": "kerberos"}); err == nil {
		t.Error("an unknown auth method should be rejected")
	}
}

func TestHostKeyCallbackRequiresFingerprintFormat(t *testing.T) {
	if _, err := hostKeyCallback(core.Config{"host_key": "nonsense"}, nil); err == nil {
		t.Error("a malformed fingerprint should be rejected")
	}
	if cb, err := hostKeyCallback(core.Config{}, nil); err != nil || cb == nil {
		t.Errorf("trust on first use callback: %v", err)
	}
}

func TestScrubRemovesSecrets(t *testing.T) {
	cfg := core.Config{"password": "hunter2hunter2"}
	err := scrub(cfg, errors.New("auth failed for hunter2hunter2"))
	if strings.Contains(err.Error(), "hunter2hunter2") {
		t.Errorf("secret leaked: %v", err)
	}
}
