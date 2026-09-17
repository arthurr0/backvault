# Troubleshooting

Symptoms, what causes them, and what to change. Each section says how to confirm the cause
before you change anything.

## Quick table

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| Panel loads a blank page | the admin panel was not built into the binary | build `web/dist` and rebuild, or use the release binary |
| Login succeeds then bounces back to the login page | base URL says https, the connection is http, so the `Secure` cookie is never sent back | set `BACKVAULT_BASE_URL` to the real URL |
| Every write from the panel fails, reads work | the proxy strips `X-Requested-With` | let the header through |
| Live log shows nothing until the run ends | the proxy buffers the event stream | `proxy_buffering off`, `flush_interval -1` |
| `POST /api/v1/jobs/{id}/run` returns 423 | the job is already running | wait, or cancel the running run |
| A job sits in queued | all worker slots are busy | raise `maxConcurrentRuns`, or wait |
| Run fails in `dump` with "executable file not found" | the external tool is missing or not on `PATH` | install it, or set `binary_path` |
| `pg_restore` reports `unrecognized configuration parameter "transaction_timeout"` | the dump was made by a newer `pg_dump` than the target server | restore into a matching version, or dump with plain format |
| `pg_dump: error: server version mismatch` | `pg_dump` is older than the server | upgrade the client tools |
| Restore to a path fails with permission denied | `ProtectSystem=strict` in the unit | add the target to `ReadWritePaths` |
| S3 returns `SignatureDoesNotMatch` | wrong region, wrong path style, or host clock skew | check all three, in that order |
| `upload-s3.sh` refuses a file over 5 GiB | the curl uploader does a single PUT | install the `aws` CLI or `mc` |
| SFTP upload fails with permission denied at the top level | the chroot root is owned by root and not writable | upload into a writable subdirectory |
| SFTP fails with a host key error | the key is unknown or changed | use `--known-hosts`, or `--host-key-checking accept-new` |
| SFTP password authentication does nothing | `sshpass` is not installed | install it, or use key authentication |
| A cron backup hangs forever with `age` | `age` wants a passphrase from a terminal | install `util-linux`, or use `--encrypt openssl` |
| The disk fills during a run | the work directory shares a filesystem with the data directory | move `work_dir`, or add space |
| Artifacts turn to status missing after a verify | the object is gone or the size differs | check the destination, then the retention rules on the other side |
| A push job is marked overdue although the script ran | the ingest failed, or `expectedIntervalMinutes` is too tight | read the script log and the run list |
| The artifact timestamp is an hour off | artifact names are UTC, schedules are not | nothing to fix, understand the convention |

## The panel is a blank page

The API works, `GET /api/v1/meta/version` returns JSON, and the browser shows nothing.

Confirm: view source on the page. If you get a minimal placeholder page rather than an
application shell, the binary was built without `web/dist`.

Fix: build the panel and rebuild the binary.

```bash
cd web && npm ci && npm run build
cd .. && make build
```

The release binaries and the container image always contain the panel. This only happens
with a binary built from a checkout where the panel was never built. The server is designed
to start anyway rather than refuse to boot, which is why the failure looks like an empty
page instead of an error.

## Login loops, 401 after a successful login

The login request returns 200, then the next request returns 401 and the panel goes back to
the login screen.

Confirm: open the browser developer tools, look at the `Set-Cookie` response header on the
login request. If it carries `Secure` and the address bar says `http://`, the browser
accepted the response and then refused to store or send the cookie.

Fix: make `BACKVAULT_BASE_URL` match reality. Over plain http for a local test, drop the https
base URL. In production, terminate TLS in the proxy and keep the https base URL. See
`install.md` section 6.

The other cause is a proxy that rewrites or drops cookies. Check `proxy_set_header` blocks
for anything touching `Cookie` or `Set-Cookie`.

## Every write fails, reads work

Reads return 200, every create, update or delete returns 403.

Confirm: reproduce with curl from the proxy's point of view.

```bash
curl -i -X POST https://backvault.example.com/api/v1/jobs/etc-nightly/run \
  -H 'Cookie: backvault_session=...' \
  -H 'X-Requested-With: backvault'
```

If that works and the panel does not, the proxy is stripping the header.

Fix: stop stripping it. In nginx, this happens when a `proxy_set_header` allow list is in
use. Add the header explicitly:

