package engine

import (
	"context"
	"fmt"

	"github.com/arthurr0/backvault/internal/core"
)

func (e *Engine) EnqueuePrune(ctx context.Context, job core.Job, createdBy string) (core.Run, error) {
	run := e.newRun(job, core.RunPrune, core.TriggerManual, createdBy)
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
		return e.runPrune(c, rs, jobID)
	}}
	if err := e.enqueue(ctx, t); err != nil {
		e.releaseJob(job.ID, run.ID)
		return core.Run{}, err
	}
	return saved, nil
}

func (e *Engine) runPrune(ctx context.Context, rs *runState, jobID string) error {
	prepare := rs.stage(ctx, "prepare")
	job, err := e.store.Jobs.Get(ctx, jobID)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	destinations, err := e.loadDestinations(ctx, job)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	prepare.Done(ctx, fmt.Sprintf("%d destination(s)", len(destinations)))
	e.applyRetention(ctx, rs, job, destinations)
	return nil
}

func (e *Engine) EnqueueVerify(ctx context.Context, artifact core.Artifact, createdBy string) (core.Run, error) {
	job := core.Job{ID: artifact.JobID, Slug: artifact.JobSlug, Name: artifact.JobName}
	run := e.newRun(job, core.RunVerify, core.TriggerManual, createdBy)
	run.Meta["artifactId"] = artifact.ID
	saved, err := e.store.Runs.Create(ctx, run)
	if err != nil {
		return core.Run{}, err
	}
	artifactID := artifact.ID
	t := &task{run: saved, execute: func(c context.Context, rs *runState) error {
		return e.runVerify(c, rs, artifactID)
	}}
	if err := e.enqueue(ctx, t); err != nil {
		return core.Run{}, err
	}
	return saved, nil
}

func (e *Engine) runVerify(ctx context.Context, rs *runState, artifactID string) error {
	prepare := rs.stage(ctx, "prepare")
	artifact, err := e.store.Artifacts.Get(ctx, artifactID)
	if err != nil {
		return prepare.Fail(ctx, err)
	}
	d, err := e.store.Destinations.Get(ctx, artifact.DestinationID)
	if err != nil {
		return prepare.Fail(ctx, fmt.Errorf("load destination: %w", err))
	}
	prepare.Done(ctx, artifact.Filename)

	stage := rs.stage(ctx, "verify")
	if err := e.verifyOne(ctx, rs, d, artifact, rs.Logger()); err != nil {
		rs.Warn(err.Error())
		stage.set(ctx, core.StageFailed, err.Error())
		return nil
	}
	rs.setResult(ctx, artifact.Filename, artifact.SHA256, 0, artifact.Size)
	stage.Done(ctx, "artifact present and the size matches")

	return nil
}
