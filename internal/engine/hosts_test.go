package engine

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/remote"
)

func (h *harness) host(t *testing.T, name string) core.Host {
	t.Helper()
	private, _, err := remote.GenerateKey("backvault@test")
	if err != nil {
		t.Fatal(err)
	}
	host := core.Host{
		Name: name, Address: "10.10.10.10", Port: 22, User: "backup",
		Auth: core.HostAuthKey, PrivateKey: private, ConnectTimeout: 1,
	}
	stored, err := h.secrets.EncryptHost(host)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := h.store.Hosts.Create(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func (h *harness) remoteJob(t *testing.T, name, kind, hostID string) (core.Source, core.Job) {
	t.Helper()
	ctx := context.Background()
	src, err := h.store.Sources.Create(ctx, core.Source{Name: name + "-source", Kind: kind, HostID: hostID, Config: core.Config{"payload": "remote payload"}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := h.store.Destinations.Create(ctx, core.Destination{Name: name + "-dest", Kind: "memory", Config: core.Config{"bucket": name}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.store.Jobs.Create(ctx, core.Job{
		Slug: name, Name: name, SourceID: src.ID, DestinationIDs: []string{d.ID},
		Enabled: true, Compression: core.CompressionNone, Encryption: core.EncryptionNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	full, err := h.store.Jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.Sources.Get(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored, full
}

func TestLoadHostDecryptsSecrets(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	created := h.host(t, "vault-host")

	if !strings.HasPrefix(created.PrivateKey, "enc:v1:") {
		t.Fatalf("private key was not encrypted at rest: %q", created.PrivateKey)
	}
	loaded, err := h.engine.LoadHost(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loaded.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("private key was not decrypted: %q", loaded.PrivateKey)
	}
	if err := remote.Validate(loaded); err != nil {
		t.Fatalf("decrypted host does not validate: %v", err)
	}
}

func TestRemoteDriverReceivesHost(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	resetSeenHosts()

	host := h.host(t, "remote-01")
	src, job := h.remoteJob(t, "remote-backup", "remotefake", host.ID)

	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunSuccess {
		t.Fatalf("run status = %s (%s)", run.Status, run.Error)
	}
	if got := seenHost("backup"); got != "remote-01" {
		t.Fatalf("backup saw host %q", got)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := h.engine.TestSource(ctx, src, log)
	if !result.OK {
		t.Fatalf("test result = %+v", result)
	}
	if got := seenHost("test"); got != "remote-01" {
		t.Fatalf("test saw host %q", got)
	}

	artifacts, err := h.store.Artifacts.ForRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %d", len(artifacts))
	}
	restoreRun, err := h.engine.EnqueueRestore(ctx, RestoreRequest{Artifact: artifacts[0], Mode: RestoreToSource, CreatedBy: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	done := h.wait(t, restoreRun.ID)
	if done.Status != core.RunSuccess {
		t.Fatalf("restore status = %s (%s)", done.Status, done.Error)
	}
	if got := seenHost("restore"); got != "remote-01" {
		t.Fatalf("restore saw host %q", got)
	}
}

func TestLocalDriverNeverSeesHost(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	resetSeenHosts()

	host := h.host(t, "remote-02")
	src, job := h.remoteJob(t, "local-driver", "fake", host.ID)

	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunFailed {
		t.Fatalf("expected the run to fail, got %s", run.Status)
	}
	if !strings.Contains(run.Error, "cannot run on a host") {
		t.Fatalf("unexpected error %q", run.Error)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := h.engine.TestSource(ctx, src, log)
	if result.OK || !strings.Contains(result.Message, "cannot run on a host") {
		t.Fatalf("test result = %+v", result)
	}
	if got := seenHost("backup"); got != "" {
		t.Fatalf("a driver without the remote capability saw host %q", got)
	}
}

func TestPlainSourceCarriesNoHost(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	resetSeenHosts()

	_, job := h.remoteJob(t, "no-host", "remotefake", "")
	queued, err := h.engine.EnqueueBackup(ctx, job, core.TriggerManual, "tester")
	if err != nil {
		t.Fatal(err)
	}
	run := h.wait(t, queued.ID)
	if run.Status != core.RunSuccess {
		t.Fatalf("run status = %s (%s)", run.Status, run.Error)
	}
	if got := seenHost("backup"); got != noHostSeen {
		t.Fatalf("expected no host, saw %q", got)
	}
}

func TestHostTestReportsUnreachable(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	created := h.host(t, "unreachable")
	host, err := h.engine.LoadHost(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	host.Address = "192.0.2.1"
	host.ConnectTimeout = 1

	result := h.engine.TestHost(ctx, host, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if result.OK {
		t.Fatalf("expected the test to fail, got %+v", result)
	}
	if result.Message == "" {
		t.Fatal("expected an error message")
	}
	if strings.Contains(result.Message, "PRIVATE KEY") {
		t.Fatalf("message leaks the private key: %q", result.Message)
	}
	if result.DurationMS > 20000 {
		t.Fatalf("test took %dms", result.DurationMS)
	}
}
