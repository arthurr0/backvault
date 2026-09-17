package secrets

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func newCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newCipher(t)
	enc, err := c.Encrypt("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) {
		t.Fatalf("missing prefix: %q", enc)
	}
	plain, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "hunter2" {
		t.Fatalf("plain = %q", plain)
	}
	again, err := c.Encrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if again != enc {
		t.Fatal("double encryption should be a no-op")
	}
}

func TestDecryptLegacyPlain(t *testing.T) {
	c := newCipher(t)
	got, err := c.Decrypt("plain-value")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain-value" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	a := newCipher(t)
	b := newCipher(t)
	enc, err := a.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("expected failure with a different key")
	}
}

func TestConfigFields(t *testing.T) {
	c := newCipher(t)
	cfg := core.Config{"user": "bob", "password": "s3cr3t", "port": 5432}
	enc, err := c.EncryptConfig(cfg, []string{"password", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if enc.String("password") == "s3cr3t" {
		t.Fatal("password not encrypted")
	}
	if enc.String("user") != "bob" {
		t.Fatal("non-secret field changed")
	}
	dec, err := c.DecryptConfig(enc, []string{"password", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.String("password") != "s3cr3t" {
		t.Fatalf("round trip failed: %v", dec)
	}
	if dec.Int("port", 0) != 5432 {
		t.Fatal("int field changed")
	}
}

func TestParseKey(t *testing.T) {
	raw := make([]byte, KeySize)
	for i := range raw {
		raw[i] = byte(i)
	}
	for _, encoded := range []string{hex.EncodeToString(raw), base64.StdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)} {
		got, err := ParseKey(encoded)
		if err != nil {
			t.Fatalf("%s: %v", encoded, err)
		}
		if string(got) != string(raw) {
			t.Fatalf("mismatch for %s", encoded)
		}
	}
	if _, err := ParseKey("short"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadKeyGeneratesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "master.key")
	key, err := LoadKey("", path)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != KeySize {
		t.Fatalf("key size %d", len(key))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	again, err := LoadKey("", path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(key) {
		t.Fatal("key changed on reload")
	}
}
