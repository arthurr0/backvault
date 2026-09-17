# Concepts

Backvault models backups as five objects that reference each other: sources, destinations, jobs, runs and artifacts, with notification channels attached to jobs and API tokens attached to automation.

## The object model

| Object | What it is | Identified by |
|---|---|---|
| Source | What to back up, for example a PostgreSQL database or a directory | id, name |
| Destination | Where artifacts are stored, for example an S3 bucket or an SFTP host | id, name |
| Job | A source, one or more destinations, a schedule, and the packing, retention and notification settings | id, unique `slug` |
| Run | One execution of a job | id |
| Artifact | One stored object on one destination, produced by a run | id |
| Notification channel | An email address, a webhook, a chat target | id, name |
| User | A person who signs in, role `admin` or `viewer` | id, email |
| API token | A credential for scripts and CI, with scopes | id, prefix |

A job has exactly one source and one or more destinations. A backup run with two destinations
produces two artifacts that share the same run, the same filename and the same sha256, because
the same packed file is uploaded to both.

### Source

A source has a `kind` that selects a driver (`postgres`, `mysql`, `mongodb`, `redis`, `sqlite`,
`files`, `ssh`, `docker`, `command`, `push`) and a `config` map that the driver validates against
its own field schema. Fields marked as secrets are encrypted at rest with the master key and are
replaced with `********` in every API response, every export and every log line. See
[security.md](security.md) for how that works, and the pages under `sources/` for each driver, for
example [sources/postgres.md](sources/postgres.md).

One source can be used by many jobs. Deleting a source that a job still references is refused with
HTTP 409.

### Destination

A destination has a `kind` (`local`, `s3`, `sftp`, `webdav`) and a driver specific config. The same
destination is normally shared by many jobs, and each job writes into its own `<jobSlug>/` prefix.
See [destinations/s3.md](destinations/s3.md) and
[destinations/hetzner-storage-box.md](destinations/hetzner-storage-box.md).

### Job

A job ties everything together:

- `slug`, unique and URL safe, derived from the name when the job is created and editable later.
  The slug appears in the artifact path, in the ingest URL and in the API, so changing it later
  changes where new artifacts are written. Old artifacts keep their recorded path.
- `schedule`, a five field cron expression, or an empty string for manual only. `@daily` style
  descriptors are accepted.
- `timezone`, the timezone the schedule is evaluated in. Artifact timestamps stay in UTC.
- `compression` (`none`, `gzip`, `zstd`) and `compressionLevel` (0 means the driver default).
- `encryption` (`none` or `age`) and `encryptionPassphrase`, required when encryption is `age`.
- `retention`, see [retention.md](retention.md).
- `notificationChannelIds` and `notifyOn`, see [notifications.md](notifications.md).
- `timeoutMinutes`, `retries`, `retryDelaySeconds`, `preCommand`, `postCommand`,
  `verifyAfterUpload`.
- `expectedIntervalMinutes`, used for overdue detection on push jobs and on any job without
  a schedule.

A job whose source kind is `push` is never scheduled. It receives artifacts through the ingest API
instead. See [push-and-ingest.md](push-and-ingest.md).

### Run

A run is one execution. Its `kind` is `backup`, `restore`, `prune`, `verify` or `ingest`, and its
`trigger` records why it started: `schedule`, `manual`, `api`, `ingest` or `retry`. A run carries
stages, a line oriented log, byte counts and the sha256 of what was uploaded.

### Artifact

An artifact is one file on one destination. It records the path, the filename, the size, the
sha256 of the stored bytes, the compression and encryption that were applied, the source kind and
the extension, so that a restore knows how to unpack it even years later.

Not every source can take an artifact back. `redis` and `push` have no restore capability at all,
the `docker` driver restores volumes but refuses `exec` sources, and `command` and `ssh` restore
only when their restore command is set. See [restore.md](restore.md).

## The backup pipeline

Every backup run walks through the same stages, in this order. Each stage is recorded in
`run.stages` with a status (`pending`, `running`, `success`, `failed`, `skipped`), a start time, a
finish time and an optional message.

| Stage | What happens | Skipped when |
|---|---|---|
| `prepare` | The job, its source and its destinations are loaded and validated, and the pack options are resolved | never |
| `pre-command` | The job's `preCommand` runs on the Backvault host | `preCommand` is empty |
| `dump` | The source driver produces a stream, which is spooled to disk | an ingest run has a `receive` stage here instead, the data arrives from the client |
| `pack` | Compression, then encryption | never, it can be a pass through |
| `upload:<destination>` | The packed file is uploaded, one stage per destination | never |
| `verify` | Each destination is asked for the stored object and its size is compared with the size Backvault uploaded | `verifyAfterUpload` is off, the stage is then recorded as `skipped` |
| `retention` | The retention policy is applied per destination | never, it reports `no retention configured` when the effective policy is empty |
| `post-command` | The job's `postCommand` runs on the Backvault host | `postCommand` is empty |
| `notify` | Matching notification channels are queued | never, the dispatch itself is asynchronous |

The upload stage name includes the destination name, so a job with two destinations shows two
upload stages, for example `upload:Hetzner box` and `upload:MinIO`.

### dump and pack

The dump stage asks the source driver for a stream. The engine copies that stream through the pack
chain into a spool file under `workDir/<runID>/<filename>`, computing three numbers as it goes: the
raw byte count, the packed byte count and the sha256 of the packed file. The sha256 is the checksum
of exactly what gets uploaded, not of the raw dump, which is what makes it verifiable against the
destination later.

