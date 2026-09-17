package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type IngestOptions struct {
	Filename  string
	SHA256    string
	Packed    bool
	Size      int64
	CreatedBy string
}

func (e *Engine) Ingest(ctx context.Context, job core.Job, body io.Reader, opts IngestOptions) (core.Run, []core.Artifact, error) {
	run := e.newRun(job, core.RunIngest, core.TriggerIngest, opts.CreatedBy)
	if !e.tryLockJob(job.ID, run.ID) {
		return core.Run{}, nil, ErrJobBusy
	}
	defer e.releaseJob(job.ID, run.ID)

	saved, err := e.store.Runs.Create(ctx, run)
	if err != nil {
		return core.Run{}, nil, err
	}
	e.publishRun(saved)

	sem := e.currentSem()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		saved.Status = core.RunFailed
		saved.Error = "ingest was canceled while waiting for a worker slot"
		now := time.Now().UTC()
		saved.FinishedAt = &now
		_ = e.store.Runs.Save(context.WithoutCancel(ctx), saved)
		e.publishRun(saved)
		return core.Run{}, nil, ctx.Err()
	}
	defer func() { <-sem }()

	timeout := defaultTimeout
	if job.TimeoutMinutes > 0 {
		timeout = time.Duration(job.TimeoutMinutes) * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	e.mu.Lock()
	e.cancels[saved.ID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.cancels, saved.ID)
		e.mu.Unlock()
	}()

	rs := newRunState(e, saved)
	rs.start(runCtx)
	err = e.runIngest(runCtx, rs, job, body, opts)
	rs.finish(runCtx, err)

	if observer := e.currentObserver(); observer != nil {
		observer.RunFinished(rs.Run())
	}

	final := rs.Run()
	artifacts, listErr := e.store.Artifacts.ForRun(context.WithoutCancel(ctx), final.ID)
	if listErr != nil {
		return final, nil, listErr
	}
	return final, artifacts, err
}

func (e *Engine) runIngest(ctx context.Context, rs *runState, job core.Job, body io.Reader, opts IngestOptions) error {
	log := rs.Logger()

	prepare := rs.stage(ctx, "prepare")
	src, err := e.store.Sources.Get(ctx, job.SourceID)
	if err != nil {
		return prepare.Fail(ctx, fmt.Errorf("load source: %w", err))
	}
	destinations, err := e.loadDestinations(ctx, job)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	if len(destinations) == 0 {
		return prepare.Fail(ctx, errors.New("job has no destinations"))
	}
	passphrase, err := e.jobPassphrase(job)
	if err != nil {
		return prepare.Fail(ctx, err)
	}

	original := path.Base(strings.TrimSpace(opts.Filename))
	if original == "." || original == "/" {
		original = ""
	}
	packOpts := PackOptions{Passphrase: passphrase}
	var ext string
	if opts.Packed {
		compression, encryption, detected := DetectPack(original)
		packOpts.Compression = compression
		packOpts.Encryption = encryption
		ext = detected
	} else {
		packOpts.Compression = job.Compression
		packOpts.Level = job.CompressionLevel
		packOpts.Encryption = job.Encryption
		_, _, detected := DetectPack(original)
		ext = detected
		if packOpts.Encryption == core.EncryptionAge && passphrase == "" {
			return prepare.Fail(ctx, errors.New("encryption passphrase is required for age encryption"))
		}
	}
	if ext == "" {
		ext = "bin"
	}
	filename := BuildFilename(job.Slug, ext, time.Now().UTC(), packOpts)
	prepare.Done(ctx, fmt.Sprintf("%d destination(s)", len(destinations)))

	receive := rs.stage(ctx, "receive")
	sp, err := newSpool(e.workDir, rs.Run().ID, filename)
	if err != nil {
		return receive.Fail(ctx, err)
	}
	defer func() {
		if err := sp.Cleanup(); err != nil {
			e.log.Warn("could not clean spool directory", "run", rs.Run().ID, "error", err)
		}
	}()

	rawHash := sha256.New()
	reader := io.TeeReader(body, rawHash)
	spoolOpts := packOpts
	if opts.Packed {
		spoolOpts = PackOptions{}
	}
	if err := sp.Fill(ctx, reader, spoolOpts); err != nil {
		return receive.Fail(ctx, err)
	}
	rawSum := hex.EncodeToString(rawHash.Sum(nil))
	if want := strings.ToLower(strings.TrimSpace(opts.SHA256)); want != "" {
		got := rawSum
		if opts.Packed {
			got = sp.SHA256
		}
		if want != got {
			return receive.Fail(ctx, fmt.Errorf("sha256 mismatch: expected %s, received %s", want, got))
		}
		log.Info("checksum verified", "sha256", got)
	}
	log.Info("payload received", "bytes", sp.PackedBytes, "filename", filename)
	receive.Done(ctx, fmt.Sprintf("%d bytes received", sp.PackedBytes))

	pack := rs.stage(ctx, "pack")
	rs.setResult(ctx, filename, sp.SHA256, sp.RawBytes, sp.PackedBytes)
	if opts.Packed {
		pack.Done(ctx, "already packed by the client")
	} else if packOpts.passthrough() {
		pack.Done(ctx, "pass-through")
	} else {
		pack.Done(ctx, fmt.Sprintf("compression=%s encryption=%s, %d bytes", orNone(string(packOpts.Compression)), orNone(string(packOpts.Encryption)), sp.PackedBytes))
	}

	artifactPath := BuildPath(job.Slug, filename)
	artifacts, err := e.uploadAll(ctx, rs, job, src, destinations, sp, artifactPath, filename, packOpts)
	if err != nil {
		return err
	}

	if job.VerifyAfterUpload {
		e.verifyArtifacts(ctx, rs, destinations, artifacts)
	} else {
		rs.skippedStage(ctx, "verify", "disabled")
	}

	e.applyRetention(ctx, rs, job, destinations)

	notifyStage := rs.stage(ctx, "notify")
	notifyStage.Done(ctx, "queued")
	return nil
}