```nginx
proxy_set_header X-Requested-With $http_x_requested_with;
```

## The live log does not stream

The run finishes and then the whole log appears at once, or the dashboard never updates
until a reload.

Confirm:

```bash
curl -N -H 'Authorization: Bearer bvt_...' \
  https://backvault.example.com/api/v1/runs/<run-id>/log/stream
```

Lines appearing as they happen means Backvault is fine and the browser path through the proxy
is not. Nothing until the end means the proxy is buffering.

Fix: `proxy_buffering off`, `proxy_http_version 1.1`, `proxy_set_header Connection ""` and a
`proxy_read_timeout` well above the heartbeat interval in nginx, or `flush_interval -1` in
Caddy. Full configurations are in `install.md` section 6.

## A job stays queued, or a run returns 423

Confirm: look at the Runs list filtered to running. Count them.

- If the count equals `maxConcurrentRuns` (default 2), the queue is working as designed and
  your job is waiting for a slot. Raise the limit in Settings if the host can take the
  parallel load, remembering that each concurrent run spools a full artifact to disk.
- If the job itself already has a run in progress, a second run of the same job is refused
  with HTTP 423 locked. One run per job at a time is a hard rule, so a slow nightly job can
  never overlap itself.
- If a run is stuck in running after a crash, cancel it from the panel or with
  `POST /api/v1/runs/{id}/cancel`. Runs that were in progress when the process died are
  marked failed at the next start.

## A run fails in the dump stage

The log says something like `exec: "pg_dump": executable file not found in $PATH`.

Confirm:

```bash
backvault check
sudo -u backvault env | grep -i path
sudo -u backvault which pg_dump
```

Under systemd the service does not inherit your login shell's `PATH`. A tool installed in
`/opt/pgsql/bin` or through a version manager is invisible to it even though it works in
your terminal.

Fixes, in order of preference:

1. Install the distribution package so the tool lands in `/usr/bin`.
2. Set `binary_path` on the source driver to the directory holding the tool. Every driver
   that shells out has this advanced field.
3. Extend `PATH` for the service:

   ```bash
   sudo systemctl edit backvault
   ```

   ```ini
   [Service]
   Environment=PATH=/opt/pgsql/bin:/usr/local/bin:/usr/bin:/bin
   ```

In the container image the tools are already installed, so this failure there means you are
using a driver whose tool is not in the image. Check the package list in `deploy/Dockerfile`.

## PostgreSQL version mismatch

Two different failures, in opposite directions.

### Restoring a newer dump into an older server

```text
pg_restore: error: could not execute query: ERROR:  unrecognized configuration parameter "transaction_timeout"
Command was: SET transaction_timeout = 0;
pg_restore: warning: errors ignored on restore: 1
```

This is a dump produced by `pg_dump` 17 or newer being restored into a PostgreSQL 16 server.
The newer client writes a `SET transaction_timeout` line that the older server does not
know. With a custom format dump `pg_restore` reports it, ignores it and continues, so the
data does arrive. Confirm by counting rows in the restored database before you panic.

Fix: restore into a server at least as new as the client that made the dump. If you have to
go backwards, dump with the plain format and strip the offending line, or install a
`pg_dump` matching the target server and take a fresh dump.

### Dumping a newer server with an older client

```text
pg_dump: error: server version: 16.14; pg_dump version: 15.6
pg_dump: error: aborting because of server version mismatch
```

`pg_dump` refuses to dump a server newer than itself, and there is no override. This is the
one that silently breaks backups after a database upgrade: the server moves to a new major
version, the client on the backup host stays where it was, and every run fails from then on.

Fix: upgrade the PostgreSQL client package on the Backvault host so it is at least as new as
the newest server you back up. Client tools are backwards compatible, so a single recent
client handles older servers too. The container image ships the PostgreSQL 18 client from
the PGDG repository, so it dumps servers from 9.2 through 18 and this failure only happens
on a host where you installed the client package yourself.

See `sources/postgres.md`.

## Restore to a path fails with permission denied

The restore run fails writing the target file, and the path is outside the data directory.

Confirm:

```bash
systemctl show backvault -p ReadWritePaths -p ProtectSystem -p ProtectHome
```

`ProtectSystem=strict` makes the whole filesystem read only except the paths listed in
`ReadWritePaths`. The shipped unit lists only `/var/lib/backvault`.

Fix: add the target.

