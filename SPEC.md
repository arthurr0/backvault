# Backvault — product and engineering specification

Backvault is a self-hosted backup manager: one binary, one admin panel, every backup accounted for.
It backs up databases, servers and plain files, ships them to S3-compatible storage, SFTP hosts
(Hetzner Storage Box) or local disks, keeps retention under control, verifies, restores and notifies.

This file is the single source of truth for everyone working on the project. Read it fully before
touching code. If you need to change a contract described here, describe the change in your final
report instead of silently diverging.

## 1. Branding

- Name: **Backvault**. A vault for your backups. Binary and CLI: `backvault`. Module: `backvault`.
- Tagline: **"Every backup, accounted for."** Secondary: "Backups you can actually restore."
- Voice: calm, precise, operator-friendly. No exclamation marks in UI copy. English everywhere.
- Palette (CSS tokens, use exactly these names):
  - `--ink`  `#0F1720` (deep navy-black, primary dark surface / dark theme background)
  - `--ink-2` `#182230` (dark elevated surface)
  - `--brass` `#D4A032` (accent: primary buttons, the logo handle and hinges, active nav, focus rings)
  - `--brass-2` `#F2C14E` (accent hover / highlights on dark)
  - `--parchment` `#F6F3EC` (light theme page background)
  - `--paper` `#FFFFFF` (light theme cards)
  - `--slate-1` `#334155`, `--slate-2` `#64748B`, `--slate-3` `#94A3B8`, `--line` `#E5E1D8` (light borders), `--line-dark` `#263241`
  - Status: success `#2E9E6B`, warning `#E0A100`, danger `#D9483B`, info `#3B82F6`, running `#7C3AED`
- Typography: Inter (UI), JetBrains Mono (logs, paths, hashes). System fallbacks always present.
- Logo: a geometric vault door (rounded-square body in `--ink`, a circular door inset, a spoked `--brass`
  handle at the centre, a small brass hinge). Wordmark "Backvault" in Inter SemiBold, letter-spacing -0.01em.
  The canonical SVGs live in brand/.
  A mark-only variant must read at 16px (favicon).
- Theme: light and dark, follows system by default, toggle persisted in localStorage.

## 2. Architecture

Single Go binary (`cmd/backvault`) that serves the HTTP API and the embedded React admin panel,
runs the scheduler and executes backup runs. SQLite (WAL) stores all state. No external services
required. Docker image and systemd unit are provided.

```
backvault/
  cmd/backvault/             cobra CLI: serve, run, jobs, runs, restore, push, user, token, export, import, check, version
  internal/
    core/                 domain types + Config helpers (DONE, do not change without reporting)
    source/               Source driver interface + registry (DONE)
    dest/                 Destination driver interface + registry (DONE)
    notify/               Notifier interface + registry (DONE)
    drivers/              all.go: Register() blank-imports/registers every driver (owned by drivers agent)
    source/<kind>/        one package per source driver (drivers agent)
    dest/<kind>/          one package per destination driver (drivers agent)
    notify/<kind>/        one package per notifier (drivers agent)
    config/               server config: yaml file + env (backend agent)
    store/                SQLite storage, migrations, repositories (backend agent)
    secrets/              master key + AES-256-GCM field encryption (backend agent)
    auth/                 argon2id passwords, sessions, API tokens, RBAC (backend agent)
    engine/               run queue, pipeline, scheduler, retention, restore, verify, overdue watcher, events bus (backend agent)
    server/               chi router, handlers, middleware, SSE, static SPA (backend agent)
    version/              version vars set via ldflags (backend agent)
  web/                    Vite + React + TypeScript admin panel (frontend agent), built into web/dist and embedded
  scripts/                standalone bash tooling for hosts without Backvault installed (ops agent)
  deploy/                 Dockerfile, docker-compose.yml, systemd unit, install script (ops agent)
  docs/                   user documentation (ops agent)
  brand/                  logo SVGs, palette, wordmark (ops agent)
  SPEC.md, README.md, Makefile, LICENSE
```

