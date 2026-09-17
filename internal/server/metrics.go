package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/arthurr0/backvault/internal/core"
	"github.com/arthurr0/backvault/internal/engine"
	"github.com/arthurr0/backvault/internal/store"
	"github.com/arthurr0/backvault/internal/version"
)

var artifactStatuses = []core.ArtifactStatus{
	core.ArtifactPresent,
	core.ArtifactMissing,
	core.ArtifactPruned,
	core.ArtifactDeleted,
}

type metrics struct {
	store    *store.Store
	engine   *engine.Engine
	registry *prometheus.Registry

	runsTotal      *prometheus.CounterVec
	runDuration    *prometheus.HistogramVec
	runBytes       *prometheus.CounterVec
	destErrors     *prometheus.CounterVec
	notifyFailures *prometheus.CounterVec

	buildInfo        *prometheus.Desc
	runsRunning      *prometheus.Desc
	runsQueued       *prometheus.Desc
	jobs             *prometheus.Desc
	jobsOverdue      *prometheus.Desc
	lastSuccessByJob *prometheus.Desc
	artifacts        *prometheus.Desc
	artifactBytes    *prometheus.Desc
}

func newMetrics(st *store.Store, eng *engine.Engine) *metrics {
	m := &metrics{
		store:    st,
		engine:   eng,
		registry: prometheus.NewRegistry(),
		runsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backvault_runs_total",
			Help: "Runs that reached a terminal status, by job, kind and status.",
		}, []string{"job", "kind", "status"}),
		runDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "backvault_run_duration_seconds",
			Help:    "Wall clock duration of finished runs, by job and kind.",
			Buckets: []float64{1, 5, 15, 60, 300, 900, 1800, 3600, 7200, 21600},
		}, []string{"job", "kind"}),
		runBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backvault_run_bytes_total",
			Help: "Bytes uploaded, after compression and encryption, by job and destination.",
		}, []string{"job", "destination"}),
		destErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backvault_destination_errors_total",
			Help: "Upload and verify failures per destination.",
		}, []string{"destination"}),
		notifyFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "backvault_notification_failures_total",
			Help: "Notifications that could not be delivered, by channel.",
		}, []string{"channel"}),
		buildInfo: prometheus.NewDesc("backvault_build_info", "Always 1, carries the build identity.",
			[]string{"version", "commit", "go_version"}, nil),
		runsRunning: prometheus.NewDesc("backvault_runs_running", "Runs currently executing.", nil, nil),
		runsQueued:  prometheus.NewDesc("backvault_runs_queued", "Runs waiting for a worker slot.", nil, nil),
		jobs:        prometheus.NewDesc("backvault_jobs", "Number of jobs, split by enabled true or false.", []string{"enabled"}, nil),
		jobsOverdue: prometheus.NewDesc("backvault_jobs_overdue", "Jobs the overdue watcher currently considers late.", nil, nil),
		lastSuccessByJob: prometheus.NewDesc("backvault_job_last_success_timestamp_seconds",
			"Unix time of the last successful run of each job.", []string{"job"}, nil),
		artifacts: prometheus.NewDesc("backvault_artifacts", "Artifacts known to Backvault per destination and status.",
			[]string{"destination", "status"}, nil),
		artifactBytes: prometheus.NewDesc("backvault_artifact_bytes", "Stored bytes per destination, artifacts in status present.",
			[]string{"destination"}, nil),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.runsTotal,
		m.runDuration,
		m.runBytes,
		m.destErrors,
		m.notifyFailures,
		m,
	)
	return m
}

func (m *metrics) RunFinished(run core.Run) {
	job := run.JobSlug
	m.runsTotal.WithLabelValues(job, string(run.Kind), string(run.Status)).Inc()
	if run.DurationMS > 0 {
		m.runDuration.WithLabelValues(job, string(run.Kind)).Observe(float64(run.DurationMS) / 1000)
	}
}

func (m *metrics) RunBytes(jobSlug, destination string, n int64) {
	if n <= 0 {
		return
	}
	m.runBytes.WithLabelValues(jobSlug, destination).Add(float64(n))
}

func (m *metrics) DestinationError(destination string) {
	m.destErrors.WithLabelValues(destination).Inc()
}

