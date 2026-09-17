# Push and ingest

Some hosts cannot be reached from Backvault: a database behind a NAT, a laptop, a customer's server. A
push job lets that host produce the backup itself and upload it to Backvault over HTTPS.

## When to push

Pull, the normal mode, means Backvault connects to your database or filesystem on a schedule. It is
simpler: one place holds the credentials, one place holds the schedule.

Push is the answer when:

- Backvault cannot open a connection to the source, because of a firewall, NAT or a private network.
- The dump needs a tool or a client version that is not on the Backvault host, for example a
  database whose client is newer than the one Backvault has, or a tool Backvault does not ship at
  all.

- The dump is cheaper to make locally, for example a large `tar` that would otherwise cross the
  network twice.
- Something else already produces the artifact and you want it tracked, alerted on and retained
  alongside everything else.

You give up the schedule: cron on the remote host decides when a backup happens. Backvault compensates
with overdue detection, described at the end of this page.

## 1. Create the push job

A push job has source kind `push`. In the panel, create a source with the driver **Push**, then a
job that uses it, or create both from the job editor.

What the job still controls:

- The destinations. Backvault uploads what you push to every destination on the job.
- Compression and encryption, unless you push already packed data, see `--packed` below.
- Retention, verify, notifications.
- `expectedIntervalMinutes`, how often you promise to push.

What it does not control: the schedule. A push job is never scheduled and never runs on its own.

From the API:

```bash
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Requested-With: backvault" \
  -d '{
        "name": "Shop database (pushed)",
        "slug": "shop-db",
        "sourceId": "01JSRCPUSH00000000000000",
        "destinationIds": ["01JDST0000000000000000000"],
        "compression": "zstd",
        "encryption": "age",
        "encryptionPassphrase": "correct horse battery staple wharf",
        "expectedIntervalMinutes": 1500,
        "retention": {"keepDaily": 14, "keepMonthly": 6}
      }' \
  https://backvault.example.com/api/v1/jobs
```

The slug matters: it is the last path segment of the ingest URL and the directory artifacts land
in. Keep it stable.

## 2. Create an ingest token

Tokens live under **Settings, API tokens**. Give the token the `ingest` scope and restrict it to the
job slugs it may write to. A token restricted to `shop-db` cannot read your artifact list, cannot
start runs and cannot push into any other job, which is exactly what you want in a file on a
database server.

```bash
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Requested-With: backvault" \
  -d '{"name": "db01 ingest", "scopes": ["ingest"], "jobSlugs": ["shop-db"]}' \
  https://backvault.example.com/api/v1/tokens
```

The response is `{"token": {...}, "secret": "bvt_..."}`, and that `secret` is the only time the
value is shown. It is 36 characters, `bvt_` plus 32. Store it on the remote host in a file
with mode 600, owned by the user that runs the backup. See [security.md](security.md).

## 3. Push

Three ways, same endpoint.

### Raw curl

The endpoint is `POST /api/v1/ingest/{jobSlug}`, and the `{jobSlug}` is the job slug, never a job
id.

```bash
curl --fail-with-body -X POST \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H "X-Backvault-Filename: shop-db-20260917-020000.dump" \
  -H "X-Backvault-Sha256: $(sha256sum shop-db.dump | awk '{print $1}')" \
  --upload-file shop-db.dump \
  https://backvault.example.com/api/v1/ingest/shop-db
```

The request is synchronous. Backvault receives the body, packs it, uploads it to every destination,
verifies, applies retention and only then answers. A large upload holds the connection open for the
whole run, which is why the client side timeouts matter more here than anywhere else in the API.

The response is `201` with the finished run and the artifacts it produced:

```json
{
  "run": {
    "id": "01JRUN0000000000000000000",
    "jobSlug": "shop-db",
    "kind": "ingest",
    "trigger": "ingest",
    "status": "success",
    "bytes": 184320,
    "sha256": "5e8c...",
    "filename": "shop-db-20260917-020000.dump.zst.age"
  },
  "artifacts": [
    {
      "id": "01JART00000000000000000",
      "destinationId": "01JDST0000000000000000000",
      "filename": "shop-db-20260917-020000.dump.zst.age",
      "status": "present",
      "size": 184320
    }
  ]
}
```