### Coding rules (hard)

- Go 1.27, module path `github.com/arthurr0/backvault`. Standard library first; approved deps: `github.com/go-chi/chi/v5`,
  `modernc.org/sqlite`, `github.com/robfig/cron/v3`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`,
  `golang.org/x/crypto`, `github.com/klauspost/compress` (zstd), `filippo.io/age`,
  `github.com/aws/aws-sdk-go-v2/...` (S3), `github.com/pkg/sftp`, `github.com/oklog/ulid/v2`,
  `github.com/emersion/go-webdav` (optional), `github.com/prometheus/client_golang` (metrics).
  Anything else: justify in your report.
- **No comments in code. None.** No `//` or `/* */` in Go, no `#` comments in shell (shebang line is fine),
  no `//` or `{/* */}` in TS/TSX, no comments in YAML/Dockerfile except where syntax requires (it never does).
  Put explanations in docs/ or in the README, never in source. Doc comments on exported identifiers are
  also forbidden. `go vet` and `golangci-lint` defaults must not require them.
- Use `log/slog` for logging. Wrap errors with `%w` and context. No `panic` outside `init`-time registration.
- Tests: `go test ./...` must pass with no external services. Integration tests that need Docker
  must be skipped unless `BACKVAULT_TEST_DOCKER=1`.
- Every HTTP handler validates input and returns the error envelope described in §6.
- Secrets never appear in logs, API responses, exports or error messages.
- Frontend: TypeScript strict, no `any` unless unavoidable. English UI copy. Accessible (labels, focus, keyboard).
- Commit messages, if you commit: plain imperative English. Never add authorship trailers or mention
  AI tooling anywhere in the repository.

## 3. Domain model (see `internal/core/types.go`, JSON tags are the API shape)

- **Source**: what to back up. `kind` selects a source driver, `config` is driver-specific and validated
  against the driver's `DriverSpec.Fields`. Secret fields are encrypted at rest and masked in API output.
- **Destination**: where artifacts go. `kind` selects a destination driver.
- **Job**: source + 1..n destinations + schedule (cron, 5-field, optional `@daily` style) + compression +
  encryption + retention + notifications. `slug` is unique, URL-safe, derived from name on create, editable.
  `schedule == ""` means manual only. Push jobs (source kind `push`) are never scheduled; they receive
  artifacts through the ingest API and use `expectedIntervalMinutes` for overdue detection.
- **Run**: one execution (backup, restore, prune, verify, ingest). Has stages, a log, bytes, sha256.
- **Artifact**: one stored object on one destination, produced by a run. A backup run with 2 destinations
  creates 2 artifacts sharing the same run, sha256 and filename.
- **NotificationChannel**: kind + config + events filter. Jobs pick channels; `notifyOn` on the job filters
  events further (empty means "use settings.defaultNotifyOn").
- **User** (admin | viewer), **APIToken** (scopes: admin, read, run, ingest; optional `jobSlugs` restriction),
  **AuditEntry**, **Settings** (single row).

Secret handling convention: driver spec fields with `Secret: true` or type `secret`, `Job.EncryptionPassphrase`,
channel secret fields. Stored encrypted (AES-256-GCM, master key). API responses replace them with
`core.SecretMask` (`********`). If a create/update request carries exactly `********` for a secret field, the
previously stored value is kept (`Config.MergeSecrets`). Export masks secrets unless `?includeSecrets=1`
by an admin, which is audited.

## 4. Backup pipeline (engine)

1. Acquire job lock (one run per job at a time) and a worker slot (`settings.maxConcurrentRuns`, default 2).
2. Stages, in order, each recorded in `run.stages` with timestamps and messages:
   `prepare` → `pre-command` (skipped if empty) → `dump` → `pack` (compress+encrypt, always present, may be
   pass-through) → `upload:<destination-slug-or-name>` (one per destination) → `verify` (if enabled) →
   `retention` → `post-command` (skipped if empty) → `notify`.
