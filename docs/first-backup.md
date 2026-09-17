# Your first backup

This walkthrough takes a fresh install to a backup you have actually restored, in about ten
minutes, and then repeats the exercise with a PostgreSQL database going to remote storage
with compression and encryption.

Before you start, have Backvault running and reachable in a browser. See `install.md`.

## Part one: files to a local directory

### 1. Create the first administrator

Open the panel. A fresh install sends you to `/setup`. Fill in your name, an email address
and a password, and submit.

You should see: the dashboard, empty, with zero jobs and a prompt to create one. You are
logged in. Going back to `/setup` now refuses to create a second account, because setup is
only available while there are no users.

If you are automating the install instead, set `BACKVAULT_ADMIN_EMAIL` and
`BACKVAULT_ADMIN_PASSWORD` before the first start and the administrator is created for you. See
`configuration.md`.

### 2. Add a destination

Go to Destinations and create one. Pick Local directory, name it `Local disk`, and set the
path to a directory the Backvault service user can write to, for example `/var/lib/backvault/backups`.

Press Test connection.

You should see: a green result with a duration in milliseconds. If it fails with a
permission error, the service user cannot write there. Under the shipped systemd unit only
paths inside `/var/lib/backvault` are writable, see `install.md` section 5.

A local directory is not a real offsite backup. It is the fastest way to see the whole
pipeline work, and it is useful later as a staging copy alongside a remote destination.

### 3. Add a source

Go to Sources and create one. Pick Files and directories, name it `Etc config`, and add the
path `/etc`. Add an exclude pattern for anything noisy, for example `*/ssl/private/*`.

Press Test connection.

You should see: a success result confirming the paths exist and are readable. A path that
does not exist is reported here rather than at three in the morning.

### 4. Create a job

Go to Jobs, then New job, and work through the sections.

- **Basics**: name it `Etc nightly`. The slug is derived from the name, `etc-nightly`, and
  you can edit it. The slug appears in every artifact filename and in the ingest URL, so
  pick something you will still understand in a year.
- **Source**: pick `Etc config`.
- **Destinations**: pick `Local disk`.
- **Schedule**: choose the daily preset, or type `0 2 * * *`, and set the timezone to yours.
  The editor shows the next five occurrences. Check them before saving, because a cron
  expression that looks right and a timezone that is wrong is the most common scheduling
  mistake.
- **Pack**: compression `zstd`, encryption `none` for now.
- **Retention**: keep last 7, keep daily 7, keep monthly 6.
- **Advanced**: leave the defaults. A timeout of 0 means six hours, and retries default to 0,
  so each destination is tried once.

Save.

You should see: the job in the list with an enabled pill, the next run time, and no runs yet.

### 5. Run it now

Open the job and press Run now.

You should see: a run appear immediately with status queued, then running, and the stage
timeline filling in: `prepare`, `pre-command` (skipped), `dump`, `pack`, `upload:Local disk`,
`verify` (skipped, because Verify after upload is off), `retention`, `post-command` (skipped)
and `notify`. The log streams live. If the log does not move but the run finishes, your reverse
proxy is buffering the event stream, see `install.md` section 6.

When it finishes the status is success, and the run shows the raw size, the packed size,
the duration and a sha256 checksum.

### 6. Look at the artifact

Open the Artifacts tab of the job.

You should see: one artifact named like `etc-nightly-20260917-020000.tar.zst`, with a size,
the destination `Local disk`, status present, and the same sha256 as the run. The timestamp
in the filename is UTC, always, no matter which timezone the schedule uses. See
`concepts.md`.

Check it on disk:

```bash
ls -l /var/lib/backvault/backups/etc-nightly/
sha256sum /var/lib/backvault/backups/etc-nightly/etc-nightly-20260917-020000.tar.zst
```

The checksum has to match what the panel shows. That is the point of recording it.

### 7. Download it

Press Download on the artifact. Backvault streams it from the destination and decompresses it
on the way out, so what you get is a plain `.tar`. Add `?raw=1` to the download URL, or use
the raw option, to get the stored bytes exactly as they are.

```bash
tar tf ~/Downloads/etc-nightly-20260917-020000.tar | head
```

You should see: the paths you backed up.

### 8. Restore it to a path

Press Restore on the artifact, choose mode Path, set the target to a scratch directory such
as `/var/lib/backvault/restore-test`, and enable extract so the tar is unpacked rather than
written as one file.

You should see: a restore run with its own stages, ending in success, and the files on disk:

```bash
ls /var/lib/backvault/restore-test/etc | head
```

A permission denied here almost always means the target is outside `ReadWritePaths` in the
systemd unit. See `install.md` section 5 and `restore.md`.

This step is the one people skip. A backup you have never restored is a hypothesis.

### 9. Run retention

Press Run now a few more times so there are several artifacts, then press Prune on the job.