Both objects are the full run and artifact records, trimmed here. The run id is at `run.id`, which
is what the CLI and the script print.

Headers the endpoint understands:

| Header | Required | Meaning |
|---|---|---|
| `Authorization: Bearer <token>` | yes | A token with the `ingest` scope, allowed for this slug |
| `X-Backvault-Filename` | no | The original filename. Backvault derives the extension, and with `?packed=1` the compression and encryption, from it |
| `X-Backvault-Sha256` | no | Lowercase hex sha256 of the body. Verified while spooling, a mismatch fails the run |
| `Content-Length` | no | Chunked uploads work, the sizes are computed while spooling. Send a real length anyway, proxies are happier with it |

Both `X-Backvault-*` names are exact, and both values are trimmed. Send `X-Backvault-Filename`: without it
Backvault cannot tell a `.dump` from a `.tar`, the extension falls back to `bin`, and a later restore
has to guess. Send `X-Backvault-Sha256` too, it is the difference between a silently truncated upload
and a failed run you get told about.

The only query parameter is `packed`, described below. There is no size limit at the HTTP layer:
the body is streamed to the spool file, not buffered in memory, and nothing rejects a large upload
with a `413`.

### What the endpoint answers

| Status | Code | When |
|---|---|---|
| `201` | | The run finished and the artifacts were stored |
| `400` | `validation_failed` | No request body |
| `401` | `unauthenticated` | No token, or an unknown or expired one |
| `403` | `forbidden` | The token has no `ingest` scope, or its `jobSlugs` list does not contain this slug |
| `404` | `not_found` | No job with that slug |
| `422` | `validation_failed` | The run was created and the pipeline failed |
| `423` | `locked` | `job is already running` |
| `500` | `internal` | Anything else, before a run existed |

