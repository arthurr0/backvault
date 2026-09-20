# CLI reference

The `backvault` binary is both the server and the client: it runs the service, and it talks to a
running service over the API for everything an operator does from a terminal.

## Two kinds of commands

**Server and recovery commands** work on the data directory directly and do not need a running
server. They need read and write access to the SQLite database and to the master key, which means
they must run as the service user (or as root) on the machine that holds the data.

| Command | Why it touches the database |
|---|---|
| `backvault serve` | it is the server |
| `backvault user` | create or repair an account when nobody can log in |
| `backvault token` | mint a token when no admin session is available |
| `backvault check` | inspects the local configuration and the tools on this host |

`backvault user` and `backvault token` open the SQLite database themselves and never call the API. They
ignore `BACKVAULT_URL` and `BACKVAULT_TOKEN` entirely.

**Client commands** call the HTTP API and work from any machine. They read `BACKVAULT_URL` and
`BACKVAULT_TOKEN` from the environment. `BACKVAULT_URL` defaults to `http://localhost:8080`, so a command
run on the server host with no environment at all still reaches a local instance.

| Command | Endpoint it uses |
|---|---|
| `backvault run` | `POST /jobs/{slug}/run`, then `GET /runs/{id}` while waiting |
| `backvault jobs` | `GET /jobs`, `GET /jobs/{slug}`, `POST /jobs/{slug}/enable` and `/disable` |
| `backvault runs` | `GET /runs`, `GET /runs/{id}/log`, `GET /runs/{id}` |
| `backvault hosts` | `GET /hosts`, `GET /hosts/{id}`, `POST /hosts/{id}/test` |
| `backvault restore` | `GET /artifacts/{id}/download` |
| `backvault push` | `POST /ingest/{slug}` |
| `backvault export` and `backvault import` | `GET /export`, `POST /import` |

Every client command sends `Authorization: Bearer $BACKVAULT_TOKEN` and prefixes the path with
`/api/v1`. The HTTP client has a five minute timeout per request, which is the practical ceiling for
a single `backvault push` of a very large file over a slow link.

## Persistent flags

```text
--config FILE     path to the configuration file
--data-dir DIR    data directory
```

Both are accepted on every command. They only mean something to the commands that read the local
configuration or the database: `serve`, `check`, `user` and `token`.

## Environment variables

| Variable | Used by | Meaning |
|---|---|---|
| `BACKVAULT_URL` | client commands | base URL of the server, default `http://localhost:8080` |
| `BACKVAULT_TOKEN` | client commands | API token with the scopes the command needs |
| `BACKVAULT_PASSWORD` | `user create`, `user password` | password to use instead of prompting |
| `BACKVAULT_CONFIG` | `serve`, `check`, `user`, `token` | path to the YAML config file |
| `BACKVAULT_DATA_DIR` | server and recovery commands | data directory, default `./data` |
| `BACKVAULT_WORK_DIR` | `serve` | spool directory, default `<dataDir>/work` |
| `BACKVAULT_LISTEN` | `serve` | listen address, default `:8080` |
| `BACKVAULT_BASE_URL` | `serve` | public URL, used in notification links and for the cookie `Secure` flag |
| `BACKVAULT_MASTER_KEY` | server and recovery commands | 32 byte key, hex or base64 |
| `BACKVAULT_MASTER_KEY_FILE` | server and recovery commands | file holding the key, default `<dataDir>/master.key` |
| `BACKVAULT_LOG_LEVEL` | `serve` | `debug`, `info`, `warn` or `error` |
| `BACKVAULT_LOG_FORMAT` | `serve` | `text` or `json` |
| `BACKVAULT_ADMIN_EMAIL`, `BACKVAULT_ADMIN_PASSWORD` | `serve` | bootstrap an admin on first start when no users exist |
| `BACKVAULT_TRUSTED_PROXIES` | `serve` | comma separated proxy CIDRs whose forwarding headers are believed |
| `BACKVAULT_METRICS_TOKEN` | `serve` | when set, `/metrics` requires this token |