3. `dump`: `source.Driver.Backup` returns a `Stream`. The engine copies the stream through the pack chain into
   a **spool file** in `workDir/<runID>/<filename>` computing raw bytes, packed bytes and sha256 (of the packed
   file, i.e. exactly what is uploaded). Stream close semantics: `Reader.Close()` must wait for any underlying
   process and return a non-nil error if the dump did not complete successfully; the engine treats a
   non-nil Close error as a failed dump even when the copy succeeded.
4. `pack`: compression `none|gzip|zstd` (`compressionLevel` 0 means driver default), then encryption
   `none|age` (age scrypt passphrase recipient; `EncryptionPassphrase` required when `age`).
   Filename: `<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]`, timestamps in UTC.
   `ext` comes from `Stream.Extension` (e.g. `dump`, `sql`, `tar`, `archive`, `rdb`, `sqlite`).
5. `upload`: sequential over destinations, each with retries (`job.retries`, backoff `retryDelaySeconds`,
   exponential x2 capped at 10 minutes). Partial success (some destinations failed) yields status `warning`
   with artifacts recorded for successful destinations. All destinations failed yields `failed`.
6. `verify`: `Stat` on each destination, compare size; for destinations that expose checksums also compare.
   Mismatch marks the artifact `missing` and the run `warning`.