A `422` is the interesting one: the run row exists, it is visible in the panel with its log, and the
message is the run error. Its id is in the error fields, not in a `run` object:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "sha256 mismatch: expected 5e8c..., received 91af...",
    "fields": {"runId": "01JRUN0000000000000000000"}
  }
}
```

So a client that wants the run id on a failure reads `error.fields.runId`. Note the literal code
string is `validation_failed`, and that `409` and `413` are never returned by this endpoint: a
concurrent push is `423`, not `409`.

### The CLI

```bash
backvault push --job shop-db --file shop-db.dump --sha256
pg_dump ... | backvault push --job shop-db --name shop-db-20260917-020000.dump --sha256
```

It reads `BACKVAULT_URL` and `BACKVAULT_TOKEN` from the environment and streams the file rather than
buffering it. `--sha256` is a switch: it hashes the payload and sends `X-Backvault-Sha256`. Without
`--file` the stream is spooled to a temporary file first, so the length and the checksum can be
computed, and the default name becomes `stdin.bin`, which is why `--name` matters there.

It retries `423` and `5xx` responses, three times by default with a delay that starts at 5 seconds,
doubles per attempt and is capped at 5 minutes. Any other `4xx` is fatal on the first attempt. The
run id is printed on stdout. See [cli.md](cli.md).

### The script

`scripts/backvault-push.sh` needs nothing but bash and curl, which is the point: it runs on a host that
has no Go toolchain and no Backvault binary. Copy the `scripts/` directory to the host, or only
`backvault-push.sh` and `lib/common.sh`.

```bash
export BACKVAULT_URL=https://backvault.example.com
export BACKVAULT_TOKEN=bvt_...
scripts/backvault-push.sh --job shop-db --file shop-db.dump
```

Verified behaviour, tested against an ingest server that checks every one of these:

- Sends `Authorization: Bearer`, `X-Backvault-Filename` and `X-Backvault-Sha256`. The checksum is computed
  with `sha256sum`, or `shasum -a 256`, or `openssl dgst -sha256`, whichever is present.
- Streams the body with a correct `Content-Length`. A 200 MB file is not read into memory.
- Never puts the token on a command line. It goes to curl through a header file with mode 600, so
  it does not appear in `ps` output.
- Adds `?packed=1` when you pass `--packed`.
- Retries five times by default, with a backoff that starts at 5 seconds and doubles per attempt,
  capped at 600 seconds. It retries connection errors and HTTP 5xx, 408 and 429.
- Gives up immediately on any other 4xx, exiting 8, and prints the `error.message` from the
  response. A bad token fails in one attempt rather than five. This includes `423`, so a push that
  collides with a running job is not retried by the script, unlike `backvault push`, which does retry
  it.
- Exits 7 when the retries are exhausted.
- Prints the run id on stdout, and nothing else on stdout. All logs are timestamped on stderr, so
  `RUN=$(backvault-push.sh ...)` gives you the run id.
- Spools stdin to a temporary file when `--file` is absent, so the checksum and the length can be
  computed. The temp file is removed by a trap, on success, on failure and on interrupt.
- `--dry-run` prints the endpoint, the headers and the length, and sends nothing. The token is shown
  as `<token hidden>`.

Options:

| Option | Default | Meaning |
|---|---|---|
| `--job SLUG` | `$BACKVAULT_JOB` | Target job |
| `--file PATH` | stdin | File to upload |
| `--name FILENAME` | basename of the file | Value of `X-Backvault-Filename` |
| `--url URL` | `$BACKVAULT_URL` | Base URL, no trailing slash |
| `--token TOKEN` | `$BACKVAULT_TOKEN` | API token |
| `--token-file FILE` | `$BACKVAULT_TOKEN_FILE` | Read the token from a file |
| `--packed` | off | The file is already compressed and encrypted |
| `--sha256 HEX` | computed | Use a checksum you already have |
| `--no-sha256` | off | Do not send the checksum header |
| `--retries N` | 5 | Attempts after the first |
| `--retry-delay SECONDS` | 5 | Initial backoff |
| `--timeout SECONDS` | 3600 | curl max time per attempt |
| `--dry-run` | off | Print the plan, send nothing |

Full reference in [scripts.md](scripts.md).

Pushing from stdin, with a name so the extension is right:

```bash
pg_dump --format=custom --dbname shop \
  | zstd -q -c \
  | scripts/backvault-push.sh --job shop-db \
      --name "shop-db-$(date -u +%Y%m%d-%H%M%S).dump.zst" --packed
