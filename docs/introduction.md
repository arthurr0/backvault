# Introduction

Backvault is a self-hosted backup manager: one binary, one admin panel, every backup accounted for.

## The problem

Most backup setups are a collection of cron jobs. Each one works when it is written and
then drifts out of sight. Nobody notices that the nightly dump has been failing for three
weeks, that the retention script kept deleting the wrong file, or that the archive on the
storage box cannot be restored because the passphrase was lost. The failure mode is silent
and you discover it on the day you need the backup.

Backvault fixes the accounting problem. Every backup is a run with a status, a log, a size, a
checksum and a list of destinations that accepted it. A job that should have run and did
not is marked overdue. An artifact that disappeared from its destination is marked missing.
You can open the panel and see, for every job, when it last succeeded and how large the
result was.

The tagline is the whole product: **Every backup, accounted for.**

## What it backs up

Backvault talks to sources through drivers. Each driver knows how to produce one stream of
bytes, and the engine handles everything after that.

| Source | What it does |
| --- | --- |
| PostgreSQL | `pg_dump` in custom or plain format, or a whole cluster with `pg_dumpall` |
| MySQL and MariaDB | `mysqldump` or `mariadb-dump` with a consistent snapshot |
| MongoDB | `mongodump --archive` |
| Redis | `redis-cli --rdb` |
| SQLite | an online backup that is safe while the database is in use |
| Files and directories | a tar archive with exclude patterns |
| Remote command over SSH | runs a command on another host and reads its standard output |
| Docker | a volume archived through a helper container, or a command inside a container |
| Custom command | any local command that writes a backup to standard output |
| Push | receives artifacts from scripts on other hosts through the ingest API |

The driver pages under `sources/` describe the fields, the external tools each one needs,
and how restore behaves. Restoring back into the source is not universal: `redis` and `push`
have no restore at all, the Docker driver restores volumes but not `exec` sources, and the
`command` and `ssh` drivers only restore when you give them a restore command.

## Where it ships them

| Destination | Notes |
| --- | --- |
| Local directory | atomic writes, useful as a staging area or a second copy |
| S3 compatible | AWS, MinIO, Backblaze B2, Wasabi, Cloudflare R2, Hetzner Object Storage |
| SFTP | any SSH server, including a Hetzner Storage Box on port 23 |
| WebDAV | Nextcloud and the Storage Box WebDAV endpoint |

A job can write to several destinations at once. Each destination gets its own artifact
record, so you can see exactly which copies exist. See `concepts.md` for the model and
`destinations/s3-providers.md` for per provider settings.

## What it does not do

Being clear about this saves you an evaluation:

- Backvault is not a deduplicating snapshot engine. It does not do block level deduplication,
  incremental chains or content addressed storage the way restic and borg do. Every run
  produces one complete artifact. If you need deduplicated hourly snapshots of a large
  filesystem, use restic or borg and point Backvault at the result, or use a `command` source
  that calls them.
- Backvault is not a replication or high availability tool. It does not keep a standby database
  in sync and it does not fail over. It takes backups and stores them.
- Backvault does not back itself up. Its own state lives in the data directory, and you have to
  copy that somewhere else yourself. `security.md` says exactly what to copy.
- Backvault does not restore your infrastructure for you. It can write an artifact back to a
  path or pipe it into the source driver that produced it. Anything beyond that is your
  runbook.

## Architecture

Backvault is a single Go binary that serves an HTTP API, an embedded React admin panel, the
scheduler and the backup engine in one process. All state lives in a SQLite database in the
data directory, in WAL mode, so there is no database server to run and no message queue to
operate. External tools such as `pg_dump` are shelled out to when the ecosystem tool is the
right answer, and everything else is done in Go.

## Where to go next

- `install.md` installs Backvault from a binary, a container image, docker compose or systemd,
  and sets up a reverse proxy.
- `first-backup.md` takes a fresh install to a verified restorable backup.
- `concepts.md` explains sources, destinations, jobs, runs, artifacts and retention.
- `configuration.md` lists every environment variable and configuration file key.
- `retention.md` shows what the retention rules keep, with worked examples.
- `encryption.md` covers age encryption and how to decrypt an artifact without Backvault.
- `push-and-ingest.md` covers backing up hosts that do not run Backvault.
- `restore.md` covers download, restore to a path and restore into a source.
- `notifications.md`, `monitoring.md` and `security.md` cover operations.
- `api.md`, `cli.md` and `scripts.md` are the reference pages.
- `troubleshooting.md` and `faq.md` are where to look when something is wrong.