Full details of each in [configuration.md](configuration.md).

## backvault serve

Runs the HTTP server, the scheduler, the run queue and the overdue watcher.

```text
backvault serve [--config FILE] [--listen ADDR] [--data-dir DIR] [--work-dir DIR]
             [--base-url URL] [--log-level LEVEL] [--log-format FORMAT]
```

Flags override the environment, which overrides the config file, which overrides the built in
defaults. See [configuration.md](configuration.md) for the exact precedence.

```bash
backvault serve --config /etc/backvault/backvault.yaml
backvault serve --data-dir ./data --listen 127.0.0.1:8080 --log-level debug
```

On a first start with an empty database, the server prints the setup URL. If
`BACKVAULT_ADMIN_EMAIL` and `BACKVAULT_ADMIN_PASSWORD` are set, it creates that admin instead, which is
how unattended deployments avoid an open setup page.

`SIGTERM` starts a graceful shutdown: no new runs are accepted, running ones get their context
canceled, and the process exits once the spool directories are cleaned. See [install.md](install.md)
for the systemd unit.

## backvault check

Reports what this host can do, without contacting a server. It takes no flags of its own, only the
persistent `--config` and `--data-dir`.

```bash
backvault check
```

```text
Configuration
  config file          /etc/backvault/backvault.yaml
  listen               :8080
  data dir             /var/lib/backvault
  work dir             /var/lib/backvault/work
  database             /var/lib/backvault/backvault.db
  master key file      /var/lib/backvault/master.key
  master key from env  no
  base url             https://backvault.example.com
  log level            info
  log format           json
  trusted proxies      (none)
  metrics token        yes
  bootstrap admin      (none)

External tools
  TOOL        STATUS   USED BY   PATH                 VERSION
  pg_dump     ok       postgres  /usr/bin/pg_dump     16.14
  pg_restore  ok       postgres  /usr/bin/pg_restore  16.14
  mysqldump   missing  mysql     -                    -
  mongodump   ok       mongodb   /usr/bin/mongodump   100.9.4
  redis-cli   ok       redis     /usr/bin/redis-cli   7.2.9
  sqlite3     ok       sqlite    /usr/bin/sqlite3     3.45.3
  docker      missing  docker    -                    -

5 tool(s) available, 2 missing, 0 configuration problem(s)
```

It prints the configuration summary first, then any configuration, directory or master key problem
it found, then the tool table. Run it after an upgrade of the host, and after adding a driver that
shells out to a tool. It exits 1 when it reports configuration problems, and 0 when only optional
tools are missing.

## backvault version

Prints the version, the commit, the build date and the Go version, the same values
`GET /meta/version` returns.

```text
backvault version [--json]
```

```bash
backvault version
backvault version --json
```

## backvault run

Queues a backup run for a job and, with `--wait`, blocks until it finishes.

```text
backvault run <job-slug> [--wait]
```

```bash
BACKVAULT_URL=https://backvault.example.com BACKVAULT_TOKEN=$TOKEN backvault run db-prod --wait
```

The token needs the `run` scope. The run id is printed on stdout in both cases. Without `--wait` the
command exits as soon as the run is queued. With `--wait` it polls `GET /runs/{id}` every two
seconds until the run reaches a terminal status, prints `status: <status>` and the run error if
there is one, and exits 1 when the run failed, which makes it usable from another scheduler.

Returns exit code 5 when the job is already running (`423` from the API).

## backvault jobs

```text
backvault jobs list
backvault jobs show <job-slug>
backvault jobs enable <job-slug>
backvault jobs disable <job-slug>
```

```bash
backvault jobs list
backvault jobs show db-prod
```

`list` takes no flags. It asks for up to 500 jobs and prints a table of slug, name, enabled,
schedule, last run status and next run. Filtering is a job for the panel or for `jq` over
`GET /jobs`. `show` prints the job as indented JSON with secrets masked. `enable` and `disable`
need the `admin` scope and print the new state.