```

Without `--name`, a stdin push through the script gets `<job>-<timestamp>.bin` and a warning,
because Backvault has nothing to derive an extension from. `backvault push` sends `stdin.bin` in the same
situation, with the same result: the stored artifact ends in `.bin`.

## Checksums

`X-Backvault-Sha256` is optional, and checked when present. The value is trimmed and lowercased, and
what it is compared against depends on `packed`:

- Without `packed`, it is the sha256 of the raw request body, the bytes you sent, before Backvault
  compresses or encrypts anything. Hash the file you are uploading.
- With `packed=1`, it is the sha256 of the spooled file, which is byte for byte what you sent,
  because a packed push is stored verbatim. In practice this is the same number.

Either way, hash the thing you hand to curl. A mismatch fails the run at the receive stage with:

```text
sha256 mismatch: expected 5e8c..., received 91af...
```

and the request answers `422` with that message and the run id in `error.fields.runId`. Nothing is
uploaded to any destination.

## `--packed` and what Backvault infers

By default, Backvault applies the job's compression and encryption to whatever you send. Send a plain
`pg_dump` file and the job packs it.

`?packed=1`, which `--packed` sets, says the client already did that work. Backvault then packs nothing
of its own, stores the bytes exactly as they arrived, and reads the filename suffixes to record what
was done. The parameter is read loosely: `1`, `true`, `yes` and `on` all mean packed, and anything else,
including `?packed=0` and `?packed`, means not packed.

| Filename you send | Recorded compression | Recorded encryption |
|---|---|---|
| `shop-db-20260917-020000.dump` | none | none |
| `shop-db-20260917-020000.dump.zst` | zstd | none |
| `shop-db-20260917-020000.sql.gz` | gzip | none |
| `shop-db-20260917-020000.dump.zst.age` | zstd | age |

This is why the filename matters with `--packed`: it is the only record of how to unpack the
artifact later. Get it wrong and a restore tries to decompress something that is not compressed.

Use `--packed` when the remote host compresses or encrypts, which is usually the right choice:
compression on the source host means less data crosses the network, and encrypting there means the
plaintext never reaches Backvault at all. If you encrypt with age, give the job the same passphrase so
Backvault can still unpack downloads and restores.

Do not use `--packed` for an openssl encrypted `.enc` file if you want Backvault to be able to unpack
it. Backvault only understands age, see [encryption.md](encryption.md).

The backup scripts handle all of this: `--to backvault` calls `backvault-push.sh --packed` with the right
filename, because they did the compression and encryption themselves.

## A complete host setup

A database server that dumps at 02:00, compresses, encrypts, and pushes.

### The environment file

Copy `scripts/backvault-agent.env.example` to `/etc/backvault/agent.env` and fill it in. It carries no
comments on purpose, every key is documented in [configuration.md](configuration.md) and
[scripts.md](scripts.md).

```ini
BACKVAULT_URL=https://backvault.example.com
BACKVAULT_TOKEN=bvt_replace_me
BACKVAULT_JOB=shop-db
BACKVAULT_TO=backvault
BACKVAULT_COMPRESS=zstd
BACKVAULT_ENCRYPT=age
BACKVAULT_PASSPHRASE_FILE=/etc/backvault/passphrase
BACKVAULT_TMPDIR=/var/tmp
PGHOST=127.0.0.1
PGPORT=5432
PGUSER=backup
PGDATABASE=shop
```

```bash
install -d -m 0750 -o root -g root /etc/backvault
install -m 0600 /dev/null /etc/backvault/agent.env
install -m 0600 /dev/null /etc/backvault/passphrase
```

Both files hold credentials. Mode 600, and the scripts warn if they are readable by anyone else.

### Test it by hand first

```bash
set -a; . /etc/backvault/agent.env; set +a
scripts/backup-postgres.sh --password-file /etc/backvault/pgpassword --dry-run
```

The dry run prints the exact `pg_dump` command, the artifact name, and where it would go, without
touching the database. Then run it for real and watch the run appear in the panel.

### Install the cron entry

```bash
scripts/install-cron.sh \
  --script backup-postgres.sh \
  --preset daily \
  --env-file /etc/backvault/agent.env \
  --args "--password-file /etc/backvault/pgpassword" \
  --tag shop-db \
  --log-file /var/log/backvault/shop-db.log
