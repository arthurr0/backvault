package engine

import (
	"context"
	"strings"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/store"
)

func (e *Engine) watchOverdue(ctx context.Context) {
	settings := e.settings(ctx)
	interval := time.Duration(settings.OverdueCheckMinutes) * time.Minute
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.checkOverdue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkOverdue(ctx)
			current := e.settings(ctx)
			next := time.Duration(current.OverdueCheckMinutes) * time.Minute
			if next > 0 && next != interval {
				interval = next
				ticker.Reset(interval)
			}
		}
	}
}

func (e *Engine) checkOverdue(ctx context.Context) {
	jobs, err := e.store.Jobs.All(ctx)
	if err != nil {
		e.log.Error("overdue check failed", "error", err)
		return
	}
	states, err := e.store.JobStates.All(ctx)
	if err != nil {
		e.log.Error("overdue check failed", "error", err)
		return
	}
	now := time.Now().UTC()
	settings := e.settings(ctx)

	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		state := states[job.ID]
		overdue, reason := jobIsOverdue(job, state, settings, now)
		if overdue == state.Overdue && (!overdue || state.OverdueNotifiedAt != nil) {
			continue
		}
		var notifiedAt *time.Time
		if overdue {
			notifiedAt = &now
		}
		if err := e.store.JobStates.SetOverdue(ctx, job.ID, overdue, notifiedAt); err != nil {
			e.log.Error("could not update overdue flag", "job", job.Slug, "error", err)
			continue
		}
		job.Overdue = overdue
		e.publishJob(job)
		if overdue {
			e.log.Warn("job is overdue", "job", job.Slug, "reason", reason)
			e.notifier.dispatchJobEvent(ctx, job, core.EventJobOverdue, "Job is overdue", reason)
		} else {
			e.log.Info("job is no longer overdue", "job", job.Slug)
		}
	}
}

func jobIsOverdue(job core.Job, state store.JobState, settings core.Settings, now time.Time) (bool, string) {
	if job.SourceKind == "push" || strings.TrimSpace(job.Schedule) == "" {
		if job.ExpectedIntervalMinutes <= 0 {
			return false, ""
		}
		deadline := time.Duration(job.ExpectedIntervalMinutes) * time.Minute
		last := state.LastSuccessAt
		if last == nil {
			if job.CreatedAt.IsZero() || now.Sub(job.CreatedAt) <= deadline {
				return false, ""
			}
			return true, "no successful run has ever been received"
		}
		if now.Sub(*last) > deadline {
			return true, "last successful run was " + now.Sub(*last).Round(time.Minute).String() + " ago"
		}
		return false, ""
	}

	grace := 30 * time.Minute
	if state.LastDurationMS > 0 {
		expected := 2 * time.Duration(state.LastDurationMS) * time.Millisecond
		if expected > grace {
			grace = expected
		}
	}
	slot, err := previousSlot(job, settings.DefaultTimezone, now.Add(-25*time.Hour), now.Add(-grace))
	if err != nil || slot.IsZero() {
		return false, ""
	}
	if state.LastRun != nil && state.LastRun.StartedAt != nil && !state.LastRun.StartedAt.Before(slot) {
		return false, ""
	}
	return true, "scheduled run at " + slot.Format(time.RFC3339) + " did not happen"
}
