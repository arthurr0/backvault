package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/store"
)

type harness struct {
	engine  *Engine
	store   *store.Store
	secrets *secrets.Cipher
	dir     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "backvault.db"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	e := New(Deps{
		Store:   st,
		Secrets: cipher,
		WorkDir: filepath.Join(dir, "work"),
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		BaseURL: "https://backvault.test",
	})
	if err := e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.Stop(stopCtx)
		_ = st.Close()
	})
	return &harness{engine: e, store: st, secrets: cipher, dir: dir}
}

func (h *harness) job(t *testing.T, name string, srcCfg core.Config, destCfgs []core.Config, mutate func(*core.Job)) core.Job {
	t.Helper()
	ctx := context.Background()
	src, err := h.store.Sources.Create(ctx, core.Source{Name: name + "-source", Kind: "fake", Config: srcCfg})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i, cfg := range destCfgs {
		d, err := h.store.Destinations.Create(ctx, core.Destination{Name: name + "-dest-" + string(rune('a'+i)), Kind: "memory", Config: cfg})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	job := core.Job{
		Slug: name, Name: name, SourceID: src.ID, DestinationIDs: ids,
		Enabled: true, Compression: core.CompressionNone, Encryption: core.EncryptionNone,
	}
	if mutate != nil {
		mutate(&job)
	}
	saved, err := h.store.Jobs.Create(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	full, err := h.store.Jobs.Get(ctx, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	return full
}

func (h *harness) wait(t *testing.T, runID string) core.Run {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.store.Runs.Get(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status.Terminal() {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish in time", runID)
	return core.Run{}
}

func TestBackupEndToEnd(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "nightly", core.Config{"payload": "backup payload"}, []core.Config{{"bucket": "b1"}}, nil)

	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunSuccess {
		t.Fatalf("status = %s, error = %s", run.Status, run.Error)
	}
	if run.RawBytes != int64(len("backup payload")) || run.Bytes != run.RawBytes {
		t.Fatalf("bytes: raw=%d packed=%d", run.RawBytes, run.Bytes)
	}
	if run.SHA256 == "" || run.Filename == "" {
		t.Fatalf("missing checksum or filename: %+v", run)
	}
	if !strings.HasPrefix(run.Filename, "nightly-") || !strings.HasSuffix(run.Filename, ".dump") {
		t.Fatalf("filename = %q", run.Filename)
	}

	wantStages := []string{"prepare", "pre-command", "dump", "pack", "upload:nightly-dest-a", "verify", "retention", "post-command", "notify"}
	if len(run.Stages) != len(wantStages) {
		t.Fatalf("stages = %d: %+v", len(run.Stages), run.Stages)
	}
	for i, name := range wantStages {
		if run.Stages[i].Name != name {
			t.Fatalf("stage %d = %q, want %q", i, run.Stages[i].Name, name)
		}
	}

	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d", len(artifacts))
	}
	a := artifacts[0]
	if a.Path != "nightly/"+run.Filename || a.Size != run.Bytes || a.SHA256 != run.SHA256 {
		t.Fatalf("artifact = %+v", a)
	}

	reader, name, err := h.engine.OpenArtifact(ctx, a, DownloadOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "backup payload" {
		t.Fatalf("downloaded %q", data)
	}
	if name != run.Filename {
		t.Fatalf("download name = %q", name)
	}

	log, err := h.store.Runs.Log(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "fake dump started") || !strings.Contains(log, "run finished") {
		t.Fatalf("log = %q", log)
	}

	updated, err := h.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastRun == nil || updated.LastRun.ID != run.ID {
		t.Fatalf("job last run = %+v", updated.LastRun)
	}
	if updated.ArtifactCount != 1 {
		t.Fatalf("artifact count = %d", updated.ArtifactCount)
	}
}

func TestBackupWithCompressionAndEncryption(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	payload := strings.Repeat("backvault-payload.", 500)
	passphrase, err := h.engine.secrets.Encrypt("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	job := h.job(t, "secure", core.Config{"payload": payload}, []core.Config{{"bucket": "b-secure"}}, func(j *core.Job) {
		j.Compression = core.CompressionZstd
		j.Encryption = core.EncryptionAge
		j.EncryptionPassphrase = passphrase
		j.VerifyAfterUpload = true
	})
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunSuccess {
		t.Fatalf("status = %s, error = %s", run.Status, run.Error)
	}
	if !strings.HasSuffix(run.Filename, ".dump.zst.age") {
		t.Fatalf("filename = %q", run.Filename)
	}
	if run.Bytes >= run.RawBytes {
		t.Fatalf("expected compression to shrink the payload: %d >= %d", run.Bytes, run.RawBytes)
	}
	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts: %v %d", err, len(artifacts))
	}
	if artifacts[0].VerifiedAt == nil {
		t.Fatal("artifact was not verified")
	}
	reader, _, err := h.engine.OpenArtifact(ctx, artifacts[0], DownloadOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Fatalf("round trip failed: %d bytes", len(data))
	}

	raw, _, err := h.engine.OpenArtifact(ctx, artifacts[0], DownloadOptions{Raw: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	rawData, err := io.ReadAll(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawData) == payload {
		t.Fatal("raw download should not be decrypted")
	}
}

func TestPartialFailureYieldsWarning(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "partial", core.Config{"payload": "data"}, []core.Config{{"bucket": "ok"}, {"bucket": "bad", "fail_put": true}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunWarning {
		t.Fatalf("status = %s, error = %s", run.Status, run.Error)
	}
	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts: %v %d", err, len(artifacts))
	}
}

func TestAllDestinationsFailing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "broken", core.Config{"payload": "data"}, []core.Config{{"bucket": "bad", "fail_put": true}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunFailed {
		t.Fatalf("status = %s", run.Status)
	}
	if !strings.Contains(run.Error, "destination") {
		t.Fatalf("error = %q", run.Error)
	}
}

func TestDumpCloseErrorFailsRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "closefail", core.Config{"payload": "data", "fail_close": true}, []core.Config{{"bucket": "cf"}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunFailed {
		t.Fatalf("status = %s", run.Status)
	}
	if !strings.Contains(run.Error, "did not complete") {
		t.Fatalf("error = %q", run.Error)
	}
}

func TestRetentionRemovesOldArtifacts(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "retained", core.Config{"payload": "x"}, []core.Config{{"bucket": "ret"}}, func(j *core.Job) {
		j.Retention = core.Retention{KeepLast: 2}
	})
	for i := 0; i < 4; i++ {
		queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
		if err != nil {
			t.Fatal(err)
		}
		run := h.wait(t, queued.ID)
		if run.Status != core.RunSuccess {
			t.Fatalf("run %d status = %s, error = %s", i, run.Status, run.Error)
		}
		time.Sleep(1100 * time.Millisecond)
	}
	present, total, err := h.store.Artifacts.List(ctx, store.ArtifactFilter{JobID: job.ID, Status: string(core.ArtifactPresent)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("present artifacts = %d", total)
	}
	pruned, prunedTotal, err := h.store.Artifacts.List(ctx, store.ArtifactFilter{JobID: job.ID, Status: string(core.ArtifactPruned)})
	if err != nil {
		t.Fatal(err)
	}
	if prunedTotal != 2 {
		t.Fatalf("pruned artifacts = %d", prunedTotal)
	}
	st := storeFor("ret")
	st.mu.Lock()
	files := len(st.files)
	st.mu.Unlock()
	if files != 2 {
		t.Fatalf("files left on destination = %d", files)
	}
	_ = present
	_ = pruned
}

func TestJobLockRejectsConcurrentRuns(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	src, err := h.store.Sources.Create(ctx, core.Source{Name: "blocking-source", Kind: "blocking"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.store.Destinations.Create(ctx, core.Destination{Name: "blocking-dest", Kind: "memory", Config: core.Config{"bucket": "blk"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.store.Jobs.Create(ctx, core.Job{Slug: "blocked", Name: "blocked", SourceID: src.ID, DestinationIDs: []string{d.ID}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester"); !errors.Is(err, ErrJobBusy) {
		t.Fatalf("expected ErrJobBusy, got %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !h.engine.IsRunning(first.ID) {
		time.Sleep(10 * time.Millisecond)
	}
	if !h.engine.Cancel(first.ID) {
		t.Fatal("cancel did not find the run")
	}
	run := h.wait(t, first.ID)
	if run.Status != core.RunCanceled {
		t.Fatalf("status = %s, error = %s", run.Status, run.Error)
	}
	if h.engine.JobBusy(job.ID) {
		t.Fatal("job lock was not released")
	}
}

func TestRestoreToPath(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "restorable", core.Config{"payload": "restore me"}, []core.Config{{"bucket": "rp"}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts: %v %d", err, len(artifacts))
	}
	target := filepath.Join(h.dir, "restored", "payload.dump")
	restoreRun, err := h.engine.EnqueueRestore(ctx, RestoreRequest{Artifact: artifacts[0], Mode: RestoreToPath, TargetPath: target, CreatedBy: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	done := h.wait(t, restoreRun.ID)
	if done.Status != core.RunSuccess {
		t.Fatalf("restore status = %s, error = %s", done.Status, done.Error)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "restore me" {
		t.Fatalf("restored %q", data)
	}
}

func TestRestoreToSource(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "srcrestore", core.Config{"payload": "into source", "name": "target-a"}, []core.Config{{"bucket": "rs"}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts: %v %d", err, len(artifacts))
	}
	restoreRun, err := h.engine.EnqueueRestore(ctx, RestoreRequest{Artifact: artifacts[0], Mode: RestoreToSource, CreatedBy: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	done := h.wait(t, restoreRun.ID)
	if done.Status != core.RunSuccess {
		t.Fatalf("restore status = %s, error = %s", done.Status, done.Error)
	}
	if string(restoredPayload("target-a")) != "into source" {
		t.Fatalf("restored payload = %q", restoredPayload("target-a"))
	}
}

func TestIngest(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "pushed", core.Config{}, []core.Config{{"bucket": "ing"}}, func(j *core.Job) {
		j.Compression = core.CompressionGzip
	})
	payload := "pushed payload"
	run, artifacts, err := h.engine.Ingest(ctx, job, strings.NewReader(payload), IngestOptions{
		Filename:  "db.sql",
		SHA256:    sha256Hex(payload),
		CreatedBy: "token:ci",
	})
	if err != nil {
		t.Fatalf("ingest: %v (%s)", err, run.Error)
	}
	if run.Status != core.RunSuccess {
		t.Fatalf("status = %s, error = %s", run.Status, run.Error)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d", len(artifacts))
	}
	if !strings.HasSuffix(artifacts[0].Filename, ".sql.gz") {
		t.Fatalf("filename = %q", artifacts[0].Filename)
	}
	reader, _, err := h.engine.OpenArtifact(ctx, artifacts[0], DownloadOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Fatalf("ingested payload = %q", data)
	}
}

func TestIngestPackedAndChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "packed", core.Config{}, []core.Config{{"bucket": "pk"}}, nil)

	payload := "already packed bytes"
	run, artifacts, err := h.engine.Ingest(ctx, job, strings.NewReader(payload), IngestOptions{
		Filename: "dump.sql.zst",
		Packed:   true,
		SHA256:   sha256Hex(payload),
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if run.Status != core.RunSuccess {
		t.Fatalf("status = %s error = %s", run.Status, run.Error)
	}
	if artifacts[0].Compression != core.CompressionZstd {
		t.Fatalf("compression = %q", artifacts[0].Compression)
	}
	if !strings.HasSuffix(artifacts[0].Filename, ".sql.zst") {
		t.Fatalf("filename = %q", artifacts[0].Filename)
	}

	bad, _, err := h.engine.Ingest(ctx, job, strings.NewReader("other"), IngestOptions{Filename: "dump.sql", SHA256: sha256Hex("mismatch")})
	if err == nil {
		t.Fatal("expected a checksum mismatch error")
	}
	if bad.Status != core.RunFailed {
		t.Fatalf("status = %s", bad.Status)
	}
}

func TestPruneRun(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "pruner", core.Config{"payload": "x"}, []core.Config{{"bucket": "pr"}}, nil)
	for i := 0; i < 2; i++ {
		queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
		if err != nil {
			t.Fatal(err)
		}
		h.wait(t, queued.ID)
		time.Sleep(1100 * time.Millisecond)
	}
	current, err := h.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	current.Retention = core.Retention{KeepLast: 1}
	if _, err := h.store.Jobs.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	reloaded, err := h.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	pruneRun, err := h.engine.EnqueuePrune(ctx, reloaded, "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := h.wait(t, pruneRun.ID)
	if done.Status != core.RunSuccess {
		t.Fatalf("prune status = %s, error = %s", done.Status, done.Error)
	}
	_, total, err := h.store.Artifacts.List(ctx, store.ArtifactFilter{JobID: job.ID, Status: string(core.ArtifactPresent)})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("present artifacts = %d", total)
	}
}

func TestVerifyRunDetectsMissingArtifact(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	job := h.job(t, "verifier", core.Config{"payload": "x"}, []core.Config{{"bucket": "vf"}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts: %v %d", err, len(artifacts))
	}
	st := storeFor("vf")
	st.mu.Lock()
	st.files = map[string][]byte{}
	st.mu.Unlock()

	verifyRun, err := h.engine.EnqueueVerify(ctx, artifacts[0], "tester")
	if err != nil {
		t.Fatal(err)
	}
	done := h.wait(t, verifyRun.ID)
	if done.Status != core.RunWarning {
		t.Fatalf("verify status = %s", done.Status)
	}
	updated, err := h.store.Artifacts.Get(ctx, artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != core.ArtifactMissing {
		t.Fatalf("artifact status = %s", updated.Status)
	}
}

func TestNotificationsOnFailure(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	resetCapturedEvents()
	if _, err := h.store.Channels.Create(ctx, core.NotificationChannel{Name: "capture", Kind: "capture", Enabled: true, Config: core.Config{"token": "x"}}); err != nil {
		t.Fatal(err)
	}
	job := h.job(t, "notified", core.Config{"fail_backup": true}, []core.Config{{"bucket": "nt"}}, nil)
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunFailed {
		t.Fatalf("status = %s", run.Status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events := capturedEvents()
		for _, ev := range events {
			if ev.Type == core.EventRunFailed && ev.Run != nil && ev.Run.ID == run.ID {
				if ev.Link != "https://backvault.test/runs/"+run.ID {
					t.Fatalf("link = %q", ev.Link)
				}
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no failure notification was sent")
}

func TestEventsBus(t *testing.T) {
	bus := NewBus()
	ch, cancel := bus.Subscribe(4)
	bus.Publish(Event{Type: TopicRunUpdated, RunID: "a"})
	select {
	case ev := <-ch:
		if ev.RunID != "a" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event received")
	}
	for i := 0; i < 20; i++ {
		bus.Publish(Event{Type: TopicRunUpdated, RunID: "flood"})
	}
	if bus.Dropped() == 0 {
		t.Fatal("expected slow subscriber drops")
	}
	cancel()
	bus.Publish(Event{Type: TopicRunUpdated, RunID: "after"})
	bus.Close()
}

func TestScheduleHelpers(t *testing.T) {
	job := core.Job{Schedule: "0 2 * * *", Timezone: "UTC", Enabled: true}
	from := time.Date(2024, 6, 15, 1, 0, 0, 0, time.UTC)
	next, err := NextRun(job, "UTC", from)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || !next.Equal(time.Date(2024, 6, 15, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %v", next)
	}
	list, err := NextRuns(job, "UTC", from, 3)
	if err != nil || len(list) != 3 {
		t.Fatalf("next runs: %v %v", list, err)
	}
	if _, err := ParseSchedule("not a cron"); err == nil {
		t.Fatal("expected a parse error")
	}
	if _, err := ParseSchedule("@daily"); err != nil {
		t.Fatalf("descriptor schedules must parse: %v", err)
	}
	if _, err := LoadLocation("Mars/Olympus"); err == nil {
		t.Fatal("expected an unknown timezone error")
	}
	manual := core.Job{Enabled: true}
	if next, err := NextRun(manual, "UTC", from); err != nil || next != nil {
		t.Fatalf("manual job should have no next run: %v %v", next, err)
	}
}

func TestQueuedRunsReportsWaitingRuns(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	settings, err := h.store.Settings.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.MaxConcurrentRuns = 1
	if _, err := h.store.Settings.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err := h.engine.ReloadSettings(ctx); err != nil {
		t.Fatal(err)
	}

	if n := h.engine.QueuedRuns(); n != 0 {
		t.Fatalf("queued runs before enqueue = %d", n)
	}

	src, err := h.store.Sources.Create(ctx, core.Source{Name: "queued-source", Kind: "blocking"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.store.Destinations.Create(ctx, core.Destination{Name: "queued-dest", Kind: "memory", Config: core.Config{"bucket": "q"}})
	if err != nil {
		t.Fatal(err)
	}
	runs := make([]core.Run, 0, 2)
	for i := 0; i < 2; i++ {
		slug := fmt.Sprintf("queued-%d", i)
		job, err := h.store.Jobs.Create(ctx, core.Job{Slug: slug, Name: slug, SourceID: src.ID, DestinationIDs: []string{d.ID}, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		run, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && h.engine.QueuedRuns() != 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if n := h.engine.QueuedRuns(); n != 1 {
		t.Fatalf("queued runs while one slot is taken = %d", n)
	}

	for _, run := range runs {
		deadline = time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && !h.engine.IsRunning(run.ID) {
			time.Sleep(10 * time.Millisecond)
		}
		if !h.engine.Cancel(run.ID) {
			t.Fatalf("cancel did not find run %s", run.ID)
		}
		h.wait(t, run.ID)
	}
	if n := h.engine.QueuedRuns(); n != 0 {
		t.Fatalf("queued runs after drain = %d", n)
	}
}
