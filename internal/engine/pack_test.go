package engine

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

func TestPackRoundTrip(t *testing.T) {
	payload := strings.Repeat("backvault-payload.", 500)
	cases := []PackOptions{
		{},
		{Compression: core.CompressionGzip},
		{Compression: core.CompressionGzip, Level: 9},
		{Compression: core.CompressionZstd},
		{Compression: core.CompressionZstd, Level: 3},
		{Encryption: core.EncryptionAge, Passphrase: "pw"},
		{Compression: core.CompressionZstd, Encryption: core.EncryptionAge, Passphrase: "pw"},
	}
	for _, opts := range cases {
		var buf bytes.Buffer
		w, closer, err := newPackWriter(&buf, opts)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		if _, err := io.Copy(w, strings.NewReader(payload)); err != nil {
			t.Fatal(err)
		}
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
		r, rc, err := newUnpackReader(bytes.NewReader(buf.Bytes()), opts)
		if err != nil {
			t.Fatalf("%+v: %v", opts, err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := rc.Close(); err != nil {
			t.Fatal(err)
		}
		if string(got) != payload {
			t.Fatalf("%+v: round trip mismatch (%d bytes)", opts, len(got))
		}
	}
}

func TestPackMissingPassphrase(t *testing.T) {
	var buf bytes.Buffer
	if _, _, err := newPackWriter(&buf, PackOptions{Encryption: core.EncryptionAge}); err == nil {
		t.Fatal("expected an error without a passphrase")
	}
	if _, _, err := newUnpackReader(&buf, PackOptions{Encryption: core.EncryptionAge}); err == nil {
		t.Fatal("expected an error without a passphrase")
	}
}

func TestBuildFilename(t *testing.T) {
	at := time.Date(2024, 6, 15, 2, 30, 45, 0, time.UTC)
	cases := []struct {
		ext  string
		opts PackOptions
		want string
	}{
		{"dump", PackOptions{}, "db-20240615-023045.dump"},
		{".sql", PackOptions{Compression: core.CompressionGzip}, "db-20240615-023045.sql.gz"},
		{"tar", PackOptions{Compression: core.CompressionZstd, Encryption: core.EncryptionAge}, "db-20240615-023045.tar.zst.age"},
		{"", PackOptions{}, "db-20240615-023045.bin"},
	}
	for _, tc := range cases {
		if got := BuildFilename("db", tc.ext, at, tc.opts); got != tc.want {
			t.Fatalf("BuildFilename(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
	if got := BuildPath("db", "db-20240615-023045.dump"); got != "db/db-20240615-023045.dump" {
		t.Fatalf("BuildPath = %q", got)
	}
}

func TestDetectPack(t *testing.T) {
	cases := []struct {
		name        string
		compression core.Compression
		encryption  core.Encryption
		ext         string
	}{
		{"dump.sql", core.CompressionNone, core.EncryptionNone, "sql"},
		{"dump.sql.gz", core.CompressionGzip, core.EncryptionNone, "sql"},
		{"dump.tar.zst.age", core.CompressionZstd, core.EncryptionAge, "tar"},
		{"payload", core.CompressionNone, core.EncryptionNone, ""},
	}
	for _, tc := range cases {
		c, e, ext := DetectPack(tc.name)
		if c != tc.compression || e != tc.encryption || ext != tc.ext {
			t.Fatalf("DetectPack(%q) = %q %q %q", tc.name, c, e, ext)
		}
	}
}
