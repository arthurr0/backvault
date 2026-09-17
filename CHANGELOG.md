# Changelog

All notable changes to Backvault are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.2] - 2026-09-18

### Fixed

- Row action menus in the jobs and artifacts tables are rendered above the page instead of
  inside the scrolling table, so they are no longer clipped; they open upwards near the
  bottom edge, close on Escape, outside clicks and scrolling, and support arrow-key navigation.

## [0.1.1] - 2026-09-18

### Fixed

- SFTP uploads now use concurrent writes. The client hid the artifact size from the sftp
  library, which fell back to sequential 32 KiB packets and made large uploads to remote
  hosts such as Hetzner Storage Box take many times longer than the link allowed.
- Uploads log their progress every 15 seconds (bytes sent, percent, rate, estimated time
  left) and the completion line reports size, duration and rate, so a long upload no
  longer looks like a hung run.
- Standard error output of dump tools such as mongodump and pg_dump is logged at info
  level unless the line looks like an error; mongodump progress no longer shows as warnings.

## [0.1.0] - 2026-09-17

First release. Everything below is new.

### Added

- Backup engine with a staged pipeline: prepare, pre-command, dump, pack, upload per
  destination, verify, retention, post-command, notify. Every stage is recorded on the run
  with its own status, timing and message.
- Run queue with a configurable concurrency limit, per-job locking, per-job timeouts,
  retries with a delay, and cancellation of a run that is still in flight.
- Cron scheduler with a per-job timezone, a next-run preview endpoint, and an overdue
  watcher that flags a job whose last successful run is older than its expected interval.
- Source drivers: `postgres` (`pg_dump` custom or plain, `pg_dumpall` for a whole cluster),
  `mysql` (`mysqldump` or `mariadb-dump`, one or many databases, or the whole server),
  `mongodb` (`mongodump --archive`), `redis` (`redis-cli --rdb`), `sqlite` (`sqlite3
  .backup` or `VACUUM INTO`), `files` (a tar archive built in Go), `ssh` (a remote command
  streamed over a built-in SSH client), `docker` (a named volume, or a command in a running
  container), `command` (any local shell command), and `push` (a placeholder for artifacts
  that arrive through the ingest API).
- Destination drivers: `s3` for S3-compatible object storage with multipart uploads,
  server-side encryption and storage classes, `sftp` over a built-in SSH client, `webdav`,
  and `local` directories. A job can write to several destinations in one run.
- Compression with zstd or gzip at a configurable level, and encryption with an age
  passphrase, applied on the way to the destination. The sha256 recorded for an artifact is
  taken from the same stream that is uploaded.
- Retention with keep last, hourly, daily, weekly, monthly, yearly and a maximum age,
  applied per destination, with the newest successful artifact always protected.
- Verify pass that confirms an artifact is still present at its destination with the size
  Backvault recorded, either after every upload or on demand for a single artifact.
- Restore to a path on the Backvault host, with optional extraction for tar artifacts, or back
  into the source driver, including into a different source of the same kind. Artifacts can
  also be downloaded decrypted and decompressed, or raw.
- Ingest API for hosts Backvault cannot reach: `POST /api/v1/ingest/{jobSlug}` with
  `X-Backvault-Filename`, `X-Backvault-Sha256` and an optional `?packed=1`, returning the run and
  the artifacts it produced.
- Notification channels for email over SMTP, webhooks signed with HMAC-SHA256, Slack,
  Discord, Telegram and ntfy, filtered per job and per event across the eight run,
  retention and restore events.
- Admin panel built with Vite, React and TypeScript, embedded in the binary: dashboard, job
  editor with a schedule builder, live run logs over server-sent events, artifact browser,
  destination browser, users, API tokens, audit log and settings.
- HTTP API under `/api/v1` with session cookies and `bvt_` bearer tokens, scopes for admin,
  read, run and ingest, per-token job restrictions on ingest and job runs, a CSRF header
  requirement for session writes, and a login rate limit.
- YAML export and import of sources, destinations, jobs and notification channels, with a
  dry run that reports every create and update before anything is written, and secrets
  masked unless they are explicitly included.
- Observability: Prometheus metrics with an optional token, `/healthz` and `/readyz` at both
  the root and under `/api/v1`, a server-sent event stream for run, job and artifact
  updates, and an audit log of every administrative action.
- Command line interface: `serve`, `check`, `version`, `run`, `jobs`, `runs`, `restore`,
  `push`, `user`, `token`, `export` and `import`, with exit codes that distinguish
  authentication, conflict, not-found and server failures.
- Encrypted credential storage: every secret driver field is encrypted with a 32-byte master
  key kept in `master.key` or supplied through the environment, and masked in every API
  response.
- Standalone scripts for hosts without Backvault: `backvault-push.sh`, `backup-postgres.sh`,
  `backup-mysql.sh`, `backup-mongodb.sh`, `backup-files.sh`, `backup-docker-volume.sh`,
  `upload-s3.sh`, `upload-sftp.sh`, `prune-s3.sh`, `prune-sftp.sh` and `install-cron.sh`,
  including an AWS Signature Version 4 implementation that needs only bash, curl and
  openssl.
- Deployment assets: a multi-stage Dockerfile on `debian:bookworm-slim` that ships a
  PostgreSQL 18 client, the MariaDB client, the MongoDB Database Tools, `redis-tools`,
  `sqlite3`, `openssh-client`, `age` and the Docker CLI for amd64 and arm64; a Compose file
  with an optional demo profile; a hardened systemd unit; and an idempotent installer.
- Documentation covering install, configuration, concepts, every driver, encryption,
  retention, push and ingest, restore, notifications, the API, the CLI, the scripts,
  security, monitoring and troubleshooting.
- Release automation: GoReleaser builds archives for linux and macOS on amd64 and arm64 with
  a `checksums.txt`, and a workflow pushes the multi-architecture container image to
  `ghcr.io/arthurr0/backvault` on every `v*` tag. The installer can download and verify a
  release instead of building from source, with `deploy/install.sh --download`.

### Changed

- The project was renamed from Coffer to Backvault on 2026-09-17, before the first release.
