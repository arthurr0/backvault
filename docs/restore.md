# Restore

A backup only counts once you have put it back, so Backvault offers three restore modes: download the
artifact, write it to a path on the Backvault host, or pipe it straight back into the source it came
from.

## The three modes

| Mode | What it does | Who should use it |
|---|---|---|
| download | Streams the artifact to you, decrypting and decompressing on the way | Anyone. The safest option, because nothing on a server is overwritten |
| `path` | A restore run writes the unpacked artifact to a path on the Backvault host, optionally extracting a tar | Recovering files onto the machine that runs Backvault |
| `source` | A restore run pipes the unpacked artifact into the source driver, for example `pg_restore` | Putting a database back where it came from, when the driver supports it |

`path` and `source` are the two values the API accepts in the `mode` field of a restore request.
Both create a run of kind `restore`, with stages and a live log like any other run, and both emit
`restore.done` or `restore.failed`. A restore run does not take the job lock, so it can start while
a backup of the same job is running. Downloading is not a mode at all: it is a plain HTTP stream on
a different endpoint and creates no run.

## Download

### From the panel

1. Open **Artifacts**, or the Artifacts tab of a job.
2. Filter by job, destination or filename with the `q` box.
3. Use the row action **Download**.

The file arrives decrypted and decompressed, so a `shop-db-20260917-020000.dump.zst.age` artifact
downloads as `shop-db-20260917-020000.dump`, ready for `pg_restore`.

### From the API

`GET /api/v1/artifacts/{id}/download` needs the `read` scope.

```bash
curl -fL -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -o shop-db-20260917-020000.dump \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID/download"
```

Two query parameters change what you get:

| Parameter | Effect |
|---|---|
| `raw` | `1`, `true`, `yes` or `on` skips unpacking. You get the exact bytes the destination holds, including the `.age` and `.zst` layers |
| `passphrase` | Decrypt with this passphrase instead of the one stored on the job. Taken verbatim, not trimmed |

The response is `application/octet-stream` with two headers worth reading:

```text
Content-Disposition: attachment; filename="shop-db-20260917-020000.dump"
X-Backvault-Sha256: 5e8c...
```

`X-Backvault-Sha256` is always the checksum Backvault recorded for the stored, packed artifact, so it
matches what you downloaded only with `raw=1`. `Content-Length` is sent only when `raw=1`, or when
the artifact is neither compressed nor encrypted; in every other case the size of the unpacked
stream is not known in advance and the response is chunked, so a progress bar has nothing to work
with. The filename in `Content-Disposition` is sanitized, and it is the name `backvault restore` uses
when `--to` points at a directory.

Only an artifact in status `present` can be downloaded. Anything else answers `409`:

```json
{"error": {"code": "conflict", "message": "artifact status is missing"}}
```

Use `raw=1` when you want to verify the stored sha256, when you plan to decrypt on another machine,
or when the artifact was packed by something Backvault does not understand, for example an openssl
encrypted `.enc` file from a standalone script. See [encryption.md](encryption.md).

Use `?passphrase=` when the artifact predates a passphrase change on the job. Anything in a URL can
end up in a proxy log, so prefer `?raw=1` plus a local `age --decrypt` when you can.

Verify what you downloaded against what Backvault recorded:

```bash
curl -fsSL -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID" | jq -r .sha256
curl -fsSL -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID/download?raw=1" | sha256sum
```

The two values must match. The recorded sha256 is always the checksum of the packed bytes, which is
why the second command needs `?raw=1`.

### With the CLI

```bash
backvault restore "$ARTIFACT_ID" --to ./shop-db.dump
backvault restore "$ARTIFACT_ID" --to ./shop-db.dump.zst.age --raw
```

See [cli.md](cli.md).

## The restore endpoint

Both restore modes go through one call:

```text
POST /api/v1/artifacts/{id}/restore
```

It needs the `admin` scope, no less: a restore overwrites data, so the `run` scope that is enough to
start a backup is not enough here. A token restricted with `jobSlugs` is not restricted on this
endpoint, which is another reason to keep `admin` tokens rare. The body is JSON:

| Field | Type | Meaning |
|---|---|---|
| `mode` | string | Required, `path` or `source`. Anything else is a `400` with `fields: {"mode": "must be path or source"}` |
| `targetPath` | string | Required for `path`, or the call is a `400` with `fields: {"targetPath": "required"}`. For `source` it is passed on to the driver, which the sqlite and files drivers use as their target |
| `extract` | bool | `path` only. Unpack a tar into `targetPath` instead of writing the file |
| `targetSourceId` | string | `source` only. Empty means the job's own source. An id that does not exist is a `400` with `fields: {"targetSourceId": "source does not exist"}` |
| `passphrase` | string | Decrypt with this instead of the job passphrase |
| `params` | object | Driver options for a `source` restore, listed further down |

The answer is `202` with the queued run, and the work happens in the background:

```json
{"run": {"id": "01JRUN0000000000000000000", "kind": "restore", "status": "queued"}}
```

Follow it with `GET /runs/{id}`, or stream the log from `GET /runs/{id}/log/stream`. The run fails
in its prepare stage when the artifact is not `present`, with `artifact status is <status>`.

Every example here authenticates with a bearer token. The same calls work with the session cookie
the panel uses, but then they also need the `X-Requested-With: backvault` header, because the CSRF
middleware rejects any session-authenticated write without it:

```bash
curl -X POST -b cookies.txt \
  -H "Content-Type: application/json" \
  -H "X-Requested-With: backvault" \
  -d '{"mode":"path","targetPath":"/srv/restore/www","extract":true}' \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID/restore"
```

## Restore to a path

A `path` restore unpacks the artifact onto the filesystem of the Backvault host. For a tar artifact it
can extract into a directory instead of writing the archive.

### From the panel

1. Open the artifact and choose **Restore**.
2. Pick mode **Path**.
3. Enter the target path, for example `/srv/restore/www`.
4. For a `tar` artifact, tick **Extract** to unpack into that directory. Without it, the `.tar` file
   is written to the path as a file.
5. Start it, then watch the run log.

### From the API

```bash
curl -X POST -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"path","targetPath":"/srv/restore/www","extract":true}' \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID/restore"
```

`extract` only works on a tar artifact. Asked for anything else, the run fails with `extract
requested but artifact extension "dump" is not tar`. With `extract`, `targetPath` is created if
missing and the archive is unpacked into it, with entries that point outside it refused rather than
followed. Without `extract`, the unpacked stream is written to `targetPath` as a file, or into it
under the artifact's own name when the path is an existing directory, through a `.partial` file that
is renamed only once the write completed.

### Permissions and the hardened unit

The restore writes as the Backvault service user, which is `backvault` in the packaged systemd unit, and
that unit is hardened. `ProtectSystem=strict` makes the entire filesystem read only except for the
paths listed in `ReadWritePaths=`, and `ProtectHome=yes` hides `/home`, `/root` and `/run/user`
entirely. A restore to `/srv/restore` fails with a permission error until you allow it:

```ini
[Service]
ReadWritePaths=/var/lib/backvault /srv/restore
```

Then `systemctl daemon-reload` and restart Backvault. Add the narrowest path that does the job, and
remove it again when the recovery is over. Do not restore into a live document root or data
directory: restore next to it, check what you got, then move it into place yourself.

The same applies in Docker: the container can only write to paths that are mounted into it, so
restore into `/data` or mount a dedicated restore volume. See [install.md](install.md).

## Restore to the source

A `source` restore pipes the unpacked artifact back into the driver that produced it, for example
`pg_restore` for PostgreSQL or `tar -x` into a Docker volume. It is available only for drivers that
declare the `restore` capability: `postgres`, `mysql`, `mongodb`, `sqlite`, `files`, `docker`,
`command` and `ssh`. The `redis` and `push` drivers have no restore at all, so their artifacts can
only be downloaded or written to a path. The driver pages under `sources/` say the same, for example
[sources/postgres.md](sources/postgres.md).

Two checks run before the driver is handed the stream. The target source must be of the same kind as
the artifact, otherwise the run fails with `source kind mismatch: artifact was created by postgres,
target source is mysql`. And the driver must support restore, otherwise it fails with `source driver
redis does not support restore`. A source restore always runs with overwrite enabled, so drivers
that refuse to replace an existing target in other contexts will replace it here.

### From the panel

1. Open the artifact and choose **Restore**.
2. Pick mode **Source**.
3. Leave the target as the job's own source, or pick another source of the same kind under
   **Target source**. The kinds must match: a `postgres` artifact can only go into a `postgres`
   source.