A dump is only considered successful when the stream closes cleanly. A driver that shells out to
`pg_dump` waits for the process and reports a non zero exit as an error, even when the copy itself
read a complete looking stream. A truncated dump is a failed run, not a small backup.

### Filenames

```text
<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]
```

Timestamps are UTC, always, regardless of the job timezone. The extension comes from the source
driver: `dump` for a PostgreSQL custom format dump, `sql` for plain SQL, `tar` for a file archive,
`archive` for `mongodump`, `rdb` for Redis, `sqlite` for SQLite. The compression suffix is added by
the pack stage, then the encryption suffix. A daily PostgreSQL job with slug `shop-db`, zstd
compression and age encryption produces:

```text
shop-db/shop-db-20260917-020000.dump.zst.age
```

Those suffixes are not decoration. A restore reads them to decide what to undo, and an ingest with
`?packed=1` reads them to record what the client already did.

### upload, verify and retention

Uploads are sequential over the destinations. Each upload is attempted `job.retries` + 1 times,
with a delay starting at `retryDelaySeconds`, doubling per attempt and capped at ten minutes. Both
fields default to 0, which means a single attempt per destination, and a `retryDelaySeconds` of 0
falls back to five seconds.

If some destinations succeed and others fail, the run ends as `warning` with artifacts recorded for
the destinations that worked. If every destination fails, the run is `failed` and no artifact is
recorded.

Verify asks each destination to stat the object it has written and compares the size with what
Backvault uploaded. Destinations are not asked for a checksum, because none of the four exposes one
uniformly. A missing object or a size mismatch marks that artifact `missing` and the run
`warning`, and a matching size records `verifiedAt` on the artifact.

Retention then applies the job's policy per destination. See [retention.md](retention.md).

### Spool space

The packed artifact is written to disk before it is uploaded, so the work directory needs room for
one full compressed backup per concurrent run. The spool directory is always removed at the end of
a run, on success and on failure. Set `BACKVAULT_WORK_DIR` to move it onto a larger filesystem, see
[configuration.md](configuration.md).

## Run status

| Status | Meaning |
|---|---|
| `queued` | Accepted and waiting for a worker slot |
| `running` | A stage is in progress |
| `success` | Every stage finished, every destination has the artifact |
| `warning` | The backup exists but something is wrong: at least one destination failed, or verify found a size or checksum mismatch |
| `failed` | The backup does not exist: the dump failed, packing failed, or every destination failed |
| `canceled` | The run context was canceled from the API or the panel |

Treat `warning` as an incident. A warning run means you have fewer copies than you asked for.

## Artifact status

| Status | Meaning |
|---|---|
| `present` | Uploaded and, when verify is on, confirmed |
| `missing` | The destination no longer has it, or verify found a mismatch |
| `pruned` | Deleted by the retention policy |
| `deleted` | Deleted on purpose through the API or the panel |

Only artifacts with status `present` are counted by retention and by the storage totals on the
dashboard.

## Concurrency, locking and timeouts

- One run per job at a time. A second run request for a job that is already running is refused with
  HTTP 423, and the panel shows the job as running.
- `settings.maxConcurrentRuns`, default 2, limits how many runs execute at once across all jobs.
  Extra runs stay `queued` until a slot frees up.
- `job.timeoutMinutes` cancels the run context when it expires. A value of 0 means six hours.
  Drivers honour the context, so a canceled run stops its `pg_dump` child rather than orphaning it.
- Cancelling a run from the API cancels the same context, with the same effect.

## Scheduling

The scheduler evaluates each job's cron expression in the job's timezone and records
`job.nextRunAt`. Two behaviours are worth knowing:

- **Missed runs at startup.** If Backvault was down across a scheduled time, the missed slot is logged
  and the job is run once immediately, provided the job is enabled and the missed slot is less than
  24 hours old. Older misses are logged and skipped, so a server that was off for a week does not
  start seven backups at boot.
- **Overdue detection.** Every `settings.overdueCheckMinutes` (default 15) the watcher marks a
  scheduled job overdue when `nextRunAt` plus a grace period of `max(30 minutes, 2x the expected
  duration of the last run)` has passed without a run. Push jobs, and jobs with no schedule, are
  overdue when the last successful run is older than `expectedIntervalMinutes`. The `job.overdue`
  event is emitted once per incident, not on every check.

## Users and tokens

Two roles: `admin` can change everything, `viewer` can read. API tokens carry scopes instead of a
role:

| Scope | Allows |
|---|---|
| `read` | Reading jobs, runs, artifacts, logs and the dashboard |
| `run` | Queueing runs, cancelling runs, queueing a verify, plus everything `read` allows |
| `ingest` | Pushing artifacts to a push job |
| `admin` | Everything, including configuration and secrets |

`admin` satisfies every scope check and `run` also satisfies `read`. A token can be restricted to
a list of job slugs, which is what you want for an ingest token that lives in an environment file
on a database host. That restriction is checked on ingest and on running a job, not on the other
endpoints, so keep the scopes narrow as well. See [security.md](security.md) and
[push-and-ingest.md](push-and-ingest.md).

## Live updates

Run state changes publish `run.updated` on an in process event bus, job changes publish
`job.updated`, artifact changes publish `artifact.updated`. The panel subscribes over server sent
events at `/api/v1/events/stream`, with a heartbeat every 20 seconds, and run logs stream line by
line from `/api/v1/runs/{id}/log/stream`. See [api.md](api.md).

## Next

- Run your first backup: [first-backup.md](first-backup.md)
- Decide how long to keep things: [retention.md](retention.md)
- Get the data back: [restore.md](restore.md)
