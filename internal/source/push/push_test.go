package push

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/source"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestBackupIsRefused(t *testing.T) {
	_, err := New().Backup(t.Context(), core.Config{}, testLogger())
	if err == nil {
		t.Fatal("push sources must not produce a backup stream")
	}
	if !strings.Contains(err.Error(), "ingest API") {
		t.Errorf("error should point at the ingest API: %v", err)
	}
}

func TestTestAlwaysSucceeds(t *testing.T) {
	if err := New().Test(t.Context(), core.Config{}, testLogger()); err != nil {
		t.Errorf("test should succeed: %v", err)
	}
}

func TestSpec(t *testing.T) {
	spec := New().Spec()
	if spec.Kind != "push" {
		t.Errorf("kind %q", spec.Kind)
	}
	if !spec.Has(core.CapIngest) {
		t.Error("push should declare the ingest capability")
	}
	if spec.Has(core.CapRestore) {
		t.Error("push should not declare a restore capability")
	}
	var d any = New()
	if _, ok := d.(source.Restorer); ok {
		t.Error("push must not implement the restorer interface")
	}
}
