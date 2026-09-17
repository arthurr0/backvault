package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/secrets"
	"github.com/arthurr0/backvault/internal/store"
)

var (
	ErrJobBusy     = errors.New("job is already running")
	ErrQueueFull   = errors.New("run queue is full")
	ErrNotRunning  = errors.New("engine is not running")
	ErrRunNotFound = errors.New("run is not active")
)

const (
	defaultTimeout   = 6 * time.Hour
	maxRetryBackoff  = 10 * time.Minute
	queueCapacity    = 256
	notifyTimeout    = 30 * time.Second
	driverTestPeriod = 30 * time.Second
)

type Observer interface {
	RunFinished(run core.Run)
	RunBytes(jobSlug, destination string, n int64)
	DestinationError(destination string)
	NotificationFailure(channel string)
}

type Deps struct {
	Store    *store.Store
	Secrets  *secrets.Cipher
	Bus      *Bus
	WorkDir  string
	Logger   *slog.Logger
	BaseURL  string
	Observer Observer
}

type task struct {
	run     core.Run
	execute func(ctx context.Context, rs *runState) error
}

type Engine struct {
	store    *store.Store
	secrets  *secrets.Cipher
	bus      *Bus
	workDir  string
	log      *slog.Logger
	baseURL  string
	observer Observer

	tasks  chan *task
	queued atomic.Int64

	mu      sync.Mutex
	sem     chan struct{}
	locks   map[string]string
	cancels map[string]context.CancelFunc
	running bool

	scheduler *Scheduler
	notifier  *Notifier

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func New(d Deps) *Engine {
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	bus := d.Bus
	if bus == nil {
		bus = NewBus()
	}
	e := &Engine{
		store:    d.Store,
		secrets:  d.Secrets,
		bus:      bus,
		workDir:  d.WorkDir,
		log:      log,
		baseURL:  d.BaseURL,
		observer: d.Observer,
		tasks:    make(chan *task, queueCapacity),
		sem:      make(chan struct{}, 2),
		locks:    map[string]string{},
		cancels:  map[string]context.CancelFunc{},
	}
	e.scheduler = newScheduler(e)
	e.notifier = newNotifier(e)
	return e
}

func (e *Engine) Bus() *Bus { return e.bus }

func (e *Engine) Store() *store.Store { return e.store }

func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = true
	e.ctx, e.cancel = context.WithCancel(context.WithoutCancel(ctx))
	e.mu.Unlock()

	settings, err := e.store.Settings.Get(ctx)
	if err != nil {
		return err
	}
	e.setConcurrency(settings.MaxConcurrentRuns)

	if err := e.recoverInterruptedRuns(ctx); err != nil {
		return err
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.dispatch()
	}()

	e.notifier.start(e.ctx)

	if err := e.scheduler.Start(e.ctx); err != nil {
		return err
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.watchOverdue(e.ctx)
	}()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.housekeeping(e.ctx)
	}()

	return nil
}

func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	e.running = false
	cancel := e.cancel
	e.mu.Unlock()

	e.scheduler.Stop()
	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		e.waitForRuns()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		e.CancelAll()
		e.waitForRuns()
	}
	e.wg.Wait()
	return nil
}

