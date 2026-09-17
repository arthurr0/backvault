# Monitoring

Backvault exposes a liveness probe, a readiness probe and Prometheus metrics, which together answer the
only question that matters: did every backup that should have run actually run, and can it be
restored.

## Probes

Both probes are registered twice, at the root and under the API prefix: `/healthz` and
`/api/v1/healthz` are the same handler, and so are `/readyz` and `/api/v1/readyz`. Both paths are
public. The `/api/v1` variants also carry `Cache-Control: no-store`, which matters behind a caching
proxy. Use whichever suits the probe configuration you already have.

### /healthz

Liveness. No authentication, no database access. It answers `200` as soon as the HTTP server is up
and keeps answering `200` even while the database is unhappy.

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

```json
{"status": "ok", "version": "1.4.0", "time": "2026-09-17T09:31:04Z"}
```

Use it for the container healthcheck and for process supervision. A failure here means the process
is wedged or gone, and restarting is the right response. The Docker image already runs this check
every 30 seconds.

### /readyz

Readiness. No authentication. It pings the database with a three second timeout and checks that the
scheduler loop is running, and returns `503` when either is not true.

```json
{"status": "ok", "database": "ok", "scheduler": true}
```

The two failure bodies differ, which tells you immediately which half is broken. A database that
does not answer reports the driver error and says nothing about the scheduler, because that check
never ran:

```json
{"status": "unavailable", "database": "dial tcp: connection refused"}
```

A database that is fine and a scheduler that is not:

```json
{"status": "unavailable", "database": "ok", "scheduler": false}
```

Use it for load balancer membership and for deployment gates, not for restarts. A `503` here with a
healthy `/healthz` means the process is running but is not doing its job: the disk holding the
database may be full or read only, or the scheduler goroutine stopped. Look at the server log, then
at [troubleshooting.md](troubleshooting.md).

## Metrics

`GET /metrics` returns the Prometheus text format. It lives at the root only. There is no
`/api/v1/metrics`, and the endpoint is outside the API middleware, so it takes no session cookie and
no API token.

It is open by default. When `metrics_token` (or `BACKVAULT_METRICS_TOKEN`) is set, the request must
carry that exact value, either as a query parameter or as a bearer credential:

```bash
curl -fsS -H "Authorization: Bearer $BACKVAULT_METRICS_TOKEN" http://127.0.0.1:8080/metrics
curl -fsS "http://127.0.0.1:8080/metrics?token=$BACKVAULT_METRICS_TOKEN"
```

Anything else answers `401` with the usual error envelope:

```json
{"error": {"code": "unauthenticated", "message": "metrics token required"}}
```

Set the token whenever the port is reachable by anything other than a local scraper, see
[configuration.md](configuration.md). The query parameter form exists for scrapers that cannot send
a header; it ends up in proxy logs, so prefer the header.

### What is exported

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `backvault_build_info` | gauge | `version`, `commit`, `go_version` | always 1, carries the build identity |
| `backvault_runs_total` | counter | `job`, `kind`, `status` | runs that reached a terminal status |
| `backvault_run_duration_seconds` | histogram | `job`, `kind` | wall clock duration of finished runs |
| `backvault_run_bytes_total` | counter | `job`, `destination` | bytes uploaded, after compression and encryption |
| `backvault_runs_running` | gauge | none | runs currently executing |
| `backvault_runs_queued` | gauge | none | runs waiting for a worker slot |
| `backvault_jobs` | gauge | `enabled` | number of jobs, split by enabled true or false |
| `backvault_jobs_overdue` | gauge | none | jobs the overdue watcher currently considers late |
| `backvault_job_last_success_timestamp_seconds` | gauge | `job` | unix time of the last successful run of each job |
| `backvault_artifacts` | gauge | `destination`, `status` | artifacts known to Backvault per destination |
| `backvault_artifact_bytes` | gauge | `destination` | stored bytes per destination, artifacts in status `present` |
| `backvault_destination_errors_total` | counter | `destination` | upload and verify failures per destination |
| `backvault_notification_failures_total` | counter | `channel` | notifications that could not be delivered |