```bash
sudo systemctl edit backvault
```

```ini
[Service]
ReadWritePaths=/srv/restore
```

```bash
sudo systemctl daemon-reload
sudo systemctl restart backvault
```

The same applies to reading. A `files` source cannot read `/home` while `ProtectHome=yes` is
set. Use `ProtectHome=read-only` for that case rather than turning it off.

## S3 returns SignatureDoesNotMatch

Check these three, in this order, because that is how often they are the cause.

1. **Region.** The signature covers the region string. A bucket in `eu-central-1` signed for
   `us-east-1` fails. Some providers want a specific region name, some accept anything. See
   `destinations/s3-providers.md`.
2. **Path style.** MinIO and most non AWS providers need path style addressing
   (`https://endpoint/bucket/key`). AWS uses virtual host style
   (`https://bucket.endpoint/key`). The host name is part of the signature, so getting this
   wrong produces a signature error rather than a clear message.
3. **Clock skew.** Signature version 4 rejects requests whose timestamp is more than fifteen
   minutes from the server's clock.

   ```bash
   timedatectl status
   sudo systemctl restart systemd-timesyncd
   ```

A trailing slash or a double slash in the prefix also changes the signed path. Set the
prefix without leading or trailing slashes.

## upload-s3.sh refuses a large file

```text
ERROR upload-s3.sh: the curl uploader does a single PUT and cannot upload more than 5 GiB, install the aws CLI or mc
```

The pure curl uploader in `scripts/upload-s3.sh` signs and sends one PUT request, and a
single S3 PUT is limited to 5 GiB by the protocol. Anything larger needs multipart upload.

Fix, in order: install the `aws` CLI or `mc` on that host, which the script prefers
automatically when present. Failing that, split the backup into several smaller jobs. Failing
that, push to Backvault instead and let the S3 destination driver handle the upload, because it
uses multipart with 16 MiB parts and has no such limit.

The limit only applies to the standalone script. Backvault's own S3 destination is not affected.

## SFTP permission denied writing at the top level

```text
sftp> -mkdir 'upload'
remote mkdir "/upload": Permission denied
dest open "/upload/db-20260917-020000.dump.zst.partial": No such file or directory
```

On an SFTP only server the login is chrooted, and the chroot directory itself has to be owned
by root and not writable by the user, otherwise OpenSSH refuses the session entirely. That
means you cannot create anything directly in the root of the chroot. This was confirmed
against `atmoz/sftp`, and it is exactly how a Hetzner Storage Box and most managed SFTP
services behave.

Fix: upload into a subdirectory that the account can write to. With `atmoz/sftp` that
directory is created for you by naming it in the user specification:

```bash
docker run -d -p 2222:22 -v /srv/keys/id.pub:/home/backvault/.ssh/keys/id.pub:ro \
  atmoz/sftp backvault::1001::upload
```

Then set the base path to `upload`, or `upload/db-prod`:

```bash
upload-sftp.sh --file dump.zst --base-path 'upload/db-prod' --host sftp.example.com --user backvault --key /etc/backvault/sftp_key
```

Nested directories under a writable base are created automatically, including names with
spaces.

## SFTP host key verification

```text
Host key verification failed.
```

The host is not in the known hosts file, or its key changed.

Confirm and fix for a first connection:

```bash
ssh-keyscan -p 23 -H u123456.your-storagebox.de > /etc/backvault/known_hosts
chmod 600 /etc/backvault/known_hosts
upload-sftp.sh --known-hosts /etc/backvault/known_hosts ...
```

`--host-key-checking accept-new` records the key on first contact and refuses it if it ever
changes afterwards, which is a reasonable middle ground for automation.
The value `no` accepts anything, removes the protection against a man in the middle, and
makes the script log a warning. Do not leave it on.

If the key changed and you did not expect it, do not clear the entry until you know why.

## SFTP password authentication does nothing

```text
ERROR upload-sftp.sh: password authentication needs sshpass, install it or use --key
```

OpenSSH deliberately refuses to read a password from a pipe, so the script needs `sshpass`
to drive it. Either install it (`apt install sshpass`, `dnf install sshpass`), or switch to
key authentication, which is better anyway:

```bash
ssh-keygen -t ed25519 -f /etc/backvault/sftp_key -N ''
chmod 600 /etc/backvault/sftp_key
ssh-copy-id -i /etc/backvault/sftp_key.pub -p 23 u123456@u123456.your-storagebox.de
```

