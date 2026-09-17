package s3

import (
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("bucket should be required")
	}
	base := core.Config{"bucket": "backups"}
	if err := d.Validate(base); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	bad := base.Clone()
	bad["endpoint"] = "minio:9000"
	if err := d.Validate(bad); err == nil {
		t.Error("an endpoint without a scheme should be rejected")
	}
	bad = base.Clone()
	bad["access_key"] = "AKIA"
	if err := d.Validate(bad); err == nil {
		t.Error("an access key without a secret key should be rejected")
	}
	bad = base.Clone()
	bad["part_size_mb"] = 1
	if err := d.Validate(bad); err == nil {
		t.Error("a part size below 5 MiB should be rejected")
	}
	bad = base.Clone()
	bad["concurrency"] = 0
	if err := d.Validate(bad); err == nil {
		t.Error("zero concurrency should be rejected")
	}
	bad = base.Clone()
	bad["sse"] = "rot13"
	if err := d.Validate(bad); err == nil {
		t.Error("an unknown sse mode should be rejected")
	}
}

func TestKeyComposition(t *testing.T) {
	c := &client{bucket: "b", prefix: "servers/db01"}
	cases := map[string]string{
		"job/file.tar":    "servers/db01/job/file.tar",
		"/job/file.tar":   "servers/db01/job/file.tar",
		"./job/file.tar":  "servers/db01/job/file.tar",
		"":                "servers/db01",
		"a/../b/file.bin": "servers/db01/b/file.bin",
	}
	for in, want := range cases {
		if got := c.key(in); got != want {
			t.Errorf("key(%q) = %q, want %q", in, got, want)
		}
	}
	if got := c.relative("servers/db01/job/file.tar"); got != "job/file.tar" {
		t.Errorf("relative = %q", got)
	}
	bare := &client{bucket: "b"}
	if got := bare.key("job/file.tar"); got != "job/file.tar" {
		t.Errorf("key without prefix = %q", got)
	}
}

func TestSpec(t *testing.T) {
	spec := New().Spec()
	if !spec.Has(core.CapBrowse) || !spec.Has(core.CapTest) {
		t.Error("s3 should declare browse and test")
	}
	var endpointHelp string
	for _, f := range spec.Fields {
		if f.Name == "endpoint" {
			endpointHelp = f.Help
		}
	}
	for _, provider := range []string{"MinIO", "Backblaze", "Wasabi", "Cloudflare R2", "Hetzner"} {
		if !strings.Contains(endpointHelp, provider) {
			t.Errorf("endpoint help does not mention %s", provider)
		}
	}
}