`kind` is `backup`, `restore`, `prune`, `verify` or `ingest`. `status` on `backvault_runs_total` is
`success`, `warning`, `failed` or `canceled`. `job` is the job slug, `destination` is the
destination name.

That is the whole output. Backvault serves its metrics from a registry of its own rather than the
default one, so none of the standard Go collectors are exported: there are no `go_` and no
`process_` series, and no `promhttp_` series either. For memory growth and file descriptor leaks,
watch the process from outside, with node exporter, cAdvisor or the container runtime.

Some of the series are pushed by the engine as runs finish, and the rest are collected at scrape
time with a five second budget. If the job query behind the gauges fails, that scrape simply omits
them rather than reporting zeros, so a gap in the gauges is a database problem, not an empty
installation.

### Cardinality

Labels carry job slugs and destination names, so the series count is roughly
`jobs x kinds x statuses`. That is fine for the hundreds of jobs a single instance handles. Avoid
generating jobs programmatically with unique slugs per run, which would grow the series count
without bound.

## Scrape configuration

```yaml
scrape_configs:
  - job_name: backvault
    scrape_interval: 60s
    scrape_timeout: 10s
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/backvault-metrics-token
    static_configs:
      - targets: ["backvault.internal:8080"]
        labels:
          instance: backvault-prod
```

A 60 second interval is enough. Backups are measured in minutes and hours, and a shorter interval
only adds load.

Add a blackbox style check on the probe if you want an external view:

```yaml
  - job_name: backvault-health
    metrics_path: /healthz
    static_configs:
      - targets: ["backvault.internal:8080"]
```

## Alerting rules

These are the four alerts worth having. The first two catch the failure mode that matters, backups
that quietly stop happening.

```yaml
groups:
  - name: backvault
    rules:
      - alert: BackvaultJobOverdue
        expr: backvault_jobs_overdue > 0
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "{{ $value }} Backvault job(s) are overdue"
          description: "Open the Backvault dashboard, the Problem jobs panel lists them."

      - alert: BackvaultBackupTooOld
        expr: time() - backvault_job_last_success_timestamp_seconds > 129600
        for: 30m
        labels:
          severity: critical
        annotations:
          summary: "No successful backup of {{ $labels.job }} for 36 hours"

      - alert: BackvaultRunFailures
        expr: increase(backvault_runs_total{status="failed"}[6h]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Backvault job {{ $labels.job }} failed in the last 6 hours"

      - alert: BackvaultNoRuns
        expr: sum(increase(backvault_runs_total[24h])) == 0
        for: 1h
        labels:
          severity: critical
        annotations:
          summary: "Backvault executed no runs in 24 hours"

      - alert: BackvaultDown
        expr: up{job="backvault"} == 0
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Backvault is not answering scrapes"
```

`BackvaultBackupTooOld` is the one to tune per environment: 129600 seconds is 36 hours, which suits a
nightly job and tolerates one missed night. For an hourly job use something closer to three hours.

`BackvaultNoRuns` catches the case the other alerts miss. If the scheduler dies, no run ever fails, so
a failure based alert stays silent forever. An absence based alert does not.

Useful expressions for a dashboard:

```promql
sum by (status) (increase(backvault_runs_total{kind="backup"}[24h]))

sum by (destination) (backvault_artifact_bytes)

histogram_quantile(0.95, sum by (le, job) (rate(backvault_run_duration_seconds_bucket[7d])))

(time() - backvault_job_last_success_timestamp_seconds) / 3600
```

The last one reads as hours since the last success per job, which is the single most useful number
to put on a wall.

## Overdue detection

Metrics tell you a run failed. Overdue detection tells you a run never happened, which is the
quieter and more dangerous case.