4. For a `files` or `sqlite` artifact, set **Target directory** or **Target file**. Left empty
   the restore writes over the `base_dir` or `path` the source reads. When the source runs on a
   host the field names a path on that host, and the dialog says which one.
5. Set the driver options the modal offers, for example clean and create for PostgreSQL.
6. Confirm. This overwrites data on the target.

### From the API

```bash
curl -X POST -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "mode": "source",
        "targetSourceId": "01JSRC000000000000000000",
        "params": {"clean": true, "create": false}
      }' \
  "https://backvault.example.com/api/v1/artifacts/$ARTIFACT_ID/restore"
```

Point `targetSourceId` at a staging source the first time. Restoring straight into production from
a backup you have not inspected turns one incident into two.

### The params each driver reads

`params` is a flat object. Keys a driver does not know are ignored, so a typo is silent: check the
run log, which records the target and the tool it used.

**postgres.** The tool depends on the artifact extension: a `dump` artifact goes through
`pg_restore`, anything else through `psql`.

| Key | Default | Effect |
|---|---|---|
| `database` | the source's `database`, else `postgres` | Target database, `-d` |
| `clean` (or `drop`) | `false` | `pg_restore --clean`, drop objects before recreating them |
| `if_exists` | whatever `clean` is | `pg_restore --if-exists` |
| `create` | `false` | `pg_restore --create` |
| `no_owner` | `true` | `pg_restore --no-owner`, which is why a restore does not fail on roles that do not exist |
| `stop_on_error` | `true` | `psql -v ON_ERROR_STOP=1`, plain SQL only |
| `single_transaction` | `false` | `psql --single-transaction`, plain SQL only |

`database` applies to both branches. `clean`, `if_exists`, `create` and `no_owner` are ignored for a
plain SQL artifact, and `stop_on_error` and `single_transaction` are ignored for a custom format
one.

**mysql.** One key, `database`. When it is absent, and the source is not set to `all_databases`,
and the source's `databases` list holds exactly one entry, that entry is used. Otherwise the dump
replays without a target database, which is what you want for a dump that carries its own
`CREATE DATABASE` and `USE` statements.

**mongodb.**

| Key | Default | Effect |
|---|---|---|
| `drop` | `false` | `mongorestore --drop` |
| `database` | the source's `database` | Restores only that database, as `--nsInclude=<db>.*` |
| `rename_from`, `rename_to` | none | `--nsFrom=<from>.*` and `--nsTo=<to>.*`. Both are needed, `rename_from` alone does nothing |

**docker.** One key, `volume`, which overrides the volume named in the source. Only a source in
`mode: volume` can be restored; a source in `mode: exec` refuses with `restore is only supported for
docker volumes, not for exec sources`. The volume is emptied before the archive is unpacked into it,
by a throwaway container built from the source's `image`.

**command and ssh.** One key, `restore_command`, which overrides the `restore_command` of the
source. With neither set, the restore fails: the artifact is streamed into that command's standard
input, and there is nothing sensible to default to.

**sqlite.** No params. The target is `targetPath` when you send one, otherwise the source's `path`.
The stream must start with the SQLite file header or the restore refuses it, the file is written
next to the target and renamed into place, and any `-wal` and `-shm` sidecars are removed so the
restored database is not read through a stale write ahead log.

**files.** No params. The archive is unpacked under `targetPath`, or under the source's `base_dir`
when you send none. The target must be an absolute path, and archive entries that resolve outside it
are refused. Ownership is only applied when the source has `restore_ownership` turned on, and it
needs root.

## Per engine notes

### PostgreSQL

Which tool you need depends on the format the job used, which you can read off the extension:

| Extension | Produced by | Restore with |
|---|---|---|
| `.dump` | `pg_dump --format=custom` | `pg_restore` |
| `.sql` | `pg_dump --format=plain`, or `pg_dumpall` | `psql` |

A custom format dump is the better default: `pg_restore` can list its contents, restore selected
tables, and reorder for dependencies.

Worked example, verified end to end against a PostgreSQL 16 server. Create an empty database,
restore into it, then check:

```bash
createdb -h localhost -U postgres shop_restored
pg_restore -h localhost -U postgres -d shop_restored shop-db-20260917-020000.dump
psql -h localhost -U postgres -d shop_restored -c '\dt' -c 'select count(*) from orders;'
```

