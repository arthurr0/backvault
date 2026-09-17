package mongodb

import (
	"testing"

	"github.com/arthurr0/backvault/internal/core"
)

func TestValidate(t *testing.T) {
	d := New()
	if err := d.Validate(core.Config{}); err == nil {
		t.Error("uri should be required")
	}
	if err := d.Validate(core.Config{"uri": "http://localhost"}); err == nil {
		t.Error("a non mongodb uri should be rejected")
	}
	if err := d.Validate(core.Config{"uri": "mongodb://localhost:27017"}); err != nil {
		t.Errorf("valid uri rejected: %v", err)
	}
	if err := d.Validate(core.Config{"uri": "mongodb+srv://cluster.example.com"}); err != nil {
		t.Errorf("srv uri rejected: %v", err)
	}
	if err := d.Validate(core.Config{"uri": "mongodb://localhost", "collection": "events"}); err == nil {
		t.Error("a collection without a database should be rejected")
	}
}

func TestRedactCoversUriAndPassword(t *testing.T) {
	cfg := core.Config{"uri": "mongodb://admin:hunter2@db:27017/?authSource=admin"}
	got := redact(cfg)
	if len(got) != 2 {
		t.Fatalf("redaction list %v", got)
	}
	if got[0] != cfg.String("uri") || got[1] != "hunter2" {
		t.Errorf("redaction list %v", got)
	}
}

func TestTargetDescription(t *testing.T) {
	if target(core.Config{}) != "all databases" {
		t.Error("empty target")
	}
	if target(core.Config{"database": "app"}) != "app" {
		t.Error("database target")
	}
	if target(core.Config{"database": "app", "collection": "events"}) != "app.events" {
		t.Error("collection target")
	}
}
