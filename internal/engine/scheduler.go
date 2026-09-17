package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/arthurr0/backvault/internal/core"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

func ParseSchedule(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("schedule is empty")
	}
	sched, err := cronParser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", spec, err)
	}
	return sched, nil
}

func LoadLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q: %w", name, err)
	}
	return loc, nil
}

func NextRun(job core.Job, defaultTimezone string, from time.Time) (*time.Time, error) {
	if strings.TrimSpace(job.Schedule) == "" || !job.Enabled {
		return nil, nil
	}
	sched, err := ParseSchedule(job.Schedule)
	if err != nil {
		return nil, err
	}
	tz := job.Timezone
	if strings.TrimSpace(tz) == "" {
		tz = defaultTimezone
	}
	loc, err := LoadLocation(tz)
	if err != nil {
		return nil, err
	}
	next := sched.Next(from.In(loc)).UTC()
	if next.IsZero() {
		return nil, nil
	}
	return &next, nil
}

func NextRuns(job core.Job, defaultTimezone string, from time.Time, count int) ([]time.Time, error) {
	if strings.TrimSpace(job.Schedule) == "" {
		return nil, nil
	}
	sched, err := ParseSchedule(job.Schedule)
	if err != nil {
		return nil, err
	}
	tz := job.Timezone
	if strings.TrimSpace(tz) == "" {
		tz = defaultTimezone
	}
	loc, err := LoadLocation(tz)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, count)
	cursor := from.In(loc)
	for i := 0; i < count; i++ {
		cursor = sched.Next(cursor)
		if cursor.IsZero() {
			break
		}
		out = append(out, cursor.UTC())
	}
	return out, nil
}

type Scheduler struct {
	e *Engine

	mu      sync.Mutex
	crons   []*cron.Cron
	running bool

	reload chan struct{}
}

func newScheduler(e *Engine) *Scheduler {
	return &Scheduler{e: e, reload: make(chan struct{}, 1)}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	if err := s.apply(ctx); err != nil {
		return err
	}
	s.runMissed(ctx)

	s.e.wg.Add(1)
	go func() {
		defer s.e.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.reload:
				if err := s.apply(ctx); err != nil {
					s.e.log.Error("scheduler reload failed", "error", err)
				}
			case <-ticker.C:
				if err := s.refreshNextRuns(ctx); err != nil {
					s.e.log.Error("could not refresh next run times", "error", err)
				}
			}
		}
	}()
	return nil
}

func (s *Scheduler) Reload() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	crons := s.crons
	s.crons = nil
	s.running = false
	s.mu.Unlock()
	for _, c := range crons {
		c.Stop()
	}
}

func (s *Scheduler) apply(ctx context.Context) error {
	jobs, err := s.e.store.Jobs.All(ctx)
	if err != nil {
		return err
	}
	settings := s.e.settings(ctx)

	byTZ := map[string][]core.Job{}
	for _, job := range jobs {
		if !job.Enabled || strings.TrimSpace(job.Schedule) == "" {
			continue
		}
		if job.SourceKind == "push" {
			continue
		}
		tz := job.Timezone
		if strings.TrimSpace(tz) == "" {
			tz = settings.DefaultTimezone
		}
		byTZ[tz] = append(byTZ[tz], job)
	}

	var fresh []*cron.Cron
	for tz, list := range byTZ {
		loc, err := LoadLocation(tz)
		if err != nil {
			s.e.log.Error("skipping jobs with invalid timezone", "timezone", tz, "error", err)
			continue
		}
		c := cron.New(cron.WithLocation(loc), cron.WithParser(cronParser), cron.WithLogger(cron.DiscardLogger))
		for _, job := range list {
			jobID := job.ID
			slug := job.Slug
			if _, err := c.AddFunc(job.Schedule, func() { s.trigger(jobID, slug) }); err != nil {
				s.e.log.Error("could not schedule job", "job", slug, "error", err)
				continue
			}
		}
		c.Start()
		fresh = append(fresh, c)
	}

	s.mu.Lock()
	old := s.crons
	s.crons = fresh
	s.mu.Unlock()
	for _, c := range old {
		c.Stop()
	}

	return s.refreshNextRuns(ctx)
}

func (s *Scheduler) trigger(jobID, slug string) {
	ctx := s.e.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	job, err := s.e.store.Jobs.Get(ctx, jobID)
	if err != nil {
		s.e.log.Error("scheduled job disappeared", "job", slug, "error", err)
		return
	}
	if !job.Enabled {
		return
	}
	if _, err := s.e.EnqueueBackup(ctx, job, core.TriggerSchedule, "scheduler"); err != nil {
		s.e.log.Warn("scheduled run not started", "job", slug, "error", err)
		return
	}
	s.e.log.Info("scheduled run queued", "job", slug)
}

func (s *Scheduler) refreshNextRuns(ctx context.Context) error {
	jobs, err := s.e.store.Jobs.All(ctx)
	if err != nil {
		return err
	}
	settings := s.e.settings(ctx)
	now := time.Now().UTC()
	for _, job := range jobs {
		next, err := NextRun(job, settings.DefaultTimezone, now)
		if err != nil {
			s.e.log.Warn("could not compute next run", "job", job.Slug, "error", err)
			next = nil
		}
		if equalTimePtr(job.NextRunAt, next) {
			continue
		}
		if err := s.e.store.JobStates.SetNextRun(ctx, job.ID, next); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) runMissed(ctx context.Context) {
	jobs, err := s.e.store.Jobs.All(ctx)
	if err != nil {
		s.e.log.Error("could not check missed runs", "error", err)
		return
	}
	settings := s.e.settings(ctx)
	now := time.Now().UTC()
	window := now.Add(-24 * time.Hour)

	for _, job := range jobs {
		if !job.Enabled || strings.TrimSpace(job.Schedule) == "" || job.SourceKind == "push" {
			continue
		}
		slot, err := previousSlot(job, settings.DefaultTimezone, window, now)
		if err != nil || slot.IsZero() {
			continue
		}
		if job.LastRun != nil && job.LastRun.StartedAt != nil && !job.LastRun.StartedAt.Before(slot) {
			continue
		}
		s.e.log.Warn("running job missed while the scheduler was down", "job", job.Slug, "missedAt", slot)
		if _, err := s.e.EnqueueBackup(ctx, job, core.TriggerSchedule, "missed-run"); err != nil {
			s.e.log.Warn("missed run not started", "job", job.Slug, "error", err)
		}
	}
}

func previousSlot(job core.Job, defaultTimezone string, from, until time.Time) (time.Time, error) {
	sched, err := ParseSchedule(job.Schedule)
	if err != nil {
		return time.Time{}, err
	}
	tz := job.Timezone
	if strings.TrimSpace(tz) == "" {
		tz = defaultTimezone
	}
	loc, err := LoadLocation(tz)
	if err != nil {
		return time.Time{}, err
	}
	cursor := from.In(loc)
	var last time.Time
	for i := 0; i < 2000; i++ {
		next := sched.Next(cursor)
		if next.IsZero() || next.After(until) {
			break
		}
		last = next.UTC()
		cursor = next
	}
	return last, nil
}

func equalTimePtr(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}
