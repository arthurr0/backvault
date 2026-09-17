# Standalone scripts

Every script in `scripts/`, with all of its flags, its environment variables and its exit
codes. These scripts are for hosts that do not run Backvault: a database server that dumps and
pushes to a Backvault push job, or a machine that uploads straight to S3 or SFTP without Backvault
in the path at all.

They need bash, curl and coreutils, nothing else that is not already on a normal Linux host.
They log to stderr with UTC timestamps, they clean their temporary files with a trap, they
never place a secret on a command line, and they behave correctly with spaces in paths.

## Installing them on a host

Copy the whole directory, including `lib/`, because every script sources
`scripts/lib/common.sh` relative to its own location.

```bash
sudo install -d -m 0755 /opt/backvault
sudo cp -r scripts/* /opt/backvault/
sudo install -d -m 0750 /etc/backvault
sudo install -m 0600 /opt/backvault/backvault-agent.env.example /etc/backvault/agent.env
sudo chmod 0700 /etc/backvault
```

Then edit `/etc/backvault/agent.env`, and use [install-cron.sh](#install-cronsh) to schedule a
job.

## Shared conventions

### Common options

Every `backup-*.sh` script accepts the same delivery and packing options on top of its own
source options.

| Option | Environment | Default | Description |
|---|---|---|---|
| `--to backvault\|s3\|sftp\|local` | `BACKVAULT_TO` | `backvault` | Where the artifact goes |
| `--dest-dir DIR` | `BACKVAULT_DEST_DIR` | | Target directory for `--to local` |
| `--compress zstd\|gzip\|none` | `BACKVAULT_COMPRESS` | `auto` | `auto` picks zstd when it is installed, then gzip, then none |
| `--compress-level N` | `BACKVAULT_COMPRESS_LEVEL` | `6` | Passed to zstd or gzip |
| `--encrypt age\|openssl\|none` | `BACKVAULT_ENCRYPT` | `none` | See [encryption](encryption.md) |
| `--passphrase-file FILE` | `BACKVAULT_PASSPHRASE_FILE` | | Required when `--encrypt` is not `none` |
| `--prefix NAME` | `BACKVAULT_PREFIX` | derived | First part of the artifact filename |
| `--name FILENAME` | | | Full filename, overrides `--prefix` and the timestamp |
| `--temp-dir DIR` | `BACKVAULT_TMPDIR` | `$TMPDIR` or `/tmp` | Where the dump is spooled |
| `--job SLUG` | `BACKVAULT_JOB` | | Backvault job slug for `--to backvault` |
| `--backvault-url URL` | `BACKVAULT_URL` | | Backvault base URL for `--to backvault` |
| `--token-file FILE` | `BACKVAULT_TOKEN_FILE` | | File holding the API token |
| `--dry-run` | | off | Print the plan, touch nothing |
| `--debug` | `BACKVAULT_DEBUG=1` | off | Verbose logging on stderr |
| `-h`, `--help` | | | Usage text |

### Filenames

Artifacts are named the same way Backvault names them, so a directory of script uploads and a
directory of Backvault uploads sort and prune identically:

```text
<prefix>-<YYYYMMDD>-<HHMMSS>.<ext>[.zst|.gz][.age|.enc]
```

The timestamp is UTC. `<ext>` is the natural extension of the dump: `dump` for a PostgreSQL
custom dump, `sql` for plain SQL, `archive` for mongodump, `tar` for a tar archive. `.age`
marks age encryption, `.enc` marks the openssl fallback.

The timestamp is the only thing the prune scripts read, so do not rename artifacts by hand.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure |
| 2 | Usage error, a bad or missing flag |
| 3 | A required command is not installed |
| 4 | Configuration error, for example a missing bucket or an unreadable key |
| 5 | The dump failed |
| 6 | Compression or encryption failed |
| 7 | Delivery failed after all retries |
| 8 | The remote side rejected the request, for example an invalid token or a 403 from S3 |
| 9 | A verification step failed |

In cron, anything other than 0 is worth an alert. Codes 5 and 7 are the two you will
actually see: the database was unreachable, or the destination was.

### Secrets

Passwords, passphrases and tokens are read from files, never from flags, because command
lines are visible to every user on the host through `ps`. The scripts warn when a secret
file is readable by anyone other than its owner. Use mode 0600.

The one place where a secret reaches an environment variable is `PGPASSWORD` for
`pg_dump`, which is how libpq expects it and which is only visible to the same user.

---

## backvault-push.sh

Uploads a file, or standard input, to a Backvault push job through the ingest API.

```bash
backvault-push.sh --job db-prod --file /var/backups/db-prod-20260917-020000.dump.zst --packed
pg_dump -Fc app | zstd | backvault-push.sh --job db-prod --name app-20260917-020000.dump.zst --packed
```

| Option | Environment | Default | Description |
|---|---|---|---|
| `--job SLUG` | `BACKVAULT_JOB` | | Target job slug, required |
| `--file PATH` | | stdin | File to upload, omit to read standard input |
| `--name FILENAME` | | basename | Value of `X-Backvault-Filename` |
| `--url URL` | `BACKVAULT_URL` | | Backvault base URL without a trailing slash, required |
| `--token TOKEN` | `BACKVAULT_TOKEN` | | API token with the `ingest` scope |
| `--token-file FILE` | `BACKVAULT_TOKEN_FILE` | | Read the token from a file instead |
| `--packed` | | off | The file is already compressed and encrypted, adds `?packed=1` |
| `--sha256 HEX` | | computed | Use this checksum instead of computing one |
| `--no-sha256` | | off | Do not send `X-Backvault-Sha256` |
| `--retries N` | `BACKVAULT_RETRIES` | 5 | Attempts after the first one |
| `--retry-delay SECONDS` | `BACKVAULT_RETRY_DELAY` | 5 | Initial backoff, doubled per attempt, capped at 600 |
| `--timeout SECONDS` | `BACKVAULT_TIMEOUT` | 3600 | curl max time per attempt |
| `--connect-timeout SEC` | `BACKVAULT_CONNECT_TIMEOUT` | 15 | curl connect timeout |
| `--insecure` | `BACKVAULT_INSECURE` | off | Skip TLS certificate verification |
| `--cacert FILE` | `BACKVAULT_CACERT` | | CA bundle for TLS verification |
| `--dry-run` | | off | Print the request that would be sent |
| `--debug` | `BACKVAULT_DEBUG` | off | Verbose logging |

What it does, in order: computes the sha256 of the file (with `sha256sum`, `shasum` or
`openssl`, whichever exists), streams the body with `curl --upload-file` so a large file is
never loaded into memory and `Content-Length` is correct, and sends the token in an
`Authorization: Bearer` header read from a temporary file so it never appears in `ps`.

A successful ingest answers `201` with `{"run": {...}, "artifacts": [...]}`. The run id is
read from `run.id`, printed on stdout, and nothing else is, so it can be captured. All
logging goes to stderr.

```bash
run_id=$(backvault-push.sh --job db-prod --file dump.zst --packed)
echo "queued $run_id"
```

When Backvault creates the run and the pipeline then fails, for example on a checksum
mismatch, it answers `422` and puts the run id at `error.fields.runId` instead. The script
prints that id on stdout too, logs the server message, and exits 8: the upload is not
retried, because a second attempt would only create a second failed run. Look the run up in
the panel to see which stage failed.

Retries cover connection errors, HTTP 5xx, 408, 429 and 423. A `423` means the job is
already running, which passes on its own. Any other 4xx is final and exits with code 8,
because retrying a rejected token, an unknown job slug or a bad checksum never helps.

There is no upload size limit at the HTTP layer, so a large artifact is never rejected for
its size. Backvault does not answer `409` or `413` on this endpoint.

With no `--file` and no terminal on stdin, the input is spooled to a temporary file first,
which is what makes the checksum and `Content-Length` possible. Pass `--name` in that case,
otherwise the filename falls back to `<job>-<timestamp>.bin` and Backvault cannot derive the
extension from it.

See [push and ingest](push-and-ingest.md) for the job and token setup.

---

## backup-postgres.sh

Dumps one database with `pg_dump`, or a whole cluster with `pg_dumpall`, then packs and
delivers it.

```bash
backup-postgres.sh --host db.internal --user backup --database shop \
  --password-file /etc/backvault/pg.pass --job shop-db
backup-postgres.sh --all-databases --password-file /etc/backvault/pg.pass \
  --to s3 --prefix cluster
```

| Option | Environment | Default | Description |
|---|---|---|---|
| `--host HOST` | `PGHOST` | local socket | Server host |
| `--port PORT` | `PGPORT` | 5432 | Server port |
| `--user USER` | `PGUSER` | | Role to connect as |
| `--database NAME` | `PGDATABASE` | | Database to dump |
| `--all-databases` | | off | Use `pg_dumpall`, always plain SQL, includes roles |
| `--format custom\|plain` | | `custom` | `custom` gives `.dump`, `plain` gives `.sql` |
| `--schema-only` | | off | Schema without rows |
| `--data-only` | | off | Rows without the schema |
| `--schema NAME` | | | Restrict to a schema, repeatable |
| `--exclude-table GLOB` | | | Skip matching tables, repeatable |
| `--sslmode MODE` | `PGSSLMODE` | | For example `require` |
| `--password-file FILE` | | | Password file, read into `PGPASSWORD` for the child |
| `--pgpassfile FILE` | `PGPASSFILE` | | Use a libpq password file instead |
| `--binary-path DIR` | | `PATH` | Directory holding `pg_dump` and `pg_dumpall` |
| `--extra-arg ARG` | | | Extra argument for the dump tool, repeatable |

The custom format always gets `--compress=0` so that compression happens once, in the pack
step, where the checksum is taken. Pass `--extra-arg --compress=9` if you want pg_dump to do
it instead, and then use `--compress none`.

`--no-password` is always set, so a missing or wrong password fails immediately instead of
waiting for a prompt that will never be answered in cron.

Verified behaviour: with a wrong password the script exits 5, logs the libpq error and
delivers nothing. A dump of an empty result is treated as a failure.

Gotcha: `pg_dump` must be at least as new as the server. The other direction bites on
restore, see [restore](restore.md) and [sources/postgres.md](sources/postgres.md).

---

## backup-mysql.sh

Dumps MySQL or MariaDB with `mysqldump` or `mariadb-dump`, whichever is installed.

```bash
backup-mysql.sh --host 127.0.0.1 --user backup --password-file /etc/backvault/my.pass \
  --database shop --ignore-table shop.sessions --job shop-mysql
```

| Option | Default | Description |
|---|---|---|
| `--host HOST` | localhost | Server host |
| `--port PORT` | 3306 | Server port |
| `--socket PATH` | | Unix socket instead of host and port |
| `--user USER` | | User to connect as |
| `--password-file FILE` | | Password file, passed through a defaults file |
| `--database NAME` | | Database to dump |
| `--all-databases` | off | Dump every database |
| `--ignore-table SPEC` | | `db.table` to skip, repeatable |
| `--no-single-transaction` | off | Drop the consistent snapshot, for MyISAM |
| `--no-routines` | off | Skip stored procedures and functions |
| `--no-triggers` | off | Skip triggers |
| `--no-events` | off | Skip scheduled events |
| `--no-tablespaces` | off | Needed when the user has no `PROCESS` privilege |
| `--ssl-mode MODE` | | For example `REQUIRED` |
| `--binary-path DIR` | `PATH` | Directory holding the dump tool |
| `--extra-arg ARG` | | Extra argument, repeatable |

The connection settings and the password are written to a temporary file with mode 0600 and
passed with `--defaults-extra-file`, so nothing sensitive appears in the process list. The
file is removed by the exit trap. The defaults are `--single-transaction --routines
--triggers --events`, which is what you want for InnoDB.

Verified behaviour: on a MariaDB 11 host the script picks `mariadb-dump`, honours
`--ignore-table` and produces a restorable `.sql` dump.

---

## backup-mongodb.sh

Dumps MongoDB with `mongodump --archive`, which produces a single stream.

```bash
backup-mongodb.sh --uri mongodb://db.internal:27017 --user backup \
  --password-file /etc/backvault/mongo.pass --database shop --job shop-mongo
```

| Option | Default | Description |
|---|---|---|
| `--uri URI` | | Connection string, takes precedence over host and port |
| `--host HOST` | localhost | Server host |
| `--port PORT` | 27017 | Server port |
| `--user USER` | | User to authenticate as |
| `--password-file FILE` | | Password file, passed through a tools config file |
| `--auth-db NAME` | admin | Authentication database |
| `--database NAME` | every database | Database to dump |
| `--collection NAME` | | Collection to dump, needs `--database` |
| `--exclude-collection NAME` | | Collection to skip, repeatable, needs `--database` |
| `--read-preference PREF` | | For example `secondaryPreferred` |
| `--oplog` | off | Point in time snapshot of a replica set |
| `--binary-path DIR` | `PATH` | Directory holding `mongodump` |
| `--extra-arg ARG` | | Extra argument, repeatable |

The password is written to a temporary MongoDB tools config file with mode 0600 and passed
with `--config`. A password embedded in `--uri` is visible to every user on the host, so
prefer `--password-file`.

Verified behaviour: against MongoDB 7 with Database Tools 100.18 the config file is accepted,
`--exclude-collection` is honoured, and the resulting archive restores with
`mongorestore --archive`.

---

## backup-files.sh

Archives files and directories with tar.

```bash
backup-files.sh --path /var/www --path /etc/nginx --exclude '*/cache/*' --job web-files
backup-files.sh --base-dir /srv --path site --to local --dest-dir /mnt/backups
```

| Option | Default | Description |
|---|---|---|
| `--path PATH` | | File or directory to archive, repeatable, at least one required |
| `--exclude GLOB` | | tar exclude pattern, repeatable |
| `--exclude-from FILE` | | Read exclude patterns from a file |
| `--base-dir DIR` | | Change into this directory first and store relative paths |
| `--one-file-system` | off | Do not cross mount points |
| `--follow-symlinks` | off | Store what symlinks point at instead of the links |
| `--tar-binary PATH` | `tar` | Which tar to use |

Use `--base-dir` whenever you can. Archiving `/srv/site` with `--base-dir /srv --path site`
gives an archive with relative paths, which is far easier to restore somewhere else than one
with absolute paths.

A path that does not exist produces a warning, not a failure, so a missing optional directory
does not break a nightly backup. tar failing produces exit code 5.

Verified behaviour: excludes are honoured, filenames with spaces round trip correctly, and
the same archive was delivered to all four targets with an identical sha256.

---

## backup-docker-volume.sh

Archives a Docker volume by running tar in a throwaway helper container.

```bash
backup-docker-volume.sh --volume pgdata --job pgdata-volume
backup-docker-volume.sh --volume pgdata --stop-container postgres --to s3
```

| Option | Default | Description |
|---|---|---|
| `--volume NAME` | | Volume to archive, required |
| `--path PATH` | `.` | Path inside the volume |
| `--exclude GLOB` | | tar exclude pattern, repeatable |
| `--helper-image IMAGE` | `alpine:3.20` | Image providing tar |
| `--docker-binary NAME` | `docker` | `docker` or `podman`, also `DOCKER_BINARY` |
| `--stop-container NAME` | | Stop during the archive and start again after, repeatable |
| `--run-arg ARG` | | Extra argument for the helper container run, repeatable |

The volume is mounted read only. Containers stopped with `--stop-container` are started
again by the exit trap, including when the archive fails or the script is interrupted.

Archiving the volume of a running database gives you a crash consistent copy, not a clean
backup. Prefer a real dump with `backup-postgres.sh` or `backup-mysql.sh`, and keep the
volume archive for configuration and file data.

Verified behaviour: tested with `--docker-binary podman` against a rootless volume, excludes
honoured.

---

## upload-s3.sh

Uploads one file to an S3 compatible bucket.

```bash
upload-s3.sh --file backup.tar.zst --prefix web-files
```

| Option | Environment | Default | Description |
|---|---|---|---|
| `--file PATH` | | | File to upload, required |
| `--name NAME` | | basename | Object name inside the prefix |
| `--key KEY` | | | Full object key, overrides `--name` and the prefix |
| `--bucket NAME` | `S3_BUCKET` | | Bucket, required |
| `--prefix PATH` | `S3_PREFIX` | | Key prefix |
| `--endpoint URL` | `S3_ENDPOINT` | AWS | Endpoint, empty means AWS |
| `--region NAME` | `S3_REGION` | `us-east-1` | Region |
| `--path-style` | `S3_PATH_STYLE=1` | auto | Force path style addressing |
| `--virtual-host` | `S3_PATH_STYLE=0` | auto | Force virtual host addressing |
| `--storage-class CLASS` | `S3_STORAGE_CLASS` | | `x-amz-storage-class` |
| `--sse ALGO` | `S3_SSE` | | `x-amz-server-side-encryption`, for example `AES256` |
| `--method auto\|aws\|mc\|curl` | `S3_METHOD` | `auto` | Which uploader to use |
| `--insecure` | `S3_INSECURE` | off | Skip TLS certificate verification |
| `--retries N` | `S3_RETRIES` | 3 | Attempts after the first one |
| `--retry-delay SECONDS` | `S3_RETRY_DELAY` | 5 | Initial backoff, doubled, capped at 600 |
| `--timeout SECONDS` | `S3_TIMEOUT` | 3600 | curl max time per attempt |
| `--dry-run` | | off | Print the target and exit |

Credentials come from `S3_ACCESS_KEY` and `S3_SECRET_KEY`, or from `AWS_ACCESS_KEY_ID` and
`AWS_SECRET_ACCESS_KEY`. There is deliberately no flag for them.

`auto` prefers the `aws` CLI, then `mc`, then the built in uploader, which signs the request
with AWS Signature Version 4 using only bash, curl and openssl. The HMAC is computed without
ever passing the secret key as a command line argument.

Path style addressing is chosen automatically: on for a custom endpoint, off for AWS. MinIO
and most self hosted gateways need it on.

The built in uploader performs a single PUT and therefore refuses files larger than 5 GiB
with exit code 4. Install the `aws` CLI or `mc` for larger objects, or split the backup.

Verified behaviour: tested against MinIO. PUT, GET, ListObjectsV2 with a prefix and a
continuation token, and DELETE all sign correctly, and a 3 MB object round tripped byte for
byte. The signing code also passes the RFC 4231 HMAC-SHA256 test vectors.

See [S3 providers](destinations/s3-providers.md) for per provider settings.

---

## upload-sftp.sh

Uploads one file to an SFTP host, atomically.

```bash
upload-sftp.sh --file backup.tar.zst --host uXXXXXX.your-storagebox.de --port 23 \
  --user uXXXXXX-sub1 --key /etc/backvault/sftp_key --base-path backups/web
```

| Option | Environment | Default | Description |
|---|---|---|---|
| `--file PATH` | | | File to upload, required |
| `--name NAME` | | basename | Remote filename |
| `--host HOST` | `SFTP_HOST` | | Host, required |
| `--port PORT` | `SFTP_PORT` | 22 | Use 23 for a Hetzner Storage Box |
| `--user USER` | `SFTP_USER` | | User, required |
| `--key FILE` | `SFTP_KEY` | | Private key file |
| `--password-file FILE` | `SFTP_PASSWORD_FILE` | | Password file, needs `sshpass` |
| `--base-path PATH` | `SFTP_BASE_PATH` | | Remote directory, created when missing |
| `--mode sftp\|ssh` | `SFTP_MODE` | `sftp` | Batch mode, or streaming through `ssh cat` |
| `--known-hosts FILE` | `SFTP_KNOWN_HOSTS` | | known_hosts file to verify against |
| `--host-key-checking MODE` | `SFTP_HOST_KEY_CHECKING` | `yes` | `yes`, `accept-new` or `no` |
| `--ssh-option OPT` | | | Extra `-o` option, repeatable |
| `--retries N` | `SFTP_RETRIES` | 3 | Attempts after the first one |
| `--retry-delay SECONDS` | `SFTP_RETRY_DELAY` | 5 | Initial backoff, doubled, capped at 600 |
| `--dry-run` | | off | Print the target and exit |

The file is written as `<name>.partial` and renamed once the transfer finished, so an
interrupted upload never looks like a complete backup. Missing directories in the base path
are created one level at a time, because SFTP has no `mkdir -p`.

Key authentication is the default and the only option when `sshpass` is not installed. A
password file is handed to `sshpass -f`, never to a flag.

Host keys are verified by default. Point `--known-hosts` at a file you fill with
`ssh-keyscan -p 23 host >> /etc/backvault/known_hosts`, or use `--host-key-checking accept-new`
for the first run. `no` is accepted and logs a warning, do not use it over the internet.

`--mode ssh` needs shell access on the target. Servers that expose only the SFTP subsystem,
which includes Hetzner Storage Boxes and the `atmoz/sftp` image, refuse it with
`This service allows sftp connections only`. Use the default `sftp` mode there.

Verified behaviour: both modes tested, including a base path containing a space, host key
verification with a known_hosts file, and `accept-new` writing the key on first contact.

---

## prune-s3.sh

Keeps the newest N objects under a prefix and deletes the rest.

```bash
prune-s3.sh --prefix db-prod --keep 14 --dry-run
prune-s3.sh --prefix db-prod --keep 14
```

| Option | Default | Description |
|---|---|---|
| `--keep N` | | How many to keep, required, at least 1 |
| `--prefix PATH` | `S3_PREFIX` | Key prefix to prune |
| `--pattern GLOB` | `*` | Extra filter on the object name |
| `--bucket NAME` | `S3_BUCKET` | Bucket |
| `--endpoint URL` | `S3_ENDPOINT` | Endpoint |
| `--region NAME` | `S3_REGION` | Region |
| `--path-style`, `--virtual-host` | auto | Addressing style |
| `--method auto\|aws\|mc\|curl` | `auto` | Which client to use |
| `--insecure` | off | Skip TLS certificate verification |
| `--dry-run` | off | List what would be deleted |

Objects are ordered by the `YYYYMMDD-HHMMSS` timestamp in their name, not by upload time, so
a re-uploaded old backup does not push a newer one out. Objects whose name carries no such
timestamp are never deleted, which keeps a `README` or a manifest in the same prefix safe.

`--keep 0` is refused. Always run it once with `--dry-run`.

Verified behaviour: tested against MinIO with six timestamped objects and one without a
timestamp, keeping 3. Listing pages through continuation tokens.

---

## prune-sftp.sh

Keeps the newest N files in a remote directory and deletes the rest.

```bash
prune-sftp.sh --base-path backups/web --keep 14 --dry-run
```

It takes the same connection options as `upload-sftp.sh` (`--host`, `--port`, `--user`,
`--key`, `--password-file`, `--known-hosts`, `--host-key-checking`, `--ssh-option`) plus
`--keep`, `--base-path`, `--pattern` and `--dry-run`.

Ordering works exactly as in `prune-s3.sh`. On top of that, `.partial` files are never
deleted, because one may belong to an upload that is running right now.

Verified behaviour: tested against `atmoz/sftp` with five timestamped files, one
untimestamped file and one `.partial`, keeping 2.

---

## install-cron.sh

Interactive helper that writes a crontab entry for one of the backup scripts.

```bash
install-cron.sh
install-cron.sh --script backup-postgres.sh --preset daily \
  --env-file /etc/backvault/agent.env --args "--database shop --job shop-db" --tag shop-db --yes
install-cron.sh --list
install-cron.sh --remove shop-db
```

| Option | Description |
|---|---|
| `--script NAME` | Backup script, a bare name or a full path |
| `--schedule CRON` | Five field cron expression |
| `--preset NAME` | `daily`, `hourly`, `weekly`, `monthly` or `fifteen-minutes` |
| `--env-file FILE` | Sourced before the script runs, for credentials |
| `--args "ARGS"` | Arguments for the backup script |
| `--tag NAME` | Marker for this entry, default derived from the script name |
| `--log-file FILE` | Append output here, default `/dev/null` so cron mails failures |
| `--list` | Show the entries this helper manages |
| `--remove TAG` | Remove the entry with this tag |
| `--yes` | Do not ask for confirmation |
| `--dry-run` | Print the line without installing it |

Run without options it lists the available scripts and asks for the script, the schedule, the
environment file, the arguments and a tag. Every entry it writes carries a `BACKVAULT_CRON_TAG`
marker, which is how `--list` and `--remove` find their own lines again and how installing
the same tag twice replaces the entry instead of duplicating it. Paths are quoted, so spaces
are safe, and a literal `%` is escaped the way cron requires.

The result looks like this:

```text
30 1 * * * BACKVAULT_CRON_TAG=shop-db . /etc/backvault/agent.env && /opt/backvault/backup-postgres.sh --database shop --job shop-db >> /var/log/backvault/shop-db.log 2>&1
```

Keep `--log-file` unset while you are setting a job up, so cron mails you the output, then
point it at a log file once it is reliable.

---

## backvault-agent.env.example

A template for the environment file that cron sources. It carries no comments by design, the
keys are explained in this page and in [configuration](configuration.md). Copy it to
`/etc/backvault/agent.env`, fill in what you need, delete the rest, and set mode 0600 with the
owner set to the account that runs the cron job.

## A complete example

A database host that dumps PostgreSQL every night, encrypts the dump and pushes it to a
Backvault push job, and a second entry that keeps a local copy on a mounted disk with its own
retention.

```bash
sudo install -d -m 0700 /etc/backvault
printf '%s\n' 'S3cret passphrase, 6 words at least' | sudo tee /etc/backvault/backup.pass >/dev/null
sudo chmod 0600 /etc/backvault/backup.pass
sudo cp scripts/backvault-agent.env.example /etc/backvault/agent.env
sudo chmod 0600 /etc/backvault/agent.env

sudo /opt/backvault/install-cron.sh --script backup-postgres.sh --preset daily \
  --env-file /etc/backvault/agent.env \
  --args "--database shop --password-file /etc/backvault/pg.pass --encrypt age --passphrase-file /etc/backvault/backup.pass --job shop-db" \
  --tag shop-db --yes

sudo /opt/backvault/install-cron.sh --script backup-postgres.sh --schedule "45 1 * * *" \
  --env-file /etc/backvault/agent.env \
  --args "--database shop --password-file /etc/backvault/pg.pass --to local --dest-dir /mnt/backups/shop" \
  --tag shop-db-local --yes
```

Then verify the first run by hand before trusting the schedule:

```bash
. /etc/backvault/agent.env
/opt/backvault/backup-postgres.sh --database shop --password-file /etc/backvault/pg.pass \
  --job shop-db --dry-run
```
