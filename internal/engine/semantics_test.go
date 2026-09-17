package engine

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/store"
)

func (h *harness) gatedJob(t *testing.T, name, gateName string) core.Job {
	t.Helper()
	ctx := context.Background()
	src, err := h.store.Sources.Create(ctx, core.Source{Name: name + "-source", Kind: "gated", Config: core.Config{"gate": gateName}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.store.Destinations.Create(ctx, core.Destination{Name: name + "-dest", Kind: "memory", Config: core.Config{"bucket": name}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := h.store.Jobs.Create(ctx, core.Job{
		Slug: name, Name: name, SourceID: src.ID, DestinationIDs: []string{d.ID},
		Enabled: true, Compression: core.CompressionNone, Encryption: core.EncryptionNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	full, err := h.store.Jobs.Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	return full
}

func TestCancelAfterCleanDumpEndsCanceled(t *testing.T) {
	h := newHarness(t)
	g := newGate("clean-cancel", "payload for a canceled run")
	job := h.gatedJob(t, "clean-cancel", "clean-cancel")

	run, err := h.engine.EnqueueBackup(context.Background(), job, core.TriggerManual, "test")
	if err != nil {
		t.Fatal(err)
	}
	g.runID = run.ID
	g.onClose = func() { h.engine.Cancel(g.runID) }
	close(g.ready)

	finished := h.wait(t, run.ID)
	if finished.Status != core.RunCanceled {
		t.Fatalf("status = %s, want canceled (error %q)", finished.Status, finished.Error)
	}
	if finished.Error != "run canceled" {
		t.Fatalf("error = %q, want %q", finished.Error, "run canceled")
	}
	artifacts, _, err := h.store.Artifacts.List(context.Background(), store.ArtifactFilter{RunID: run.ID, Page: store.Page{Limit: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("canceled run produced %d artifacts, want none", len(artifacts))
	}
}

func TestCancelWithFailingCloseEndsCanceled(t *testing.T) {
	h := newHarness(t)
	g := newGate("dirty-cancel", "payload for an aborted run")
	g.closeErr = io.ErrUnexpectedEOF
	job := h.gatedJob(t, "dirty-cancel", "dirty-cancel")

	run, err := h.engine.EnqueueBackup(context.Background(), job, core.TriggerManual, "test")
	if err != nil {
		t.Fatal(err)
	}
	g.runID = run.ID
	g.onClose = func() { h.engine.Cancel(g.runID) }
	close(g.ready)

	finished := h.wait(t, run.ID)
	if finished.Status != core.RunCanceled {
		t.Fatalf("status = %s, want canceled (error %q)", finished.Status, finished.Error)
	}
}

func TestTimedOutRunFailsNamingTheTimeout(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := h.store.Runs.Create(ctx, core.Run{
		ID: store.NewID(), Kind: core.RunBackup, Trigger: core.TriggerManual,
		Status: core.RunRunning, QueuedAt: time.Now().UTC(),
		Stages: []core.Stage{}, ArtifactIDs: []string{},
		Meta: map[string]string{"timeoutMinutes": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := newRunState(h.engine, run)
	rs.start(ctx)

	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	rs.finish(expired, context.DeadlineExceeded)

	got, err := h.store.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RunFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.Error != "run timed out after 1 minute" {
		t.Fatalf("error = %q, want it to name the timeout", got.Error)
	}
}

func TestTimedOutRunUsesTheDefaultTimeoutLabel(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := h.store.Runs.Create(ctx, core.Run{
		ID: store.NewID(), Kind: core.RunBackup, Trigger: core.TriggerManual,
		Status: core.RunRunning, QueuedAt: time.Now().UTC(),
		Stages: []core.Stage{}, ArtifactIDs: []string{}, Meta: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := newRunState(h.engine, run)
	rs.start(ctx)

	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	rs.finish(expired, context.DeadlineExceeded)

	got, err := h.store.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "run timed out after 360 minutes"
	if got.Error != want {
		t.Fatalf("error = %q, want %q", got.Error, want)
	}
}

func TestMissedScheduledRunIsReplayedAtStartup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "backvault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}

	src, err := st.Sources.Create(ctx, core.Source{Name: "missed-source", Kind: "fake", Config: core.Config{"payload": "missed"}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Destinations.Create(ctx, core.Destination{Name: "missed-dest", Kind: "memory", Config: core.Config{"bucket": "missed"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Jobs.Create(ctx, core.Job{
		Slug: "missed", Name: "missed", SourceID: src.ID, DestinationIDs: []string{d.ID},
		Enabled: true, Schedule: "*/5 * * * *", Timezone: "UTC",
		Compression: core.CompressionNone, Encryption: core.EncryptionNone,
	}); err != nil {
		t.Fatal(err)
	}

	logs := &strings.Builder{}
	e := New(Deps{
		Store:   st,
		Secrets: cipher,
		WorkDir: filepath.Join(dir, "work"),
		Logger:  slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.Stop(stopCtx)
	}()

	deadline := time.Now().Add(20 * time.Second)
	var runs []core.Run
	for time.Now().Before(deadline) {
		list, _, err := st.Runs.List(ctx, store.RunFilter{Page: store.Page{Limit: 10}})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) > 0 && list[0].Status.Terminal() {
			runs = list
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(runs) == 0 {
		t.Fatal("the missed schedule was not replayed at startup")
	}
	if runs[0].Trigger != core.TriggerSchedule {
		t.Fatalf("trigger = %s, want schedule", runs[0].Trigger)
	}
	if runs[0].Status != core.RunSuccess {
		t.Fatalf("status = %s, want success", runs[0].Status)
	}
	if !strings.Contains(logs.String(), "missed") {
		t.Fatalf("startup did not log the missed run, got %q", logs.String())
	}
}

func TestCanceledRunWithoutErrorEndsCanceled(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	run, err := h.store.Runs.Create(ctx, core.Run{
		ID: store.NewID(), Kind: core.RunBackup, Trigger: core.TriggerManual,
		Status: core.RunRunning, QueuedAt: time.Now().UTC(),
		Stages: []core.Stage{}, ArtifactIDs: []string{}, Meta: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	rs := newRunState(h.engine, run)
	rs.start(ctx)

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	rs.finish(canceled, nil)

	got, err := h.store.Runs.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RunCanceled {
		t.Fatalf("status = %s, want canceled", got.Status)
	}
}