## backvault runs

```text
backvault runs list [--job SLUG] [--status STATUS] [--limit N]
backvault runs log <run-id> [--follow|-f]
```

```bash
backvault runs list --job db-prod --status failed
backvault runs log 01JQ2K9F7A0000000000000100 --follow
```

`--limit` defaults to 50. There is no `--kind` flag; filter by kind through the API if you need it.
`--follow` re-reads `GET /runs/{id}/log` from the last byte offset once a second and stops when the
run reaches a terminal status, so it works through proxies that do not like long lived connections.

## backvault hosts

```text
backvault hosts list [--q TEXT]
backvault hosts show <name-or-id>
backvault hosts test <name-or-id>
```

```bash
backvault hosts list
backvault hosts test edge-01
```

`list` needs the `read` scope and prints one line per host with its address, user, auth method, how
many sources run on it, the outcome of its last test and the tools that test found:

```text
NAME     ADDRESS          USER    AUTH  SOURCES  LAST TEST                TOOLS
edge-01  10.0.0.12:22     backup  key   4        ok 2026-09-20T14:06:55Z  tar sqlite3 gzip sudo
```

`--q` filters by name, description or address. `show` prints one host as JSON with its secrets
masked, and accepts a name as well as an id.

`test` needs the `admin` scope, opens the connection now and records the result on the host:

```text
ok: true
message: connection successful
os: Linux 6.8.0-40-generic x86_64
tools: tar sqlite3 gzip sudo
duration: 71ms
```

An unreachable host prints the same block with `ok: false` and the reason in `message`, and exits
non-zero, which makes it usable in a monitoring check. Creating and editing hosts is done in the
panel or through the [API](api.md), because that is where key generation lives.

## backvault restore

Downloads an artifact through the API and writes it to a path.

```text
backvault restore <artifact-id> --to <path> [--raw] [--passphrase PASSPHRASE]
```

```bash
backvault restore 01JQ2K9F7A0000000000000200 --to ./shop.dump
backvault restore 01JQ2K9F7A0000000000000200 --to ./shop.dump.zst.age --raw
```

`--to` is required. When it points at an existing directory, the filename comes from the
`Content-Disposition` header of the response. The file is written to `<target>.partial` and renamed
once the transfer completed, so a truncated download never looks like a finished one.

By default the artifact is decrypted and decompressed on the way through, so what lands on disk is
the dump the source produced. `--raw` writes the stored bytes as they are. `--passphrase` overrides
the job passphrase for artifacts encrypted with an older one. Restoring into a database or into a
path on the server is a different operation, see [restore.md](restore.md).

`--passphrase` travels as a query parameter and sits in the process list of that host, so prefer
`--raw` plus a local `age --decrypt` when the passphrase is not already on the machine.

## backvault push

Uploads a file or a stream to a push job, the same contract as
[scripts/backvault-push.sh](scripts.md).

```text
backvault push --job <slug> [--file PATH] [--name FILENAME] [--packed] [--sha256]
            [--retries N] [--retry-delay DURATION]
```

```bash
backvault push --job web-files --file /tmp/web01.tar.zst --packed --sha256
pg_dump shop | zstd | backvault push --job db-prod --name shop-20260314-020000.sql.zst --packed
```

`--job` is required. With no `--file`, or with `--file -`, the body is read from standard input and
spooled to a temporary file so the checksum and the length can be computed, and the default name
becomes `stdin.bin`. With a `--file` and no `--name`, the basename of the file is sent.

`--sha256` is a switch, not a value: it tells the CLI to hash the payload itself and send the result
in `X-Backvault-Sha256`. `--packed` sets `?packed=1`, meaning the file is already compressed and
encrypted and Backvault should store it as is. The run id is printed on stdout.

`--retries` defaults to 3 and `--retry-delay` to 5s. Only `423` and `5xx` responses are retried,
along with transport errors; any other `4xx` is fatal on the first attempt, so a bad token or an
unknown slug fails immediately instead of four times. The delay doubles per attempt and is capped at
5 minutes.