Inspect before you restore:

```bash
pg_restore --list shop-db-20260917-020000.dump | head -40
pg_restore --list shop-db-20260917-020000.dump | grep ' TABLE DATA '
```

Restore one table into a scratch database:

```bash
pg_restore -h localhost -U postgres -d shop_scratch --table=orders shop-db-20260917-020000.dump
```

A plain dump replays with `psql`, and `ON_ERROR_STOP` is worth the typing, otherwise a broken
statement scrolls past and you end up with a half restored database that looks fine:

```bash
psql -h localhost -U postgres -d shop_restored -v ON_ERROR_STOP=1 -f shop-db-20260917-020000.sql
```

**Version skew, with a real failure.** A dump taken with `pg_dump` 18 and restored into a
PostgreSQL 16 server produces:

```text
pg_restore: error: could not execute query: ERROR:  unrecognized configuration parameter "transaction_timeout"
Command was: SET transaction_timeout = 0;
pg_restore: warning: errors ignored on restore: 1
```

The data restored correctly, the row counts matched, and the only casualty was a `SET` the older
server does not know. Still, "errors ignored on restore: 1" is exactly the kind of line that is easy
to scroll past, and the next version gap may not be as harmless. Rules that avoid it:

- `pg_dump` must be at least as new as the server it dumps. Dumping a 16 server with a 15 client
  fails outright with a server version mismatch.
- Restore with tools from the same major version as the target server, and prefer restoring into
  the same major version the dump came from.
- Read the tail of a restore, do not assume a zero exit means a clean restore. `pg_restore` ignores
  errors by default, add `--exit-on-error` when you want it to stop.

The Backvault Docker image ships the PostgreSQL 18 client, so it dumps servers from 9.2 through
18. What it cannot do is restore a dump taken with an 18 client into an older server: run
that restore on a host with a client matching the target, or push a dump taken with the
matching client in the first place, see [push-and-ingest.md](push-and-ingest.md).


**Ownership and roles.** A dump of one database does not carry roles. Restoring it on a fresh
cluster leaves `GRANT` and `ALTER OWNER` statements failing for roles that do not exist. Either
create the roles first, or turn on the source's `all_databases` option, which dumps the cluster
with `pg_dumpall` and includes roles and tablespaces.

### MySQL and MariaDB

The artifact is plain SQL. Create the database, then replay:

```bash
mysql --host localhost --user root --password shop_restored < shop-db-20260917-020000.sql
```

A dump taken from a source with `all_databases` turned on contains its own `CREATE DATABASE` and
`USE` statements, so do not pick a target database on the command line for it. A source that lists
several entries in `databases` produces the same kind of dump, because Backvault passes them to
`mysqldump --databases`.

Notes:

- Restore into an empty database. A replay over a populated one merges rather than replaces, and
  leaves rows nobody asked for.
- `SET FOREIGN_KEY_CHECKS=0` at the top and `=1` at the end speeds up a large restore and avoids
  ordering problems. `mysqldump` output usually handles this already.
- Routines, triggers and events are only in the dump if the job asked for them. The backup script
  asks for all three by default.
- A MariaDB dump restored into MySQL, or the reverse, can fail on collations and on
  `utf8mb4_uca1400` style collation names. Restore into the engine the dump came from.

### MongoDB

The artifact is a `mongodump --archive` stream:

```bash
mongorestore --host localhost --port 27017 \
  --username root --password "$PASS" --authenticationDatabase admin \
  --archive=shop-20260917-020000.archive
```

Restore into a different namespace first, which is verified and takes one flag pair:

```bash
mongorestore --archive=shop-20260917-020000.archive \
  --nsFrom 'shop.*' --nsTo 'shop_restored.*'
```

`--drop` replaces collections that already exist. Without it, documents are inserted alongside what
is there. If the job used `--oplog`, add `--oplogReplay` to get the point in time the dump
represents.

### SQLite

The artifact is a database file. Stop the application, move the old file aside, put the new one in
place, and check it before you start again:

```bash
sqlite3 app.db 'PRAGMA integrity_check;'
```

Do not copy a file over a database that something has open. Backvault takes these backups with
`.backup` or `VACUUM INTO`, which is consistent, but restoring is your responsibility.

