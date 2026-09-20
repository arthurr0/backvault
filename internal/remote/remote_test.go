package remote

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestGenerateKeyRoundTrip(t *testing.T) {
	priv, pub, err := GenerateKey("backvault@test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(priv, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("unexpected private key format: %q", priv[:40])
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") || !strings.HasSuffix(pub, " backvault@test") {
		t.Fatalf("unexpected public key line: %q", pub)
	}
	h := core.Host{Address: "example", User: "root", Auth: core.HostAuthKey, PrivateKey: priv}
	if err := Validate(h); err != nil {
		t.Fatalf("validate: %v", err)
	}
	derived, err := PublicKeyOf(h)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, derived) {
		t.Fatalf("derived public key %q does not match %q", derived, pub)
	}
}

func TestValidateReportsProblems(t *testing.T) {
	err := Validate(core.Host{Auth: core.HostAuthPassword, HostKey: "abc"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"address is required", "user is required", "password is required", "SHA256:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{"simple": "simple", "/var/lib/data": "/var/lib/data", "has space": "'has space'", "it's": `'it'\''s'`, "": "''"}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ShellJoin([]string{"tar", "-C", "/srv/my app", "-cf", "-", "."}); got != "tar -C '/srv/my app' -cf - ." {
		t.Errorf("ShellJoin = %q", got)
	}
}

func TestSudoWrapsCommand(t *testing.T) {
	c := &Client{host: core.Host{Sudo: true}}
	if got := c.Command("docker ps"); got != "sudo -n -- sh -c 'docker ps'" {
		t.Errorf("Command = %q", got)
	}
	c.host.Sudo = false
	if got := c.Command("docker ps"); got != "docker ps" {
		t.Errorf("Command = %q", got)
	}
}