7. `retention`: apply `job.retention` (or `settings.defaultRetention` when the job's retention is zero) per
   destination, over artifacts with status `present` of this job. Deleted artifacts become `pruned`. Retention
   algorithm (restic-style): keep the newest `keepLast`; then for each of hourly/daily/weekly/monthly/yearly keep
   the newest artifact in each of the most recent N distinct buckets; `maxAgeDays` removes anything older
   unless it is protected by one of the keep rules. An artifact kept by any rule is kept. Never delete the
   newest successful artifact. Retention is also runnable manually (`POST /jobs/{id}/prune`) as a prune run.
8. Timeout: `job.timeoutMinutes` (0 = 6 hours) cancels the run context. Cancel from API also cancels the
   context; drivers must honour ctx.
9. Spool directory is always removed at the end, on success or failure.
10. The run log is line-oriented text stored in the DB (`run_logs` table, appended in chunks) and streamed live
    over SSE. The engine gives drivers an `*slog.Logger` whose handler writes into the run log; use
    `log.Info/Warn/Error` with short messages and key/value attrs.
11. Ingest: `POST /api/v1/ingest/{jobSlug}` streams the request body into the spool as an already-dumped
    artifact (no dump stage). Headers: `X-Backvault-Filename` (optional original name, used to derive ext),
    `X-Backvault-Sha256` (optional, verified after spooling, mismatch fails the run), `Content-Length`.
    Query `?packed=1` means the client already compressed/encrypted; the engine then skips pack and records
    compression/encryption from the filename suffixes. Otherwise the job's pack settings apply.
12. Restore modes (`POST /artifacts/{id}/restore`):
    - `download`: handled by `GET /artifacts/{id}/download` which streams from the destination, decrypting
      (passphrase from job, or `?passphrase=` override) and decompressing unless `?raw=1`.
    - `path`: restore run that writes the unpacked artifact to `targetPath` on the Backvault host (a file, or for
      tar extension extracts into the directory when `extract=true`).
    - `source`: restore run that pipes the unpacked artifact into `source.Restorer.Restore` of the job's source
      (or `targetSourceId` override, must be the same kind). Available when the driver has `CapRestore`.
13. Overdue watcher: every `settings.overdueCheckMinutes` (default 15) check scheduled jobs whose
    `nextRunAt + max(30m, 2×expected duration)` passed without a run, and push jobs whose last successful
    run is older than `expectedIntervalMinutes`. Mark `job.overdue`, emit `job.overdue` once per incident.
14. Missed runs at startup: if the scheduler was down across a scheduled time, log it and run the job once
    immediately if `job.enabled` and the missed slot is less than 24h old.
15. Events bus: in-process pub/sub. Every run state change publishes `run.updated` with the run JSON; job
    changes publish `job.updated`; used by SSE `/events/stream` and by the notifier dispatcher.

## 5. Drivers

Every driver publishes a `core.DriverSpec` with a form schema. The frontend renders driver forms from
`GET /meta/{sources|destinations|notifiers}` only, it must not hardcode per-driver forms. Field names are
`snake_case`. Use `Group` ("Connection", "Options", "Advanced") and `Advanced: true` to keep forms tidy.
Use `ShowIf` for conditional fields (e.g. `{"auth": "password"}`). `Tools` lists external binaries the driver
shells out to (`pg_dump`), reported by `GET /meta/tools`.

### Sources (kind → extension → notes)

| kind | label | ext | how | restore |
|---|---|---|---|---|
| `postgres` | PostgreSQL | `dump` (custom format) or `sql` (plain) | `pg_dump` with `PGPASSWORD`, options: host, port, database, user, password, sslmode, format (custom/plain), schema-only, exclude tables, extra args; `all_databases` uses `pg_dumpall` → `sql` | `pg_restore` (custom) / `psql` (plain), options clean/create |
| `mysql` | MySQL / MariaDB | `sql` | `mysqldump --single-transaction --routines --triggers --events` (or `mariadb-dump`), all-databases flag, defaults-extra-file for password | `mysql` |
| `mongodb` | MongoDB | `archive` | `mongodump --archive` via uri, optional db/collection | `mongorestore --archive` |
| `redis` | Redis | `rdb` | `redis-cli --rdb -` (host/port/password/tls) | none |
| `sqlite` | SQLite | `sqlite` | `sqlite3 <db> ".backup <tmp>"` if sqlite3 available, otherwise online backup via modernc driver `VACUUM INTO` | copy file to path |
| `files` | Files & directories | `tar` | Go-native tar of `paths` (list), `exclude` globs, follow symlinks option, one-file-system option, base dir | extract to path |
| `ssh` | Remote command over SSH | configurable (`tar` default) | connects with key/password, runs `command` on remote host, streams stdout; presets in UI copy: remote `pg_dump`, remote `tar`. Requires exit code 0 | optional: run `restore_command` with stdin |
| `docker` | Docker volume / container | `tar` | volume: `docker run --rm -v vol:/data alpine tar -C /data -cf - .`; container exec mode: `docker exec <c> <cmd>` stdout | volume: tar extract into volume |
| `command` | Custom command | configurable | runs a local shell command, captures stdout, stderr goes to run log, exit code must be 0 | optional `restore_command` |
| `push` | Push (remote script) | derived from ingest filename | no Backup (returns error "push sources receive data through the ingest API"); `Test` succeeds | none |

Prefer Go-native implementations where practical (files, sqlite, ssh) and shelling out where the ecosystem
tool is the standard (pg_dump, mysqldump, mongodump, redis-cli, docker). Look up binaries via `exec.LookPath`
with an optional `binary_path` advanced field. Stderr of child processes goes to the run log.

### Destinations

| kind | label | notes |
|---|---|---|
| `local` | Local directory | `path`; atomic writes (tmp + rename); create dirs; `List` recursive; `Stat` |
| `s3` | S3-compatible | endpoint (blank = AWS), region, bucket, prefix, access key, secret key, path-style flag, storage class, server-side encryption flag; multipart upload with 16MiB parts; works with AWS, MinIO, Backblaze B2, Wasabi, Cloudflare R2, Hetzner Object Storage; presets as Help text |
| `sftp` | SFTP (Hetzner Storage Box etc.) | host, port (23 default for Hetzner, 22 otherwise), user, auth (password / private key), private key (secret, PEM), key passphrase, host key fingerprint (optional, verified when set; otherwise TOFU with a warning in log), base path; `mkdir -p`; write to `.partial` then rename |
| `webdav` | WebDAV | url, user, password, base path (Nextcloud, Hetzner Storage Box WebDAV); optional, do last |

Destination `Put` receives `size` (known, spooled). Destinations must create parent directories, must
overwrite safely, and `Delete` of a missing object returns nil.

### Notifiers

| kind | notes |
|---|---|
| `email` | SMTP host/port/user/password/TLS mode (starttls/tls/none), from, to (list); HTML + text body |
| `webhook` | url, method, optional headers (list `Key: Value`), secret → `X-Backvault-Signature` HMAC-SHA256 of body; JSON body is the `notify.Event` |
| `slack` | incoming webhook url; blocks with status colour |
| `discord` | webhook url; embed with colour |
| `telegram` | bot token, chat id; Markdown-safe message |
| `ntfy` | server url (default https://ntfy.sh), topic, optional token; priority by severity |

Messages include: site name, job name, status, duration, size, destinations, error (if any), link to the run.

## 6. HTTP API

Base `/api/v1`. JSON. Cookies for the browser (`backvault_session`, HttpOnly, SameSite=Lax, Secure when base
URL is https), `Authorization: Bearer <token>` for API tokens. CSRF: state-changing cookie-authenticated
requests must carry header `X-Requested-With: backvault` (frontend always sends it).

Error envelope: `{"error": {"code": "validation_failed", "message": "...", "fields": {"name": "required"}}}`
with proper HTTP status (400 validation, 401 unauthenticated, 403 forbidden, 404 not_found, 409 conflict,
423 locked (job already running), 500 internal). Lists return `{"items": [...], "total": n}`. Pagination via
`?limit=&offset=`; default limit 50, max 500. Filtering via query params named after fields.

| Method + path | Auth | Description |
|---|---|---|
| GET `/healthz`, GET `/readyz` | none | liveness / readiness (DB reachable, scheduler running) |
| GET `/metrics` | none (or token when `metricsToken` set) | Prometheus: runs total by status, run duration, bytes uploaded, artifacts by destination, jobs overdue, last success timestamp per job |
| GET `/setup/status` | none | `{needsSetup: bool}` |
| POST `/setup` | none, only while no users | `{name,email,password}` creates the admin and logs in |
| POST `/auth/login` | none | `{email,password}` → `{user}`, sets cookie; rate limited (5/min/IP) |
| POST `/auth/logout` | session | clears cookie |
| GET `/auth/me` | any | `{user, authType: "session"|"token", scopes}` |
| GET `/meta/version` | any | `core.VersionInfo` |
| GET `/meta/sources` `/meta/destinations` `/meta/notifiers` | any | `{items: DriverSpec[]}` |
| GET `/meta/tools` | any | `{items: ToolStatus[]}` |
| GET `/meta/timezones` | any | `{items: string[]}` |
| GET `/dashboard` | read | `DashboardStats` |
| GET `/events/stream` | read (session or token) | SSE: `event: run.updated` / `job.updated` / `artifact.updated`, `data: <json>`; heartbeat every 20s |
| GET/POST `/sources`, GET/PUT/DELETE `/sources/{id}` | read / admin | delete refused (409) when jobs reference it |
| POST `/sources/test` | admin | body = `{kind, config}` (unsaved), returns `{ok, message, durationMs}` |
| POST `/sources/{id}/test` | admin | same, stores last test result |
| GET/POST `/destinations`, GET/PUT/DELETE `/destinations/{id}` | read / admin | |
| POST `/destinations/test`, POST `/destinations/{id}/test` | admin | |
| GET `/destinations/{id}/browse?prefix=` | read | `{items: dest.Object[]}` |
| GET/POST `/jobs`, GET/PUT/DELETE `/jobs/{id}` | read / admin | `{id}` may also be a slug. Create validates cron, timezone, encryption passphrase. Delete removes job; artifacts remain listed (job name kept) unless `?deleteArtifacts=1` |
| POST `/jobs/{id}/run` | run | queues a backup run → `{run}` (423 if running) |
| POST `/jobs/{id}/enable`, `/disable` | admin | |
| POST `/jobs/{id}/prune` | admin | queues a prune run |
| GET `/jobs/{id}/runs` `/jobs/{id}/artifacts` | read | filtered lists |
| POST `/jobs/{id}/duplicate` | admin | copies a job with `-copy` slug |
| GET `/runs` | read | filters: `job`, `status`, `kind`, `since`, `until` |
| GET `/runs/{id}` | read | |
| POST `/runs/{id}/cancel` | run | |
| GET `/runs/{id}/log` | read | `text/plain`, whole log; `?offset=` returns from byte offset |
| GET `/runs/{id}/log/stream` | read | SSE `event: line` with `data: <text>`, `event: done` on terminal |
| GET `/artifacts` | read | filters `job`, `destination`, `status`, `since`, `until`, `q` (filename) |
| GET `/artifacts/{id}` | read | |
| DELETE `/artifacts/{id}` | admin | deletes on the destination, marks `deleted` |
| GET `/artifacts/{id}/download` | read | streams file; `?raw=1` skips unpack; `?passphrase=` override |
| POST `/artifacts/{id}/restore` | admin | `{mode: "path"|"source", targetPath, extract, targetSourceId, passphrase, params}` → `{run}` |
| POST `/artifacts/{id}/verify` | run | queues a verify run for that artifact |
| POST `/ingest/{jobSlug}` | token with `ingest` (and slug allowed) | body stream; see §4.11; returns `{run, artifacts}` (201) |
| GET/POST `/notifications/channels`, GET/PUT/DELETE `/notifications/channels/{id}` | read / admin | |
| POST `/notifications/channels/{id}/test`, POST `/notifications/channels/test` | admin | sends a test event |
| GET/PUT `/settings` | read / admin | `core.Settings` |
| GET/POST `/users`, GET/PUT/DELETE `/users/{id}`, PUT `/users/{id}/password` | admin (self allowed for own password with `currentPassword`) | cannot delete last admin |
| GET/POST `/tokens`, DELETE `/tokens/{id}` | admin | POST returns `{token: APIToken, secret: "bvt_..."}` once |
| GET `/audit` | admin | filters `actor`, `action`, `objectType`, `since` |
| GET `/export` | admin | YAML: sources, destinations, jobs, channels (secrets masked unless `?includeSecrets=1`) |
| POST `/import` | admin | YAML body, `?dryRun=1` returns the plan; upsert by slug/name |

Audit every admin mutation: action names like `source.create`, `job.update`, `job.run`, `artifact.delete`,
`token.create`, `settings.update`, `export.secrets`.

Static: everything not under `/api` serves the embedded SPA (`web/dist`), with `index.html` fallback for
client routes and long cache headers for hashed assets. If `web/dist` is absent at build time the server
still builds and serves a minimal placeholder page.

## 7. Admin panel (web/)

Vite + React 19 + TypeScript + Tailwind CSS v4 + TanStack Query + React Router + lucide-react + recharts.
Dev: `npm run dev` proxies `/api` to `http://localhost:8080`. `npm run build` outputs `web/dist`.
`npm run typecheck`, `npm run lint` must pass. Include a mock API server (`web/mock/server.mjs`, Node
only, no deps) implementing enough of §6 with realistic sample data so the UI can be developed and
screenshotted without the Go backend (`npm run mock`).

Layout: left sidebar (logo, nav, theme toggle, user menu), top bar with page title, breadcrumbs and a global
"Run job" quick action + Ctrl/Cmd+K command palette (jump to jobs/sources/destinations/runs, run a job).
Responsive down to 768px (sidebar collapses to a drawer).

Pages and must-have behaviour:
- **Setup** (`/setup`): first-run admin creation. **Login** (`/login`).
- **Dashboard** (`/`): stat tiles (jobs, enabled, overdue, failing, running, 24h success/failed, total
  storage), area/bar chart of daily success/failed and bytes for 30 days, storage per destination, recent
  runs table (live via SSE), upcoming runs, problem jobs with quick actions.
- **Jobs** (`/jobs`): table with status pill, source, destinations, schedule (human-readable cron text plus
  next run relative), last run, size, tags; filters; row actions run/enable/disable/edit/duplicate/delete.
  **Job detail** (`/jobs/:slug`): overview cards, run history, artifacts, config summary, run-now, prune,
  export YAML. **Job editor** (`/jobs/new`, `/jobs/:slug/edit`): wizard-like sections: Basics, Source (pick
  existing or create inline), Destinations (multi-select), Schedule (cron builder with presets + raw cron +
  timezone + next 5 occurrences preview), Pack (compression, level, encryption + passphrase with strength
  hint), Retention (visual explanation of what is kept), Notifications, Advanced (timeout, retries, hooks,
  verify, expected interval for push jobs). Push jobs show a "How to push" panel with ready-to-copy curl and
  `backvault push` commands including the ingest URL.
- **Runs** (`/runs`, `/runs/:id`): filterable list; detail with stage timeline, metrics, artifacts and a
  live log viewer (auto-scroll, pause, search, download, ANSI stripped), cancel button while running.
- **Artifacts** (`/artifacts`): filterable table with size, sha256 (copy), destination, status; actions
  download, restore (modal: mode path/source, options), verify, delete. Browse destination view.
- **Sources** (`/sources`) and **Destinations** (`/destinations`): cards/table, create/edit with driver picker
  (icon grid grouped by category) and dynamic schema forms, "Test connection" with result, used-by jobs list,
  destination usage bar.
- **Notifications** (`/notifications`): channels CRUD with dynamic forms, event matrix, test send.
- **Settings** (`/settings`): General, Users, API tokens (create modal shows secret once with copy),
  Audit log, Tools (availability of pg_dump etc. with hints), About (version, links), Import/Export.
- Global: toasts, confirm dialogs for destructive actions, empty states with guidance, skeleton loaders,
  error boundaries, 401 → redirect to login, relative timestamps with absolute tooltip, byte formatting,
  duration formatting, keyboard shortcuts (`g j` jobs, `g r` runs, `?` help).

Dynamic form renderer: given `core.Field[]` render inputs by type (`string`, `text`, `secret` with reveal toggle,
`int`, `port`, `bool` switch, `select`, `list` as tag input / multiline, `path`), honour `required`, `default`,
`placeholder`, `help`, `group` (collapsible sections, "Advanced" collapsed by default), `showIf`. Secret fields
loaded from the API show `********` and are only sent back if changed.

## 8. CLI (`cmd/backvault`)

```
backvault serve [--config backvault.yaml] [--listen :8080] [--data-dir ./data]
backvault check                          reports available external tools and config problems
backvault run <job-slug> [--wait]        queues a run through the running server API (uses BACKVAULT_URL/BACKVAULT_TOKEN) 
backvault jobs list|show|enable|disable  via API
backvault runs list|log <id>             via API
backvault restore <artifact-id> --to <path> [--raw] [--passphrase]   via API download
backvault push --job <slug> [--file <path> | stdin] [--name <filename>] [--packed] [--sha256]   via ingest API, streams, retries
backvault user create|list|password      direct DB access (server may be stopped), for bootstrap and recovery
backvault token create --name --scopes --jobs   direct DB access
backvault export [--include-secrets] / backvault import <file> [--dry-run]   via API
backvault version
```
Env: `BACKVAULT_URL`, `BACKVAULT_TOKEN`, `BACKVAULT_CONFIG`, `BACKVAULT_DATA_DIR`, `BACKVAULT_LISTEN`, `BACKVAULT_MASTER_KEY`
(hex/base64 32 bytes) or `BACKVAULT_MASTER_KEY_FILE` (default `<dataDir>/master.key`, auto-generated 0600),
`BACKVAULT_BASE_URL`, `BACKVAULT_LOG_LEVEL`, `BACKVAULT_LOG_FORMAT` (text|json), `BACKVAULT_ADMIN_EMAIL` +
`BACKVAULT_ADMIN_PASSWORD` (bootstrap admin when no users exist), `BACKVAULT_WORK_DIR` (default `<dataDir>/work`),
`BACKVAULT_TRUSTED_PROXIES`, `BACKVAULT_METRICS_TOKEN`. Config file keys mirror env in lower snake case.

## 9. Standalone scripts (`scripts/`)

For hosts that only run a cron job and push to Backvault, or that upload straight to storage without Backvault.
Bash, `set -Eeuo pipefail`, no comments, `usage()` text, config via env or `--flags`, exit codes meaningful,
logs to stderr with timestamps, temp files cleaned with `trap`. Each script has a matching page in docs/.

- `backvault-push.sh`: uploads a file (or stdin) to `POST /ingest/<job>` with curl, sha256 header, retries, `--packed`.
- `backup-postgres.sh`, `backup-mysql.sh`, `backup-mongodb.sh`, `backup-files.sh`, `backup-docker-volume.sh`:
  produce a dump, optionally compress (zstd/gzip) and encrypt (age or openssl fallback), then either push to
  Backvault (`BACKVAULT_URL`, `BACKVAULT_TOKEN`, `BACKVAULT_JOB`) or upload directly with `upload-s3.sh` / `upload-sftp.sh`.
- `upload-s3.sh`: uses `aws` CLI if present, else `mc`, else pure curl SigV4 (implement SigV4 in bash+openssl).
- `upload-sftp.sh`: uploads to SFTP (Hetzner Storage Box friendly: port 23, `.partial` + rename) via `sftp`/`ssh`.
- `prune-s3.sh`, `prune-sftp.sh`: keep-last-N pruning by filename timestamp.
- `install-cron.sh`: interactive helper that writes a cron entry for a chosen backup script.
- `backvault-agent.env.example`.

## 10. Deploy (`deploy/`)

- `Dockerfile` (multi-stage: node build of web/, go build with ldflags for version, final `alpine` with
  `postgresql-client`, `mariadb-client`, `mongodb-tools`, `redis`, `sqlite`, `openssh-client`, `docker-cli`,
  `tzdata`, `ca-certificates`; runs as non-root `backvault` user, `VOLUME /data`, `EXPOSE 8080`, healthcheck).
- `docker-compose.yml` (backvault + optional minio + optional postgres sample for trying it out).
- `systemd/backvault.service` (hardened unit), `install.sh` (builds or downloads binary to /usr/local/bin, creates
  user, data dir, unit), `backvault.example.yaml`.
- `Makefile` targets: `web`, `build`, `test`, `lint`, `dev-server`, `dev-web`, `docker`, `clean`.

## 11. Definition of done

- `make build` produces `bin/backvault` with the embedded panel; `go test ./...` and `npm run typecheck && npm run build` pass.
- Fresh start: `./bin/backvault serve` → setup page → create admin → add local destination + files source →
  create job → run → artifact appears → download works → restore to path works → prune works → logs stream live.
- SFTP and S3 destinations verified against `atmoz/sftp` and `minio/minio` containers; PostgreSQL source
  verified against `postgres:16` container (integration tests behind `BACKVAULT_TEST_DOCKER=1`).
- Docs cover install, first backup, every driver, Hetzner Storage Box, S3 providers, encryption, retention,
  push/ingest, restore, API, CLI, scripts, troubleshooting.