The token needs the `ingest` scope, and the job slug must be allowed by the token restriction.
See [push-and-ingest.md](push-and-ingest.md).

## backvault user

Direct database access, for bootstrap and for the day nobody can log in. The server may be stopped.

```text
backvault user create --email EMAIL [--name NAME] [--role admin|viewer] [--password PASSWORD]
backvault user list
backvault user password --email EMAIL [--password PASSWORD]
```

```bash
sudo -u backvault backvault user create --email ada@example.com --name 'Ada Lovelace' --role admin
sudo -u backvault backvault user password --email ada@example.com
```

`--role` defaults to `admin`. With no `--password` the command falls back to `BACKVAULT_PASSWORD`, and
without that it prompts twice on the terminal without echoing, which keeps the password out of the
shell history and the process list. When stdin is not a terminal it reads one line instead, so
`echo 'secret' | backvault user password --email ...` works in a provisioning script. Passwords must be
between 10 and 512 characters. Setting a password also deletes that user's active sessions.

Both subcommands need `BACKVAULT_DATA_DIR` (or `--data-dir`) to point at the real data directory, and
the master key must be readable.

## backvault token

Also direct database access, for the case where you need an ingest token before anyone has logged
in.

```text
backvault token create --name NAME [--scopes SCOPE[,SCOPE]] [--jobs SLUG[,SLUG]] [--expires RFC3339]
backvault token list
backvault token delete --id TOKEN_ID
```

```bash
sudo -u backvault backvault token create --name 'web01 push' --scopes ingest --jobs web-files
sudo -u backvault backvault token list
```

`--scopes` defaults to `read`. Scopes are `admin`, `read`, `run` and `ingest`. `--expires` takes an
RFC3339 timestamp, for example `2027-01-01T00:00:00Z`, not a duration. The secret is printed on
stdout and is not recoverable afterwards; the confirmation line goes to stderr, so
`TOKEN=$(backvault token create ...)` captures the secret alone.

## backvault export and backvault import

```text
backvault export [--include-secrets] [--out FILE]
backvault import <file> [--dry-run]
```

```bash
backvault export --out backvault-config.yaml
backvault import backvault-config.yaml --dry-run
backvault import backvault-config.yaml
```

Both go through the API and need the `admin` scope. Without `--out`, the export is written to
stdout. Export masks secrets unless `--include-secrets`, which is recorded in the audit log. Import
matches jobs by slug and everything else by name, then creates or updates, and prints a table of the
changes. `--dry-run` prints the plan and changes nothing.

An export without secrets is a safe thing to keep in a git repository. An export with secrets is as
sensitive as the master key, treat it accordingly, see [security.md](security.md).

## Exit codes

The binary produces six codes and no others. Errors go to stderr as `error: <message>`, and an API
error appends the `error.fields` of the response as `(field: reason, ...)`.

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | everything else, see below |
| 4 | authentication or authorization failure, `401` or `403` from the API |
| 5 | conflict, the job is already running (`423`) or the object is in use (`409`) |
| 6 | not found, no such job, run or artifact (`404`) |
| 8 | the server could not be reached, or it answered `5xx` after the retries |

There is no exit code 2, 3 or 7. Code 1 is the catch all and covers more than an unexpected error:
a usage error, an unknown flag, a missing required flag such as `--to` or `--job`, a configuration
error, a file that cannot be read or written, any API status not listed above (`400`, `422`, `429`
and the rest), `backvault check` reporting configuration problems, and `backvault run --wait` on a run
that ended failed.

Scripts should branch on these rather than on message text.

The shell helpers under `scripts/` are a separate program with a larger scheme of their own: 2 for a
usage error, 3 for a missing dependency, 4 for a configuration error, 5 for a failed dump, 6 for a
failed pack, 7 for a delivery that failed after the retries, 8 for an upload the server rejected and
9 for a failed verification. Do not reuse a table from [scripts.md](scripts.md) for the Go binary, or
the other way round.