A Storage Box has its own key upload procedure, see
`destinations/hetzner-storage-box.md`.

## A cron backup hangs with age

The job never finishes and the log stops after the pack stage.

Cause: the `age` command line tool reads a passphrase from the terminal device, not from
standard input, by design. Under cron there is no terminal, so it waits forever or fails.

Confirm: run the same command by hand with `< /dev/null` and watch it fail or block.

Fixes:

1. Install `util-linux`, which provides `script`. The backup scripts use it to give `age` a
   pseudo terminal, and then passphrase encryption works unattended.
2. Use `--encrypt openssl` instead, which reads the passphrase from a file natively. The
   artifact is then named `.enc` rather than `.age`, and is decrypted with `openssl enc -d`.
3. Let Backvault do the encryption instead of the script. Backvault uses the age library directly
   and never needs a terminal. Push the artifact unencrypted over https to an ingest endpoint
   without `--packed`, and let the job's pack settings apply.

`encryption.md` covers the trade offs and the decryption commands for both formats.

## The disk fills during a run

The run fails with no space left on device, and the data directory is on the same filesystem
as everything else.

Cause: the engine spools the complete packed artifact to `work_dir` before uploading. A
40 GB dump needs 40 GB of temporary space, minus compression, and two concurrent runs need
both at the same time.

Confirm:

```bash
df -h /var/lib/backvault
du -sh /var/lib/backvault/work/*
```

Fixes: point `work_dir` at a filesystem with room, keep `maxConcurrentRuns` at a level the
disk can support, and remember that a local destination stores artifacts on that filesystem
too. The work directory is cleaned at the end of every run, on success and on failure, so
leftovers there mean a process was killed, and they are safe to delete while no run is
active.

```yaml
data_dir: /var/lib/backvault
work_dir: /mnt/scratch/backvault-work
```

## Artifacts turn to missing after a verify

The verify stage stats each object on its destination and compares the size. A mismatch or a
missing object marks the artifact `missing` and the run `warning`.

Confirm by listing the destination yourself, through the Browse view or with the provider's
own tools. Then work out who deleted it:

- A lifecycle rule on the bucket, which is the most common answer. An S3 lifecycle policy
  that expires objects after 30 days deletes artifacts Backvault still counts on.
- A second Backvault instance or an old cron script pruning the same prefix.
- A pruning script with a prefix that matches more than it should, see `scripts.md`.

Decide who owns retention for that prefix, Backvault or the provider, and turn the other one
off. Two systems pruning the same directory will always look like corruption.

## A push job is overdue although the script ran

Backvault marks a push job overdue when its last successful run is older than
`expectedIntervalMinutes`.

Confirm, on the host that pushes:

```bash
grep backvault /var/log/syslog
/opt/backvault/scripts/backvault-push.sh --job db-prod --file /tmp/test.dump --dry-run
```

Then check the exit code of the last real run. `backvault-push.sh` exits 8 when the server
rejected the upload (wrong token, unknown job slug, a token not allowed for that slug) and 7
when it gave up after its retries. Both of those leave cron silent unless the entry captures
the output, which is why the cron entry written by `install-cron.sh` supports a log file.

The other cause is an interval that does not match the schedule. A job that runs daily at
02:00 and has `expectedIntervalMinutes` set to 1440 goes overdue on any delay at all. Allow
headroom, for example 1560 minutes for a daily job.

See `push-and-ingest.md`.

## The artifact timestamp looks wrong

A job scheduled at 02:00 in Europe/Warsaw produces
`etc-nightly-20260917-000000.tar.zst`.

This is correct. Schedules run in the job's timezone, and artifact filenames are always UTC,
so the two differ by the offset. The convention keeps names sortable and unambiguous across
daylight saving changes, which is what the pruning scripts depend on. The panel shows local
times with the absolute value in a tooltip. See `concepts.md`.

## Getting more detail

```bash
BACKVAULT_LOG_LEVEL=debug
```

Set it, restart, reproduce, set it back. Debug level logs every driver call and every HTTP
request. For the standalone scripts, use `--debug`, which prints the resolved command lines
and the request targets to stderr without ever printing a secret.

If you are reporting a problem, include the output of `backvault version`, `backvault check`, the
run log, and the relevant section of your reverse proxy configuration.
