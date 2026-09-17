package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/arthurr0/backvault/internal/core"
)

func scrapeMetrics(t *testing.T, env *testEnv) string {
	t.Helper()
	resp := env.do(env.request("GET", "/metrics", nil))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestMetricsExportsDocumentedSeries(t *testing.T) {
	env := newEnv(t)
	env.setup()
	ctx := context.Background()

	src := env.createSource("files")
	dst := env.createDestination("primary", "metrics-bucket")
	job := env.createJob("Metrics Job", src.ID, []string{dst.ID}, map[string]any{"schedule": "@daily"})

	run, err := env.store.Runs.Create(ctx, core.Run{
		JobID: job.ID, JobSlug: job.Slug, JobName: job.Name,
		Kind: core.RunBackup, Trigger: core.TriggerManual, Status: core.RunQueued,
		QueuedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Now().UTC()
	run.Status = core.RunSuccess
	run.StartedAt = &finished
	run.FinishedAt = &finished
	run.DurationMS = 2500
	run.Bytes = 4096
	if err := env.store.Runs.Save(ctx, run); err != nil {
		t.Fatal(err)
	}

	if _, err := env.store.Artifacts.Create(ctx, core.Artifact{
		JobID: job.ID, JobSlug: job.Slug, JobName: job.Name, RunID: run.ID,
		DestinationID: dst.ID, DestinationName: dst.Name, DestinationKind: dst.Kind,
		Path: "metrics/artifact.dump", Filename: "artifact.dump", Size: 4096,
		Status: core.ArtifactPresent, CreatedAt: finished,
	}); err != nil {
		t.Fatal(err)
	}

	observer := env.server.Metrics()
	observer.RunFinished(run)
	observer.RunBytes(job.Slug, dst.Name, run.Bytes)
	observer.DestinationError(dst.Name)
	observer.NotificationFailure("ops-email")

	body := scrapeMetrics(t, env)

	documented := []string{
		"backvault_build_info",
		"backvault_runs_total",
		"backvault_run_duration_seconds",
		"backvault_run_bytes_total",
		"backvault_runs_running",
		"backvault_runs_queued",
		"backvault_jobs",
		"backvault_jobs_overdue",
		"backvault_job_last_success_timestamp_seconds",
		"backvault_artifacts",
		"backvault_artifact_bytes",
		"backvault_destination_errors_total",
		"backvault_notification_failures_total",
	}
	for _, name := range documented {
		if !strings.Contains(body, "\n# TYPE "+name+" ") && !strings.HasPrefix(body, "# TYPE "+name+" ") {
			t.Errorf("metric %s is not exported", name)
		}
	}

	for _, sample := range []string{
		`backvault_jobs{enabled="true"} 1`,
		`backvault_jobs{enabled="false"} 0`,
		`backvault_build_info{commit=`,
		`backvault_runs_total{job="metrics-job",kind="backup",status="success"} 1`,
		`backvault_run_bytes_total{destination="primary",job="metrics-job"} 4096`,
		`backvault_destination_errors_total{destination="primary"} 1`,
		`backvault_notification_failures_total{channel="ops-email"} 1`,
		`backvault_artifacts{destination="primary",status="present"} 1`,
		`backvault_artifacts{destination="primary",status="missing"} 0`,
		`backvault_artifact_bytes{destination="primary"} 4096`,
		`backvault_job_last_success_timestamp_seconds{job="metrics-job"}`,
		"backvault_run_duration_seconds_bucket{job=\"metrics-job\",kind=\"backup\",le=\"5\"} 1",
		"backvault_runs_queued 0",
		"backvault_runs_running 0",
		"backvault_jobs_overdue 0",
	} {
		if !strings.Contains(body, sample) {
			t.Errorf("metrics output is missing %q", sample)
		}
	}

	for _, name := range []string{"go_goroutines", "process_start_time_seconds"} {
		if !strings.Contains(body, name) {
			t.Errorf("runtime collector metric %s is not exported", name)
		}
	}
}

func TestMetricsTypesMatchDocumentation(t *testing.T) {
	env := newEnv(t)
	env.setup()
	dst := env.createDestination("archive", "types-bucket")

	observer := env.server.Metrics()
	observer.RunFinished(core.Run{JobSlug: "typed", Kind: core.RunVerify, Status: core.RunWarning, DurationMS: 1000})
	observer.RunBytes("typed", dst.Name, 1)
	observer.DestinationError(dst.Name)
	observer.NotificationFailure("ops-email")

	body := scrapeMetrics(t, env)
	want := map[string]string{
		"backvault_build_info":                  "gauge",
		"backvault_runs_running":                "gauge",
		"backvault_runs_queued":                 "gauge",
		"backvault_jobs":                        "gauge",
		"backvault_jobs_overdue":                "gauge",
		"backvault_artifacts":                   "gauge",
		"backvault_artifact_bytes":              "gauge",
		"backvault_runs_total":                  "counter",
		"backvault_run_bytes_total":             "counter",
		"backvault_destination_errors_total":    "counter",
		"backvault_notification_failures_total": "counter",
		"backvault_run_duration_seconds":        "histogram",
	}
	for name, kind := range want {
		line := "# TYPE " + name + " " + kind
		if !strings.Contains(body, line) {
			t.Errorf("expected %q in metrics output", line)
		}
	}
}
