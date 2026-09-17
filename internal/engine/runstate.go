package engine

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

type runState struct {
	e   *Engine
	mu  sync.Mutex
	run core.Run

	rl  *runLog
	log *slog.Logger

	warned    bool
	warnMsg   string
	successAt *time.Time
}

func newRunState(e *Engine, run core.Run) *runState {
	rs := &runState{e: e, run: run}
	rs.rl = newRunLog(func(text string) {
		ctx := context.Background()
		if _, err := e.store.Runs.AppendLog(ctx, run.ID, text); err != nil {
			e.log.Error("could not append run log", "run", run.ID, "error", err)
			return
		}
		e.bus.Publish(Event{Type: TopicRunLog, RunID: run.ID, JobID: run.JobID, Data: text})
	})
	rs.log = slog.New(newRunLogHandler(rs.rl, slog.LevelInfo))
	return rs
}

func (rs *runState) Logger() *slog.Logger { return rs.log }

func (rs *runState) Run() core.Run {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return cloneRun(rs.run)
}

func cloneRun(r core.Run) core.Run {
	out := r
	out.Stages = append([]core.Stage(nil), r.Stages...)
	out.ArtifactIDs = append([]string(nil), r.ArtifactIDs...)
	if r.Meta != nil {
		meta := make(map[string]string, len(r.Meta))
		for k, v := range r.Meta {
			meta[k] = v
		}
		out.Meta = meta
	}
	return out
}

func (rs *runState) update(ctx context.Context, fn func(r *core.Run)) {
	rs.mu.Lock()
	fn(&rs.run)
	snapshot := cloneRun(rs.run)
	rs.mu.Unlock()
	if err := rs.e.store.Runs.Save(context.WithoutCancel(ctx), snapshot); err != nil {
		rs.e.log.Error("could not save run", "run", snapshot.ID, "error", err)
	}
	rs.e.publishRun(snapshot)
}

func (rs *runState) start(ctx context.Context) {
	now := time.Now().UTC()
	rs.update(ctx, func(r *core.Run) {
		r.Status = core.RunRunning
		r.StartedAt = &now
	})
}

func (rs *runState) Warn(msg string) {
	rs.mu.Lock()
	rs.warned = true
	if rs.warnMsg == "" {
		rs.warnMsg = msg
	}
	rs.mu.Unlock()
}

type stageHandle struct {
	rs   *runState
	name string
}

func (rs *runState) stage(ctx context.Context, name string) *stageHandle {
	now := time.Now().UTC()
	rs.update(ctx, func(r *core.Run) {
		r.Stages = append(r.Stages, core.Stage{Name: name, Status: core.StageRunning, StartedAt: &now})
	})
	rs.log.Info("stage started", "stage", name)
	return &stageHandle{rs: rs, name: name}
}

func (rs *runState) skippedStage(ctx context.Context, name, message string) {
	now := time.Now().UTC()
	rs.update(ctx, func(r *core.Run) {
		r.Stages = append(r.Stages, core.Stage{Name: name, Status: core.StageSkipped, StartedAt: &now, FinishedAt: &now, Message: message})
	})
}

func (s *stageHandle) set(ctx context.Context, status core.StageStatus, message string) {
	now := time.Now().UTC()
	s.rs.update(ctx, func(r *core.Run) {
		for i := len(r.Stages) - 1; i >= 0; i-- {
			if r.Stages[i].Name == s.name && r.Stages[i].Status == core.StageRunning {
				r.Stages[i].Status = status
				r.Stages[i].FinishedAt = &now
				r.Stages[i].Message = message
				return
			}
		}
	})
}

func (s *stageHandle) Done(ctx context.Context, message string) {
	s.set(ctx, core.StageSuccess, message)
	s.rs.log.Info("stage finished", "stage", s.name)
}

func (s *stageHandle) Fail(ctx context.Context, err error) error {
	s.set(ctx, core.StageFailed, err.Error())
	s.rs.log.Error("stage failed", "stage", s.name, "error", err.Error())
	return err
}

func (rs *runState) addArtifact(ctx context.Context, a core.Artifact) {
	rs.update(ctx, func(r *core.Run) {
		r.ArtifactIDs = append(r.ArtifactIDs, a.ID)
	})
	rs.e.publishArtifact(a)
}

func (rs *runState) setResult(ctx context.Context, filename, sha string, rawBytes, packedBytes int64) {
	rs.update(ctx, func(r *core.Run) {
		r.Filename = filename
		r.SHA256 = sha
		r.RawBytes = rawBytes
		r.Bytes = packedBytes
	})
}

func (rs *runState) timeoutLabel() string {
	minutes := int(defaultTimeout / time.Minute)
	rs.mu.Lock()
	raw := rs.run.Meta["timeoutMinutes"]
	rs.mu.Unlock()
	if v, err := strconv.Atoi(raw); err == nil && v > 0 {
		minutes = v
	}
	if minutes == 1 {
		return "1 minute"
	}
	return strconv.Itoa(minutes) + " minutes"
}

func (rs *runState) finish(ctx context.Context, runErr error) {
	now := time.Now().UTC()
	saveCtx := context.WithoutCancel(ctx)

	rs.mu.Lock()
	warned := rs.warned
	warnMsg := rs.warnMsg
	rs.mu.Unlock()

	canceled := errors.Is(ctx.Err(), context.Canceled) || (runErr != nil && errors.Is(runErr, context.Canceled))
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || (runErr != nil && errors.Is(runErr, context.DeadlineExceeded))

	status := core.RunSuccess
	message := ""
	switch {
	case canceled:
		status = core.RunCanceled
		message = "run canceled"
	case timedOut:
		status = core.RunFailed
		message = "run timed out after " + rs.timeoutLabel()
	case runErr != nil:
		status = core.RunFailed
		message = runErr.Error()
	case warned:
		status = core.RunWarning
		message = warnMsg
	}

	if status == core.RunSuccess || status == core.RunWarning {
		rs.log.Info("run finished", "status", string(status))
	} else {
		rs.log.Error("run finished", "status", string(status), "error", message)
	}
	rs.rl.Close()

	rs.update(saveCtx, func(r *core.Run) {
		r.Status = status
		r.Error = message
		r.FinishedAt = &now
		if r.StartedAt != nil {
			r.DurationMS = now.Sub(*r.StartedAt).Milliseconds()
		}
		for i := range r.Stages {
			if r.Stages[i].Status == core.StageRunning {
				r.Stages[i].Status = core.StageFailed
				r.Stages[i].FinishedAt = &now
				if r.Stages[i].Message == "" {
					r.Stages[i].Message = message
				}
			}
		}
	})

	run := rs.Run()
	if run.JobID != "" {
		var successAt *time.Time
		if status == core.RunSuccess || status == core.RunWarning {
			successAt = &now
		}
		if err := rs.e.store.JobStates.SetLastRun(saveCtx, run.JobID, run.Summary(), successAt); err != nil {
			rs.e.log.Error("could not save job state", "job", run.JobID, "error", err)
		}
		if status == core.RunSuccess || status == core.RunWarning {
			if err := rs.e.store.JobStates.SetOverdue(saveCtx, run.JobID, false, nil); err != nil {
				rs.e.log.Error("could not clear overdue flag", "job", run.JobID, "error", err)
			}
		}
		if job, err := rs.e.store.Jobs.Get(saveCtx, run.JobID); err == nil {
			rs.e.publishJob(job)
		}
	}
}
