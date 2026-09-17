package sftp

import (
	"errors"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("host and user should be required")
	}
	base := core.Config{"host": "u1.your-storagebox.de", "user": "u1", "auth": "password", "password": "x"}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	noSecret := base.Clone()
	delete(noSecret, "password")
	if err := d.Validate(noSecret); err == nil {
		t.Error("password auth without a password should be rejected")
	}
	keyCfg := core.Config{"host": "h", "user": "u", "auth": "key"}
	if err := d.Validate(keyCfg); err == nil {
		t.Error("key auth without a key should be rejected")
	}
	bad := base.Clone()
	bad["host_key"] = "MD5:aa"
	if err := d.Validate(bad); err == nil {
		t.Error("a non SHA256 fingerprint should be rejected")
	}
	bad = base.Clone()
	bad["concurrent_requests"] = 0
	if err := d.Validate(bad); err == nil {
		t.Error("zero concurrency should be rejected")
	}
}

func TestResolveAndRelative(t *testing.T) {
	c := &client{base: "backups/backvault"}
	cases := map[string]string{
		"job/file.tar":  "backups/backvault/job/file.tar",
		"/job/file.tar": "backups/backvault/job/file.tar",
		"":              "backups/backvault",
	}
	for in, want := range cases {
		got, err := c.resolve(in)
		if err != nil {
			t.Fatalf("resolve(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
	if got := c.relative("backups/backvault/job/file.tar"); got != "job/file.tar" {
		t.Errorf("relative = %q", got)
	}
	root := &client{base: "."}
	if got, _ := root.resolve("job/file.tar"); got != "job/file.tar" {
		t.Errorf("resolve with a dot base = %q", got)
	}
}

func TestPortHelpMentionsHetzner(t *testing.T) {
	for _, f := range New().Spec().Fields {
		if f.Name == "port" {
			if !strings.Contains(f.Help, "23") {
				t.Errorf("port help should mention the Hetzner port: %q", f.Help)
			}
			return
		}
	}
	t.Error("no port field")
}

func TestScrubRemovesSecrets(t *testing.T) {
	err := scrub(core.Config{"password": "hunter2hunter2"}, errors.New("failed for hunter2hunter2"))
	if strings.Contains(err.Error(), "hunter2hunter2") {
		t.Errorf("secret leaked: %v", err)
	}
}