Every `overdueCheckMinutes` (default 15, a runtime setting) the watcher looks at two things:

- **Scheduled jobs.** A job is overdue when `nextRunAt` plus a grace period has passed with no run.
  The grace period is the larger of 30 minutes and twice the job's typical duration, so a job that
  normally takes an hour is not flagged for being twenty minutes late.
- **Push jobs.** A push job has no schedule, so it is judged by `expectedIntervalMinutes`: when the
  last successful run is older than that, the job is overdue. A host that stops pushing, because
  cron was removed, the token expired or the machine was decommissioned, shows up here and nowhere
  else. Set `expectedIntervalMinutes` on every push job, see
  [push-and-ingest.md](push-and-ingest.md).

When a job becomes overdue, `job.overdue` is set, a `job.overdue` event is emitted once per incident
(not once per check), and `backvault_jobs_overdue` goes up. The flag clears on the next successful run.

A missed schedule while the server was down is handled separately: at startup Backvault runs a job once
immediately if it is enabled and the missed slot is less than 24 hours old, and logs that it did so.

## What to watch in the logs

With `log_format: json`, these fields carry the signal: `run_id`, `job_slug`, `stage`, `destination`
and `error`. Three patterns are worth a saved query:

- `stage=upload` with `error` set, repeated for the same `destination`: credentials or connectivity
  to that destination, not a source problem.
- `stage=dump` with a non zero exit code from an external tool: the tool, its version or its
  permissions on the Backvault host. `backvault check` lists what is installed.
- `stage=verify` with a size mismatch: the destination accepted an upload it did not store intact.
  The artifact is marked `missing` and the run ends as `warning`, which is easy to miss if you only
  alert on `failed`. `BackvaultRunFailures` above should be widened to `status=~"failed|warning"` when
  you care about this.

Run logs themselves are kept in the database for `runHistoryDays` and are available at
`GET /runs/{id}/log`, which answers `text/plain` with the full length in `X-Backvault-Log-Size` and
accepts an `offset` in bytes so a tail can resume where it stopped. See [api.md](api.md).

## The live event stream

Two server-sent event streams carry the same signal as the metrics, without the scrape delay. Both
need the `read` scope and take no query parameters, both send a keepalive comment line every 20
seconds, and neither emits a `retry:` field, so the client decides its own reconnect policy.

`GET /api/v1/events/stream` opens with the comment line `: connected` and then emits three event
names:

```text
event: run.updated
data: {"id":"01JQ2K9F7A0000000000000100","jobSlug":"db-prod","status":"running", ...}

event: job.updated
data: {"id":"01JJOB000000000000000000","slug":"db-prod","enabled":true, ...}

event: artifact.updated
data: {"id":"01JART00000000000000000","status":"present","size":184320, ...}
```

The `data` line is the object itself, with no envelope around it, so parse it as a run, a job or an
artifact according to the event name. Run log lines are deliberately not on this stream. The
internal bus buffers 256 events and drops rather than queues for a consumer that cannot keep up,
which makes this a live view and not an audit trail: for a record of what happened, read
`GET /runs` and the audit log.

`GET /api/v1/runs/{id}/log/stream` follows one run. It has no `: connected` preamble, polls the log
every 400 ms, and emits `line` events whose data is one raw log line, not JSON and not quoted, then
a single `done` event whose data is the bare terminal status, one of `success`, `warning`, `failed`
or `canceled`. An unknown run id answers a normal JSON `404` before any stream headers are sent.

## Verifying the thing that matters

No metric proves a backup is restorable. Schedule a real restore, at least quarterly: pick an
artifact, restore it into a scratch database or directory, and check the row counts or the file
tree. [restore.md](restore.md) walks through it. Enabling `verifyAfterUpload` on the job is the
cheap continuous check, it confirms the object exists on the destination with the expected size
after every run.
