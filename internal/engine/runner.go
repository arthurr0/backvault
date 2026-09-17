package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

func (e *Engine) newRun(job core.Job, kind core.RunKind, trigger core.RunTrigger, createdBy string) core.Run {
	run := core.Run{
		ID:          store.NewID(),
		JobID:       job.ID,
		JobSlug:     job.Slug,
		JobName:     job.Name,
		Kind:        kind,
		Trigger:     trigger,
		Status:      core.RunQueued,
		QueuedAt:    time.Now().UTC(),
		Stages:      []core.Stage{},
		ArtifactIDs: []string{},
		Meta:        map[string]string{},
		CreatedBy:   createdBy,
	}
	if job.TimeoutMinutes > 0 {
		run.Meta["timeoutMinutes"] = strconv.Itoa(job.TimeoutMinutes)
	}
	return run
}

func (e *Engine) EnqueueBackup(ctx context.Context, job core.Job, trigger core.RunTrigger, createdBy string) (core.Run, error) {
	run := e.newRun(job, core.RunBackup, trigger, createdBy)
	if !e.tryLockJob(job.ID, run.ID) {
		return core.Run{}, ErrJobBusy
	}
	saved, err := e.store.Runs.Create(ctx, run)
	if err != nil {
		e.releaseJob(job.ID, run.ID)
		return core.Run{}, err
	}
	jobID := job.ID
	t := &task{run: saved, execute: func(c context.Context, rs *runState) error {
		return e.runBackup(c, rs, jobID)
	}}
	if err := e.enqueue(ctx, t); err != nil {
		e.releaseJob(job.ID, run.ID)
		return core.Run{}, err
	}
	return saved, nil
}