### Files and tar archives

```bash
tar -tvf www-20260917-020000.tar | head
tar -x -f www-20260917-020000.tar -C /srv/restore
```

List before extracting. Paths in the archive are relative to the job's base directory, so extracting
in the wrong place scatters files. Extract into an empty directory, look at what you have, then
move it into position with `rsync`, which also lets you see what would change:

```bash
rsync -a --dry-run --delete /srv/restore/www/ /var/www/
```

### Docker volumes

A volume artifact is a tar of the volume contents. Put it back with a helper container, with the
volume mounted read write:

```bash
docker run --rm -i -v pgdata:/data alpine:3.20 \
  tar -x -f - -C /data < volume-pgdata-20260917-020000.tar
```

Stop the containers that use the volume first. Restoring a database volume under a running database
corrupts it.

## Restore without Backvault

This is the path that has to work when the Backvault host is the thing you lost. Nothing below needs
Backvault, only the credentials for the destination and the passphrase.

### 1. Find the file

From S3, with any client, listing by the job slug prefix:

```bash
aws s3 ls --endpoint-url "$S3_ENDPOINT" "s3://$S3_BUCKET/shop-db/"
aws s3 cp --endpoint-url "$S3_ENDPOINT" \
  "s3://$S3_BUCKET/shop-db/shop-db-20260917-020000.dump.zst.age" .
```

From SFTP:

```bash
sftp -P 23 -i ~/.ssh/storagebox u123456@u123456.your-storagebox.de
sftp> cd backups/shop-db
sftp> ls -1
sftp> get shop-db-20260917-020000.dump.zst.age
```

Artifact names sort chronologically, because the timestamp is `YYYYMMDD-HHMMSS` in UTC, so the last
line of a sorted listing is the newest backup. See [destinations/s3.md](destinations/s3.md) and
[destinations/hetzner-storage-box.md](destinations/hetzner-storage-box.md) for credentials and
endpoints.

### 2. Decrypt and decompress

```bash
age --decrypt --output shop-db-20260917-020000.dump.zst \
  shop-db-20260917-020000.dump.zst.age
zstd -d shop-db-20260917-020000.dump.zst
```

For an openssl encrypted `.enc` artifact from a standalone script:

```bash
openssl enc -d -aes-256-cbc -pbkdf2 \
  -in shop-db-20260917-020000.dump.zst.enc \
  -pass file:/path/to/passphrase \
  | zstd -dc > shop-db-20260917-020000.dump
```

### 3. Restore

Use the per engine commands above.

### Keep this runbook off the Backvault host

Write down, somewhere that is not this server:

- The destination, the bucket or base path, and read credentials for it.
- The encryption passphrase for each job that uses one.
- Which engine and version each job backs up, so you know what client to install.

A copy of this page, three credentials and a laptop should be enough to get your data back. Test it
once a quarter, as described in [encryption.md](encryption.md).

## When a restore fails

| Symptom | Cause | Fix |
|---|---|---|
| `no such file or directory` on a path restore | `ProtectSystem=strict` or a missing parent directory | Add the path to `ReadWritePaths=`, create the parent |
| Downloaded file is still `.age` | Downloaded with `?raw=1`, or the artifact is `.enc` | Drop `raw=1`, or decrypt by hand |
| `age: incorrect passphrase` | Job passphrase changed after the artifact was made | Pass the old one with `?passphrase=` or to `age` directly |
| `unrecognized configuration parameter` | Client newer than the server | Use matching client versions, see above |
| Restore to source refused | The driver has no `restore` capability, or the kinds differ | Download and restore by hand |
| `403 missing scope: admin` | The token can start runs but not restores | Use a token with the `admin` scope |
| `403 missing X-Requested-With: backvault header` | A session cookie call without the CSRF header | Add the header, or use a bearer token |
| `400` with `fields.mode` or `fields.targetPath` | `mode` was not `path` or `source`, or a path restore had no `targetPath` | Fix the body, see the field table above |
| `409 artifact status is missing` | The artifact is no longer on the destination | Run a verify, then restore an artifact that is still `present` |
| sha256 does not match | Corruption on the destination, or you compared against an unpacked download | Compare with `?raw=1`, then run a verify on the artifact |

More in [troubleshooting.md](troubleshooting.md).