You should see: a prune run, and artifacts that fall outside the retention rules moved to
status pruned and deleted from the destination. The newest artifact on each destination is never
deleted, whatever the rules say. `retention.md` works through the algorithm with examples.

Part one is done. You have a backup that you have restored, with a checksum you verified.

## Part two: PostgreSQL to remote storage, compressed and encrypted

The second half is the same pipeline with a real source, a remote destination and
encryption. If you started the demo profile from `install.md`, the PostgreSQL server and the
MinIO endpoint are already running.

### 1. Check the tools

```bash
backvault check
```

`pg_dump` must be listed as available. If it is missing, install the PostgreSQL client
package on the Backvault host, then check the version: `pg_dump` has to be at least as new as
the server you are dumping. See `sources/postgres.md`.

### 2. Add the destination

For S3 compatible storage, go to Destinations and create an S3 destination:

- Endpoint: `http://localhost:9000` for the demo MinIO, empty for AWS.
- Region: `us-east-1`.
- Bucket: `backups`, created beforehand.
- Prefix: `backvault`.
- Access key and secret key: `backvault` and `backvault-demo-secret` for the demo.
- Path style: on for MinIO and for most non AWS providers.

For a Hetzner Storage Box, create an SFTP destination instead, with port 23 and a sub
account. `destinations/hetzner-storage-box.md` has the details, and
`destinations/s3-providers.md` covers endpoint, region and path style per provider.

Press Test connection. You should see: a success result. A `SignatureDoesNotMatch` here is
usually a wrong region or path style setting, see `troubleshooting.md`.

### 3. Add the source

Create a PostgreSQL source:

- Host, port, database, user, password: for the demo, `localhost`, `5432`, `demo`,
  `postgres`, `backvault-demo-secret`.
- Format: custom. That produces a `.dump` file that `pg_restore` can restore selectively.
- Leave the rest at the defaults.

Press Test connection. You should see: a success result with a duration. The test runs
`select 1` through `psql`, and falls back to `pg_isready` when `psql` is not installed on the
Backvault host.

### 4. Create the job with encryption

Create a job named `Shop database`:

- Source: the PostgreSQL source. Destinations: the S3 destination, and optionally the local
  directory as a second copy.
- Schedule: `0 3 * * *` in your timezone.
- Pack: compression `zstd`, encryption `age`, and a passphrase. Use a long random passphrase
  from a password manager and store it there before you save. Backvault encrypts the passphrase
  with the master key and never shows it again through the API.
- Retention: keep last 14, keep daily 14, keep monthly 12, keep yearly 3.
- Advanced: enable Verify after upload, so every run checks that the object really is on the
  destination with the right size.

Save and press Run now.

You should see: stages `prepare`, `dump`, `pack`, `upload:...`, `verify`, `retention` and
`notify`, and an artifact named like
`shop-database-20260917-030000.dump.zst.age`. The suffixes tell you the whole story: a
PostgreSQL custom format dump, compressed with zstd, encrypted with age.

### 5. Prove you can read it back

Download the artifact. Backvault decrypts it with the job passphrase and decompresses it, so
what lands is a plain `.dump`. Restore it into a scratch database:

```bash
createdb -h localhost -U postgres restore_check
pg_restore -h localhost -U postgres -d restore_check shop-database-20260917-030000.dump
psql -h localhost -U postgres -d restore_check -c '\dt'
```

You should see: your tables.

Now do it the hard way, without Backvault, because that is the case that matters when the
Backvault host is the thing that burned down:

```bash
age --decrypt -o shop.dump.zst shop-database-20260917-030000.dump.zst.age
zstd -d shop.dump.zst -o shop.dump
pg_restore -h localhost -U postgres -d restore_check shop.dump
```

`age` asks for the passphrase on the terminal. `encryption.md` explains the file naming
conventions and the decryption steps in detail, including the case where the artifact was
produced by the standalone scripts with `openssl` instead of `age`.

Write the passphrase down somewhere that survives the loss of the Backvault host. An artifact
whose passphrase is gone is a file you pay storage for and can never read.

## What to set up next

- **Notifications**: add at least one channel and send a test. Backups that fail silently
  are the reason this product exists. See `notifications.md`.
- **A second destination**: one copy is not a backup. Add a remote destination to the jobs
  that currently write locally, or the other way round. Each destination gets its own
  artifact record, so a partial failure shows as a warning rather than a success.
- **Verify after upload**: turn it on for the jobs that matter. It catches destinations that
  accept an upload and then lose it.
- **Monitoring**: scrape `/metrics` and alert on jobs overdue and on the age of the last
  success per job. See `monitoring.md`.
- **Push jobs**: for hosts that cannot be reached from Backvault, install the standalone
  scripts there and push to an ingest endpoint. See `push-and-ingest.md` and `scripts.md`.
- **Back up Backvault itself**: copy the data directory somewhere else, including the master
  key. See `security.md`.
- **Retention**: read `retention.md` and check that what you configured keeps what you think
  it keeps.