func (e *Engine) runBackup(ctx context.Context, rs *runState, jobID string) error {
	log := rs.Logger()

	prepare := rs.stage(ctx, "prepare")
	job, err := e.store.Jobs.Get(ctx, jobID)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	src, err := e.store.Sources.Get(ctx, job.SourceID)
	if err != nil {
		return prepare.Fail(ctx, fmt.Errorf("load source: %w", err))
	}
	driver, srcCfg, err := e.SourceConfig(src)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	if err := driver.Validate(srcCfg); err != nil {
		return prepare.Fail(ctx, fmt.Errorf("source %s: %w", src.Name, err))
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
	opts := PackOptions{
		Compression: job.Compression,
		Level:       job.CompressionLevel,
		Encryption:  job.Encryption,
		Passphrase:  passphrase,
	}
	if opts.Encryption == core.EncryptionAge && opts.Passphrase == "" {
		return prepare.Fail(ctx, errors.New("encryption passphrase is required for age encryption"))
	}
	log.Info("run prepared", "job", job.Slug, "source", src.Kind, "destinations", len(destinations))
	prepare.Done(ctx, fmt.Sprintf("%d destination(s)", len(destinations)))

	if err := e.runHook(ctx, rs, "pre-command", job.PreCommand); err != nil {
		return err
	}

	dump := rs.stage(ctx, "dump")
	stream, err := driver.Backup(ctx, srcCfg, log)
	if err != nil {
		return dump.Fail(ctx, fmt.Errorf("dump failed: %w", err))
	}

	filename := BuildFilename(job.Slug, stream.Extension, time.Now().UTC(), opts)
	sp, err := newSpool(e.workDir, rs.Run().ID, filename)
	if err != nil {
		_ = stream.Reader.Close()
		return dump.Fail(ctx, err)
	}
	defer func() {
		if err := sp.Cleanup(); err != nil {
			e.log.Warn("could not clean spool directory", "run", rs.Run().ID, "error", err)
		}
	}()

	copyErr := sp.Fill(ctx, stream.Reader, opts)
	closeErr := stream.Reader.Close()
	if copyErr != nil {
		return dump.Fail(ctx, fmt.Errorf("dump failed: %w", copyErr))
	}
	if closeErr != nil {
		return dump.Fail(ctx, fmt.Errorf("dump did not complete: %w", closeErr))
	}
	log.Info("dump complete", "rawBytes", sp.RawBytes)
	dump.Done(ctx, fmt.Sprintf("%d bytes read", sp.RawBytes))

	pack := rs.stage(ctx, "pack")
	rs.setResult(ctx, filename, sp.SHA256, sp.RawBytes, sp.PackedBytes)
	packMsg := "pass-through"
	if !opts.passthrough() {
		packMsg = fmt.Sprintf("compression=%s encryption=%s", orNone(string(opts.Compression)), orNone(string(opts.Encryption)))
	}
	log.Info("pack complete", "bytes", sp.PackedBytes, "sha256", sp.SHA256, "filename", filename)
	pack.Done(ctx, packMsg+fmt.Sprintf(", %d bytes", sp.PackedBytes))

	path := BuildPath(job.Slug, filename)
	artifacts, uploadErr := e.uploadAll(ctx, rs, job, src, destinations, sp, path, filename, opts)
	if uploadErr != nil {
		return uploadErr
	}

	if job.VerifyAfterUpload {
		e.verifyArtifacts(ctx, rs, destinations, artifacts)
	} else {
		rs.skippedStage(ctx, "verify", "disabled")
	}

	e.applyRetention(ctx, rs, job, destinations)

	if err := e.runHook(ctx, rs, "post-command", job.PostCommand); err != nil {
		return err
	}

	notifyStage := rs.stage(ctx, "notify")
	notifyStage.Done(ctx, "queued")
	return nil
}

func (e *Engine) loadDestinations(ctx context.Context, job core.Job) ([]core.Destination, error) {
	out := make([]core.Destination, 0, len(job.DestinationIDs))
	for _, id := range job.DestinationIDs {
		d, err := e.store.Destinations.Get(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load destination %s: %w", id, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func (e *Engine) jobPassphrase(job core.Job) (string, error) {
	if job.EncryptionPassphrase == "" {
		return "", nil
	}
	plain, err := e.secrets.Decrypt(job.EncryptionPassphrase)
	if err != nil {
		return "", fmt.Errorf("read job passphrase: %w", err)
	}
	return plain, nil
}

func (e *Engine) uploadAll(ctx context.Context, rs *runState, job core.Job, src core.Source, destinations []core.Destination, sp *spool, path, filename string, opts PackOptions) ([]core.Artifact, error) {
	log := rs.Logger()
	artifacts := []core.Artifact{}
	failures := 0

	for _, d := range destinations {
		stage := rs.stage(ctx, "upload:"+d.Name)
		err := e.uploadOne(ctx, rs, job, d, sp, path)
		if err != nil {
			failures++
			if observer := e.currentObserver(); observer != nil {
				observer.DestinationError(d.Name)
			}
			log.Error("upload failed", "destination", d.Name, "error", err.Error())
			stage.set(ctx, core.StageFailed, err.Error())
			continue
		}
		artifact := core.Artifact{
			JobID:           job.ID,
			JobSlug:         job.Slug,
			JobName:         job.Name,
			RunID:           rs.Run().ID,
			DestinationID:   d.ID,
			DestinationName: d.Name,
			DestinationKind: d.Kind,
			Path:            path,
			Filename:        filename,
			Size:            sp.PackedBytes,
			SHA256:          sp.SHA256,
			Compression:     opts.Compression,
			Encryption:      opts.Encryption,
			SourceKind:      src.Kind,
			Extension:       extensionOf(filename),
			Status:          core.ArtifactPresent,
			CreatedAt:       time.Now().UTC(),
		}
		saved, err := e.store.Artifacts.Create(ctx, artifact)
		if err != nil {
			failures++
			stage.set(ctx, core.StageFailed, err.Error())
			continue
		}
		artifacts = append(artifacts, saved)
		rs.addArtifact(ctx, saved)
		if observer := e.currentObserver(); observer != nil {
			observer.RunBytes(job.Slug, d.Name, sp.PackedBytes)
		}
		stage.Done(ctx, fmt.Sprintf("%d bytes", sp.PackedBytes))
	}

	if failures > 0 && len(artifacts) == 0 {
		return nil, fmt.Errorf("all %d destination(s) failed", failures)
	}
	if failures > 0 {
		rs.Warn(fmt.Sprintf("%d of %d destinations failed", failures, len(destinations)))
	}
	return artifacts, nil
}

func (e *Engine) uploadOne(ctx context.Context, rs *runState, job core.Job, d core.Destination, sp *spool, path string) error {
	log := rs.Logger()
	attempts := job.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Duration(job.RetryDelaySeconds) * time.Second
	if delay <= 0 {
		delay = 5 * time.Second
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			wait := backoff(delay, attempt-1)
			log.Warn("retrying upload", "destination", d.Name, "attempt", attempt, "waitSeconds", int(wait.Seconds()))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		lastErr = e.putOnce(ctx, d, sp, path, log)
		if lastErr == nil {
			log.Info("upload complete", "destination", d.Name, "path", path)
			return nil
		}
		if errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded) {
			return lastErr
		}
	}
	return lastErr
}

func (e *Engine) putOnce(ctx context.Context, d core.Destination, sp *spool, path string, log *slog.Logger) error {
	client, err := e.OpenDestination(ctx, d, log)
	if err != nil {
		return err
	}
	defer client.Close()
	f, err := sp.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	if err := client.Put(ctx, path, f, sp.PackedBytes); err != nil {
		return fmt.Errorf("upload to %s: %w", d.Name, err)
	}
	return nil
}

func (e *Engine) verifyArtifacts(ctx context.Context, rs *runState, destinations []core.Destination, artifacts []core.Artifact) {
	log := rs.Logger()
	stage := rs.stage(ctx, "verify")
	byID := map[string]core.Destination{}
	for _, d := range destinations {
		byID[d.ID] = d
	}
	problems := 0
	for _, a := range artifacts {
		d, ok := byID[a.DestinationID]
		if !ok {
			continue
		}
		if err := e.verifyOne(ctx, rs, d, a, log); err != nil {
			problems++
			if observer := e.currentObserver(); observer != nil {
				observer.DestinationError(d.Name)
			}
			log.Warn("verification failed", "artifact", a.Filename, "destination", d.Name, "error", err.Error())
		}
	}
	if problems > 0 {
		rs.Warn(fmt.Sprintf("%d artifact(s) failed verification", problems))
		stage.set(ctx, core.StageFailed, fmt.Sprintf("%d artifact(s) failed verification", problems))
		return
	}
	stage.Done(ctx, fmt.Sprintf("%d artifact(s) verified", len(artifacts)))
}

func (e *Engine) verifyOne(ctx context.Context, rs *runState, d core.Destination, a core.Artifact, log *slog.Logger) error {
	client, err := e.OpenDestination(ctx, d, log)
	if err != nil {
		return err
	}
	defer client.Close()
	obj, err := client.Stat(ctx, a.Path)
	if err != nil || obj == nil {
		_ = e.store.Artifacts.SetStatus(ctx, a.ID, core.ArtifactMissing, time.Now().UTC())
		a.Status = core.ArtifactMissing
		e.publishArtifact(a)
		if err == nil {
			err = errors.New("object not found on destination")
		}
		return err
	}
	if obj.Size != a.Size {
		_ = e.store.Artifacts.SetStatus(ctx, a.ID, core.ArtifactMissing, time.Now().UTC())
		a.Status = core.ArtifactMissing
		e.publishArtifact(a)
		return fmt.Errorf("size mismatch: expected %d, found %d", a.Size, obj.Size)
	}
	now := time.Now().UTC()
	if err := e.store.Artifacts.SetVerified(ctx, a.ID, now); err != nil {
		return err
	}
	a.VerifiedAt = &now
	e.publishArtifact(a)
	log.Info("artifact verified", "artifact", a.Filename, "destination", d.Name, "bytes", obj.Size)
	return nil
}

func (e *Engine) applyRetention(ctx context.Context, rs *runState, job core.Job, destinations []core.Destination) {
	log := rs.Logger()
	stage := rs.stage(ctx, "retention")
	settings := e.settings(ctx)
	retention := EffectiveRetention(job, settings)
	if retention.IsZero() {
		stage.Done(ctx, "no retention configured")
		return
	}
	removed := 0
	problems := 0
	for _, d := range destinations {
		items, err := e.store.Artifacts.ForJobDestination(ctx, job.ID, d.ID, core.ArtifactPresent)
		if err != nil {
			problems++
			log.Error("retention could not list artifacts", "destination", d.Name, "error", err.Error())
			continue
		}
		_, remove := Plan(items, retention, time.Now().UTC())
		if len(remove) == 0 {
			continue
		}
		n, err := e.pruneArtifacts(ctx, d, remove, log)
		removed += n
		if err != nil {
			problems++
		}
	}
	if problems > 0 {
		rs.Warn("retention encountered errors")
		stage.set(ctx, core.StageFailed, fmt.Sprintf("%d destination(s) failed, %d artifact(s) removed", problems, removed))
		return
	}
	stage.Done(ctx, fmt.Sprintf("%d artifact(s) removed", removed))
}

func (e *Engine) pruneArtifacts(ctx context.Context, d core.Destination, remove []core.Artifact, log *slog.Logger) (int, error) {
	client, err := e.OpenDestination(ctx, d, log)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	removed := 0
	var lastErr error
	for _, a := range remove {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := client.Delete(ctx, a.Path); err != nil {
			lastErr = fmt.Errorf("delete %s on %s: %w", a.Path, d.Name, err)
			log.Error("could not delete artifact", "artifact", a.Filename, "destination", d.Name, "error", err.Error())
			continue
		}
		if err := e.store.Artifacts.SetStatus(ctx, a.ID, core.ArtifactPruned, time.Now().UTC()); err != nil {
			lastErr = err
			continue
		}
		a.Status = core.ArtifactPruned
		e.publishArtifact(a)
		removed++
		log.Info("artifact pruned", "artifact", a.Filename, "destination", d.Name)
	}
	return removed, lastErr
}

func (e *Engine) runHook(ctx context.Context, rs *runState, name, command string) error {
	if strings.TrimSpace(command) == "" {
		rs.skippedStage(ctx, name, "not configured")
		return nil
	}
	stage := rs.stage(ctx, name)
	log := rs.Logger()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		log.Info(name, "output", line)
	}
	if err != nil {
		return stage.Fail(ctx, fmt.Errorf("%s failed: %w", name, err))
	}
	stage.Done(ctx, "ok")
	return nil
}

func extensionOf(filename string) string {
	_, _, ext := DetectPack(filename)
	return ext
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return v
}

func backoff(base time.Duration, step int) time.Duration {
	wait := base
	for i := 1; i < step; i++ {
		wait *= 2
		if wait >= maxRetryBackoff {
			return maxRetryBackoff
		}
	}
	if wait > maxRetryBackoff {
		return maxRetryBackoff
	}
	return wait
}