```

It writes one line into the crontab of the current user:

```text
0 2 * * * BACKVAULT_CRON_TAG=shop-db . /etc/backvault/agent.env && /opt/backvault/scripts/backup-postgres.sh --password-file /etc/backvault/pgpassword >> /var/log/backvault/shop-db.log 2>&1
```

The `BACKVAULT_CRON_TAG` marker is how the helper finds its own entries again:

```bash
scripts/install-cron.sh --list
scripts/install-cron.sh --remove shop-db
```

Installing with an existing tag replaces that entry rather than adding a second one. `--dry-run`
prints the line without touching the crontab, and without `--yes` it asks before writing.

Run it with no options to be prompted for the script, the schedule, the environment file and the
arguments.

### Writing the cron line yourself

```text
0 2 * * * . /etc/backvault/agent.env && /opt/backvault/scripts/backup-postgres.sh --password-file /etc/backvault/pgpassword >> /var/log/backvault/shop-db.log 2>&1
```

Things that bite in cron and not in a shell:

- `PATH` is short. Use absolute paths for the scripts and set `PATH` at the top of the crontab if
  `pg_dump` lives somewhere unusual.
- A literal `%` in a cron command means a newline. Escape it as `\%`. A `date +%Y%m%d` inline needs
  `date +\%Y\%m\%d`, which is one good reason to let the scripts build the filename.
- Redirect to a log file, otherwise output goes to local mail nobody reads.
- Cron has no `BACKVAULT_*` variables unless you source the environment file, which is what the
  leading `. /etc/backvault/agent.env &&` does.

More cron examples in [scripts.md](scripts.md).

### Other schedules

Hourly, for a database that changes all day:

```text
0 * * * * . /etc/backvault/agent.env && /opt/backvault/scripts/backup-mysql.sh --database shop --password-file /etc/backvault/mysqlpassword >> /var/log/backvault/shop.log 2>&1
```

A file tree every night, with excludes:

```text
30 1 * * * . /etc/backvault/agent.env && /opt/backvault/scripts/backup-files.sh --path /var/www --exclude '*/cache/*' --exclude '*.log' >> /var/log/backvault/www.log 2>&1
```

A Docker volume, with the container stopped for the duration:

```text
0 3 * * * . /etc/backvault/agent.env && /opt/backvault/scripts/backup-docker-volume.sh --volume pgdata --stop-container postgres >> /var/log/backvault/pgdata.log 2>&1
```

## Overdue detection

A push job has no schedule, so Backvault cannot tell a healthy quiet night from a cron entry that was
deleted six weeks ago. `expectedIntervalMinutes` is how you tell it.

The watcher runs every `settings.overdueCheckMinutes`, 15 by default. A push job is overdue when its
last successful run is older than `expectedIntervalMinutes`. The job is marked overdue, it appears
under problem jobs on the dashboard, and `job.overdue` fires once for the incident, not on every
check. See [notifications.md](notifications.md).

Set the interval to the real interval plus enough slack that a slow night does not page anyone:

| Push schedule | `expectedIntervalMinutes` | Why |
|---|---|---|
| Hourly | 90 | One missed hour is noise, two is a problem |
| Daily at 02:00 | 1500 | 25 hours, tolerates a long dump and a late start |
| Weekly | 10800 | 7.5 days |

Leave it at 0 to disable overdue detection for that job. That is rarely what you want: the main
failure of a push setup is silence, and this is the only thing that notices it.

Verify the detection works. Disable the cron entry, wait for the window to pass, and check that you
get the alert. A monitoring rule you have never seen fire is a monitoring rule you do not have.

## Watching what arrived

Each push creates a run of kind `ingest`. In the panel the job shows it with the artifacts it
produced, the size and the sha256. From the API:

```bash
curl -fsSL -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  "https://backvault.example.com/api/v1/runs?job=shop-db&kind=ingest&limit=5" | jq '.items[] | {id, status, bytes, finishedAt}'
```

An ingest run has no dump stage, because the data arrived already dumped. Its stages are prepare,
receive, pack, upload per destination, verify, retention and notify. With `packed=1` the pack stage
still appears, recorded as `already packed by the client`. See [concepts.md](concepts.md).

The stored filename is built by Backvault, not taken from your header: it is
`<job-slug>-<YYYYMMDD>-<HHMMSS>.<ext>` in UTC, plus the `.zst`, `.gz` and `.age` suffixes for
whatever packing applies, and the object lands under `<job-slug>/` on every destination.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `401 unauthenticated` | Bad or expired token | Check the token file, create a new token |
| `403 forbidden` | Token lacks `ingest`, or the slug is not in its `jobSlugs` | Reissue with the right scope and slug |
| `404 not_found` | No job with that slug | Check the slug. Any job accepts a push, but the slug must match exactly |
| `423 locked` | The job is already running, a scheduled run or an earlier push | Wait and retry. `backvault push` does this for you |
| `422 validation_failed` | The run started and the pipeline failed | Read `error.message`, then the log of `error.fields.runId` |
| Run fails with a checksum error | The upload was truncated, or the file changed while uploading | Retry. Push a file, not a stream being written |
| Artifact restores as garbage | `--packed` with a filename whose suffixes do not match the content | Name the file after what it really is |
| Nothing arrives, no error | cron entry missing or failing before the push | `scripts/install-cron.sh --list`, read the log file |
| Script exits 8 immediately | A 4xx the script will not retry, the message is on stderr | Read the message, it is the server's `error.message` |

More in [troubleshooting.md](troubleshooting.md).