func (m *metrics) NotificationFailure(channel string) {
	m.notifyFailures.WithLabelValues(channel).Inc()
}

func (m *metrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.buildInfo
	ch <- m.runsRunning
	ch <- m.runsQueued
	ch <- m.jobs
	ch <- m.jobsOverdue
	ch <- m.lastSuccessByJob
	ch <- m.artifacts
	ch <- m.artifactBytes
}

func (m *metrics) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info := version.Info()
	ch <- prometheus.MustNewConstMetric(m.buildInfo, prometheus.GaugeValue, 1, info.Version, info.Commit, info.GoVersion)

	queued := 0
	if m.engine != nil {
		queued = m.engine.QueuedRuns()
	}
	ch <- prometheus.MustNewConstMetric(m.runsQueued, prometheus.GaugeValue, float64(queued))

	running := 0
	if m.store != nil {
		if n, err := m.store.Runs.CountByStatus(ctx, core.RunRunning); err == nil {
			running = n
		}
	}
	ch <- prometheus.MustNewConstMetric(m.runsRunning, prometheus.GaugeValue, float64(running))

	var jobs []core.Job
	if m.store != nil {
		if list, err := m.store.Jobs.All(ctx); err == nil {
			jobs = list
		}
	}
	enabled, disabled, overdue := 0, 0, 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		} else {
			disabled++
		}
		if j.Overdue {
			overdue++
		}
	}
	ch <- prometheus.MustNewConstMetric(m.jobs, prometheus.GaugeValue, float64(enabled), "true")
	ch <- prometheus.MustNewConstMetric(m.jobs, prometheus.GaugeValue, float64(disabled), "false")
	ch <- prometheus.MustNewConstMetric(m.jobsOverdue, prometheus.GaugeValue, float64(overdue))

	if m.store == nil {
		return
	}

	if last, err := m.store.Runs.LastSuccessByJob(ctx); err == nil {
		for _, j := range jobs {
			if t, ok := last[j.ID]; ok {
				ch <- prometheus.MustNewConstMetric(m.lastSuccessByJob, prometheus.GaugeValue, float64(t.Unix()), j.Slug)
			}
		}
	}

	m.collectArtifacts(ctx, ch)
}

func (m *metrics) collectArtifacts(ctx context.Context, ch chan<- prometheus.Metric) {
	dests, _, err := m.store.Destinations.List(ctx, store.DestinationFilter{Page: store.Page{Limit: store.MaxLimit}})
	if err != nil {
		return
	}
	stats, err := m.store.Artifacts.StatsByDestination(ctx)
	if err != nil {
		return
	}
	counts := map[string]map[core.ArtifactStatus]store.ArtifactStat{}
	for _, s := range stats {
		byStatus, ok := counts[s.DestinationID]
		if !ok {
			byStatus = map[core.ArtifactStatus]store.ArtifactStat{}
			counts[s.DestinationID] = byStatus
		}
		byStatus[s.Status] = s
	}
	for _, d := range dests {
		byStatus := counts[d.ID]
		for _, status := range artifactStatuses {
			ch <- prometheus.MustNewConstMetric(m.artifacts, prometheus.GaugeValue,
				float64(byStatus[status].Count), d.Name, string(status))
		}
		for status, s := range byStatus {
			if isKnownArtifactStatus(status) {
				continue
			}
			ch <- prometheus.MustNewConstMetric(m.artifacts, prometheus.GaugeValue,
				float64(s.Count), d.Name, string(status))
		}
		ch <- prometheus.MustNewConstMetric(m.artifactBytes, prometheus.GaugeValue,
			float64(byStatus[core.ArtifactPresent].Bytes), d.Name)
	}
}

func isKnownArtifactStatus(s core.ArtifactStatus) bool {
	for _, known := range artifactStatuses {
		if known == s {
			return true
		}
	}
	return false
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(s.cfg.MetricsToken); token != "" {
		provided := strings.TrimSpace(r.URL.Query().Get("token"))
		if provided == "" {
			header := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(strings.ToLower(header), "bearer ") {
				provided = strings.TrimSpace(header[len("bearer "):])
			}
		}
		if provided != token {
			s.writeError(w, r, http.StatusUnauthorized, codeUnauthenticated, "metrics token required")
			return
		}
	}
	promhttp.HandlerFor(s.metrics.registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}
