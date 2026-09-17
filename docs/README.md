# Backvault documentation

Backvault is a self-hosted backup manager: one binary, one admin panel, every backup accounted
for. This is the full documentation, one topic per page.

## Start here

| Page | What it covers |
|---|---|
| [Introduction](introduction.md) | What Backvault is, what it is not, and how it is built |
| [Install](install.md) | Binary, Docker, Compose, systemd, reverse proxies, HTTPS |
| [First backup](first-backup.md) | From a fresh install to a verified restore |
| [Concepts](concepts.md) | Sources, destinations, jobs, runs, artifacts and the pipeline |

## Configuring Backvault

| Page | What it covers |
|---|---|
| [Configuration](configuration.md) | Every environment variable, YAML key and flag |
| [Retention](retention.md) | The keep policy with worked examples |
| [Encryption](encryption.md) | age passphrases, and decrypting outside Backvault |
| [Notifications](notifications.md) | Email, webhook, Slack, Discord, Telegram, ntfy |
| [Security](security.md) | Master key, secrets at rest, tokens, backing up Backvault itself |

## Drivers

| Page | What it covers |
|---|---|
| [Sources](sources/README.md) | One page per source driver, with fields and gotchas |
| [Destinations](destinations/README.md) | One page per destination driver |
| [S3 providers](destinations/s3-providers.md) | AWS, MinIO, Backblaze B2, Wasabi, Cloudflare R2, Hetzner |
| [Hetzner Storage Box](destinations/hetzner-storage-box.md) | Sub accounts, port 23, SSH keys, WebDAV |

Source drivers: [PostgreSQL](sources/postgres.md), [MySQL and MariaDB](sources/mysql.md),
[MongoDB](sources/mongodb.md), [Redis](sources/redis.md), [SQLite](sources/sqlite.md),
[files and directories](sources/files.md), [SSH](sources/ssh.md), [Docker](sources/docker.md),
[command](sources/command.md), [push](sources/push.md).

Destination drivers: [local](destinations/local.md), [S3](destinations/s3.md),
[SFTP](destinations/sftp.md), [WebDAV](destinations/webdav.md).

## Running backups

| Page | What it covers |
|---|---|
| [Push and ingest](push-and-ingest.md) | Push jobs, ingest tokens, cron, overdue detection |
| [Scripts](scripts.md) | Every standalone script, flag by flag |
| [Restore](restore.md) | Download, restore to a path, restore into the source |
| [Monitoring](monitoring.md) | Health endpoints, Prometheus metrics, alerting rules |

## Reference

| Page | What it covers |
|---|---|
| [API reference](api.md) | Every endpoint, with request and response examples |
| [CLI reference](cli.md) | Every command and flag |
| [Troubleshooting](troubleshooting.md) | Symptoms, causes, fixes |
| [FAQ](faq.md) | The questions people ask before adopting it |
| [Release process](release-process.md) | How a release is cut, what it produces, how to verify it |

## Conventions used in these pages

- Commands prefixed with `sudo` need root on the Backvault host, the rest do not.
- Artifact filenames follow `<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]`
  with timestamps in UTC, whatever timezone the schedule uses.
- Placeholders are written like `db-prod`, `uXXXXXX.your-storagebox.de` and `bvt_...`, and
  are never real values.
- Anything described as verified was tested against a real service in a container, and the
  page says what was tested.

Images referenced by the documentation live in `docs/images/`.