func (e *Engine) waitForRuns() {
	for {
		e.mu.Lock()
		n := len(e.cancels)
		e.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *Engine) CancelAll() {
	e.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(e.cancels))
	for _, c := range e.cancels {
		cancels = append(cancels, c)
	}
	e.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (e *Engine) Cancel(runID string) bool {
	e.mu.Lock()
	cancel, ok := e.cancels[runID]
	e.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

func (e *Engine) IsRunning(runID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.cancels[runID]
	return ok
}

func (e *Engine) setConcurrency(n int) {
	if n <= 0 {
		n = 2
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if cap(e.sem) == n {
		return
	}
	e.sem = make(chan struct{}, n)
}

func (e *Engine) ReloadSettings(ctx context.Context) error {
	settings, err := e.store.Settings.Get(ctx)
	if err != nil {
		return err
	}
	e.setConcurrency(settings.MaxConcurrentRuns)
	return nil
}

func (e *Engine) currentSem() chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sem
}

func (e *Engine) dispatch() {
	for {
		select {
		case <-e.ctx.Done():
			return
		case t := <-e.tasks:
			sem := e.currentSem()
			select {
			case sem <- struct{}{}:
				e.queued.Add(-1)
			case <-e.ctx.Done():
				e.queued.Add(-1)
				e.abandon(t, "server is shutting down")
				return
			}
			e.wg.Add(1)
			go func(t *task, sem chan struct{}) {
				defer e.wg.Done()
				defer func() { <-sem }()
				e.execute(t)
			}(t, sem)
		}
	}
}

func (e *Engine) abandon(t *task, reason string) {
	ctx := context.Background()
	run := t.run
	run.Status = core.RunFailed
	run.Error = reason
	now := time.Now().UTC()
	run.FinishedAt = &now
	_ = e.store.Runs.Save(ctx, run)
	e.releaseJob(run.JobID, run.ID)
	e.publishRun(run)
}

func (e *Engine) tryLockJob(jobID, runID string) bool {
	if jobID == "" {
		return true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, busy := e.locks[jobID]; busy {
		return false
	}
	e.locks[jobID] = runID
	return true
}

func (e *Engine) releaseJob(jobID, runID string) {
	if jobID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if cur, ok := e.locks[jobID]; ok && cur == runID {
		delete(e.locks, jobID)
	}
}

func (e *Engine) JobBusy(jobID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, busy := e.locks[jobID]
	return busy
}

func (e *Engine) enqueue(ctx context.Context, t *task) error {
	e.mu.Lock()
	running := e.running
	e.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	select {
	case e.tasks <- t:
		e.queued.Add(1)
		e.publishRun(t.run)
		return nil
	default:
		return ErrQueueFull
	}
}

func (e *Engine) QueuedRuns() int {
	n := e.queued.Load()
	if n < 0 {
		return 0
	}
	return int(n)
}

func (e *Engine) execute(t *task) {
	parent := e.ctx
	if parent == nil {
		parent = context.Background()
	}
	timeout := defaultTimeout
	if v, ok := t.run.Meta["timeoutMinutes"]; ok {
		if d, err := time.ParseDuration(v + "m"); err == nil && d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	defer cancel()

	e.mu.Lock()
	e.cancels[t.run.ID] = cancel
	e.mu.Unlock()

	rs := newRunState(e, t.run)
	rs.start(ctx)

	err := t.execute(ctx, rs)
	rs.finish(ctx, err)

	e.mu.Lock()
	delete(e.cancels, t.run.ID)
	e.mu.Unlock()
	e.releaseJob(rs.Run().JobID, t.run.ID)

	if observer := e.currentObserver(); observer != nil {
		observer.RunFinished(rs.Run())
	}
}

func (e *Engine) publishRun(run core.Run) {
	e.bus.Publish(Event{Type: TopicRunUpdated, RunID: run.ID, JobID: run.JobID, Data: run})
}

func (e *Engine) publishJob(job core.Job) {
	e.bus.Publish(Event{Type: TopicJobUpdated, JobID: job.ID, Data: job})
}

func (e *Engine) publishArtifact(a core.Artifact) {
	e.bus.Publish(Event{Type: TopicArtifactUpdated, JobID: a.JobID, Data: a})
}

func (e *Engine) recoverInterruptedRuns(ctx context.Context) error {
	runs, err := e.store.Runs.Unfinished(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		run.Status = core.RunFailed
		run.Error = "run was interrupted by a server restart"
		run.FinishedAt = &now
		if run.StartedAt != nil {
			run.DurationMS = now.Sub(*run.StartedAt).Milliseconds()
		}
		for i := range run.Stages {
			if run.Stages[i].Status == core.StageRunning {
				run.Stages[i].Status = core.StageFailed
				run.Stages[i].FinishedAt = &now
				run.Stages[i].Message = "interrupted"
			}
		}
		if err := e.store.Runs.Save(ctx, run); err != nil {
			return err
		}
		e.log.Warn("marked interrupted run as failed", "run", run.ID, "job", run.JobSlug)
	}
	return nil
}

func (e *Engine) housekeeping(ctx context.Context) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	e.cleanupOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.cleanupOnce(ctx)
		}
	}
}

func (e *Engine) cleanupOnce(ctx context.Context) {
	settings, err := e.store.Settings.Get(ctx)
	if err != nil {
		e.log.Error("cleanup could not read settings", "error", err)
		return
	}
	res, err := e.store.Cleanup(ctx, settings, time.Now().UTC())
	if err != nil {
		e.log.Error("cleanup failed", "error", err)
		return
	}
	if res.Runs > 0 || res.Audit > 0 || res.Sessions > 0 {
		e.log.Info("cleanup done", "runs", res.Runs, "audit", res.Audit, "sessions", res.Sessions)
	}
}

func (e *Engine) settings(ctx context.Context) core.Settings {
	s, err := e.store.Settings.Get(context.WithoutCancel(ctx))
	if err != nil {
		e.log.Error("could not read settings", "error", err)
		return store.DefaultSettings()
	}
	return s
}

func (e *Engine) runLink(ctx context.Context, runID string) string {
	base := e.settings(ctx).BaseURL
	if base == "" {
		base = e.baseURL
	}
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/runs/%s", base, runID)
}

func (e *Engine) SchedulerRunning() bool {
	return e.scheduler != nil && e.scheduler.Running()
}

func (e *Engine) ReloadSchedule() {
	if e.scheduler != nil {
		e.scheduler.Reload()
	}
}

func (e *Engine) SetObserver(o Observer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observer = o
}

func (e *Engine) currentObserver() Observer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.observer
}
