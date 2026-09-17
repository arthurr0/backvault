# Configuration

Backvault reads its startup configuration from a YAML file, the environment and command line flags, and
keeps everything else in the database where the panel can edit it.

## Where settings live

There are two layers, and knowing which is which saves a lot of confusion.

**Startup configuration** decides how the process runs: the listen address, where data lives, how it
logs, and the master key. It is read once at startup from the file, the environment and the flags.
Changing it needs a restart. This page documents it in full.

**Runtime settings** decide how Backvault behaves: the site name, the default retention, how many runs
may execute at once. They live in the database, are edited in the panel under Settings or with
`PUT /settings`, and take effect without a restart. They are listed at the bottom of this page.

## Precedence

Later sources win:

1. built in defaults
2. the YAML config file, if one is given with `--config` or `BACKVAULT_CONFIG`
3. environment variables
4. command line flags

An empty value does not override. Setting `BACKVAULT_LISTEN=` leaves the value from the file in place
rather than clearing it.

The YAML parser rejects unknown keys. A typo such as `data-dir` instead of `data_dir` stops the
server at startup with an error naming the line, rather than silently running with a default. This
is deliberate: a config file that is quietly ignored is how backups end up in the wrong place.

## Startup settings

| YAML key | Environment | Flag | Default | Meaning |
|---|---|---|---|---|
| `listen` | `BACKVAULT_LISTEN` | `--listen` | `:8080` | address and port the HTTP server binds |
| `data_dir` | `BACKVAULT_DATA_DIR` | `--data-dir` | `./data` | database, master key and work directory |
| `work_dir` | `BACKVAULT_WORK_DIR` | `--work-dir` | `<data_dir>/work` | spool directory for runs in flight |
| `master_key` | `BACKVAULT_MASTER_KEY` | none | empty | the key itself, hex or base64 |
| `master_key_file` | `BACKVAULT_MASTER_KEY_FILE` | none | `<data_dir>/master.key` | file holding the key |
| `base_url` | `BACKVAULT_BASE_URL` | `--base-url` | empty | public URL of this instance |
| `log_level` | `BACKVAULT_LOG_LEVEL` | `--log-level` | `info` | `debug`, `info`, `warn` or `error` |
| `log_format` | `BACKVAULT_LOG_FORMAT` | `--log-format` | `text` | `text` or `json` |
| `admin_email` | `BACKVAULT_ADMIN_EMAIL` | none | empty | bootstrap admin address |
| `admin_password` | `BACKVAULT_ADMIN_PASSWORD` | none | empty | bootstrap admin password |
| `trusted_proxies` | `BACKVAULT_TRUSTED_PROXIES` | none | empty | proxies whose forwarding headers are believed |
| `metrics_token` | `BACKVAULT_METRICS_TOKEN` | none | empty | bearer token required on `/metrics` |
| none | `BACKVAULT_CONFIG` | `--config` | empty | path to the YAML file itself |

### listen

Host and port, for example `:8080` for every interface, or `127.0.0.1:8080` to bind the loopback
only and let a reverse proxy publish it. Binding the loopback is the right default when a proxy
terminates TLS, see [install.md](install.md).

### data_dir and work_dir

`data_dir` holds `backvault.db` (SQLite in WAL mode, so `backvault.db-wal` and `backvault.db-shm` appear next
to it), `master.key` unless it is elsewhere, and `work/`. A relative path is resolved to an absolute
one at startup, relative to the working directory of the process, which is why the systemd unit sets
`WorkingDirectory`.

`work_dir` is where a run spools its dump before uploading. Each run gets its own
`<work_dir>/<runID>/` directory, mode `0750`, holding one spool file with mode `0600`. It needs
enough free space for the largest packed artifact you produce, and the run directory is removed
when the run ends, on success and on failure. Put it on the same filesystem as `data_dir` unless
you have a reason not to, and keep it off `/tmp` when `/tmp` is a small tmpfs.

Both directories should be mode `0750` and owned by the service user. See [security.md](security.md).

### master_key and master_key_file

Every secret Backvault stores, database passwords, SSH keys, webhook URLs, job encryption passphrases,
is encrypted with AES-256-GCM under this key. It is 32 bytes, supplied as 64 hex characters or as
base64.

When neither `master_key` nor an existing `master_key_file` is found, Backvault generates a key on
first start and writes it to `master_key_file` with mode `0600`. That file is the single most
important thing in the data directory.

```bash
head -c 32 /dev/urandom | base64
openssl rand -hex 32
```

Prefer `master_key_file` over `master_key` for a long running service: an environment variable is
visible to anything that can read `/proc/<pid>/environ` and tends to end up in shell history and in
container inspection output. `master_key` is useful when the key comes from a secret manager at
start time.

**If the key is lost, the encrypted values cannot be recovered.** The database still opens, jobs and
run history are intact, but every stored password, key and passphrase is unreadable, and jobs fail
at the connection stage until you re-enter the credentials. Artifacts encrypted with age are a
separate matter: they are recoverable with the passphrase itself, which is why the passphrase
belongs in your password manager and not only in Backvault. See [encryption.md](encryption.md).

To rotate the key: stop the service, export with secrets
(`backvault export --include-secrets --out config.yaml`), move the old key file aside, start the
service so a new key is generated, then import the export
(`backvault import config.yaml`). Delete the export afterwards. Do this during a window with no runs.

### base_url

The public URL, for example `https://backvault.example.com`, with no trailing slash. It must start with
`http://` or `https://` when set, and startup fails with a clear error otherwise. It is used for
links in notifications and for deciding whether the session cookie carries the `Secure` flag, so a
Backvault behind HTTPS with an unset or `http://` base URL will hand out cookies without `Secure`.

### log_level and log_format

`text` is meant for a terminal, `json` for a log collector. Structured fields are stable across both:
`run_id`, `job_slug`, `stage`, `destination`. Set `debug` while diagnosing a driver problem, it logs
the external commands being run, never their credentials.

### admin_email and admin_password

Both or neither. A configuration that sets only one of them fails at startup with
`admin_email and admin_password must be set together`.

They are used only while the database holds no users. With both set, the first start creates that
admin, named `Administrator`, and the setup page is never reachable, which is how an unattended
deployment avoids a window in which anyone who finds the URL can claim the instance. Remove them
from the config after the first start: they are ignored once a user exists, but there is no reason
to keep a password on disk.

### trusted_proxies

A list of addresses, or CIDR ranges, whose `X-Forwarded-For` and `X-Real-Ip` headers are believed.
The client address is then the rightmost entry of `X-Forwarded-For` that is not itself a trusted
proxy, with `X-Real-Ip` as the fallback. When the peer is not in the list the headers are ignored
and the direct peer address is used. This affects the client IP in the audit log and in the login
rate limit. Set it to your reverse proxy and nothing else. In YAML it is a list, in the environment
it is comma separated.

### metrics_token

When empty, `/metrics` is open to anyone who can reach the port. When set, the endpoint requires
the same value, either as `Authorization: Bearer <metrics_token>` or as `?token=<metrics_token>`.
Use it whenever Backvault is reachable from outside, see [monitoring.md](monitoring.md).

`/metrics` is served at the root only, not under `/api/v1`.

## The example files

`deploy/backvault.example.yaml`, the file `install.sh` copies to `/etc/backvault/backvault.yaml`:

```yaml
listen: ":8080"
data_dir: /var/lib/backvault
work_dir: /var/lib/backvault/work
master_key: ""
master_key_file: /var/lib/backvault/master.key
base_url: https://backvault.example.com
log_level: info
log_format: text
admin_email: ""
admin_password: ""
trusted_proxies:
  - 127.0.0.1
  - ::1
metrics_token: ""
```

`deploy/systemd/backvault.env.example`, copied to `/etc/backvault/backvault.env` and read by the unit through
`EnvironmentFile`:

```ini
BACKVAULT_LISTEN=:8080
BACKVAULT_DATA_DIR=/var/lib/backvault
BACKVAULT_WORK_DIR=/var/lib/backvault/work
BACKVAULT_MASTER_KEY_FILE=/var/lib/backvault/master.key
BACKVAULT_BASE_URL=https://backvault.example.com
BACKVAULT_LOG_LEVEL=info
BACKVAULT_LOG_FORMAT=json
BACKVAULT_TRUSTED_PROXIES=127.0.0.1,::1
BACKVAULT_METRICS_TOKEN=
BACKVAULT_ADMIN_EMAIL=
BACKVAULT_ADMIN_PASSWORD=
TZ=UTC
```

Neither file carries comments, by design. Configuration files are read by programs and by people who
copy them, and a comment that drifts out of date is worse than no comment. This page is where the
keys are explained, and it is versioned with the code.

Using both a YAML file and an environment file is fine, the environment wins. Pick one as the place
you edit, and leave the other minimal.

`TZ` is not a Backvault setting, it is the process timezone, which affects log timestamps in `text`
format. Schedules carry their own timezone per job and are not affected by it.

## Docker

The image sets `BACKVAULT_DATA_DIR=/data` and `BACKVAULT_LISTEN=:8080` already. Pass the rest as
environment variables:

```bash
docker run -d --name backvault \
  -p 8080:8080 \
  -v backvault-data:/data \
  -e BACKVAULT_BASE_URL=https://backvault.example.com \
  -e BACKVAULT_LOG_FORMAT=json \
  ghcr.io/arthurr0/backvault:latest
```

To mount a config file instead, put it in the volume and point at it:

```bash
docker run -d --name backvault \
  -v backvault-data:/data \
  -v /etc/backvault/backvault.yaml:/etc/backvault/backvault.yaml:ro \
  -e BACKVAULT_CONFIG=/etc/backvault/backvault.yaml \
  ghcr.io/arthurr0/backvault:latest
```

## Runtime settings

These live in the database, not in the config file. Edit them under Settings in the panel, or with
`PUT /settings` (see [api.md](api.md)).

| Field | Default | Meaning |
|---|---|---|
| `siteName` | `Backvault` | shown in the panel and in every notification |
| `baseUrl` | empty | link target in notifications, falls back to the startup `base_url` |
| `defaultTimezone` | `UTC` | preselected when creating a job |
| `maxConcurrentRuns` | `2` | how many runs execute at once, the rest queue |
| `defaultRetention` | `keepLast 7`, `keepDaily 7`, `keepWeekly 4`, `keepMonthly 6` | applied to jobs whose own retention is all zeroes |
| `runHistoryDays` | `90` | runs and their logs older than this are removed |
| `auditHistoryDays` | `365` | audit entries older than this are removed |
| `overdueCheckMinutes` | `15` | how often the overdue watcher runs |
| `defaultNotifyOn` | `run.failed`, `run.warning`, `job.overdue` | events used by jobs with an empty `notifyOn` |

```json
{
  "siteName": "Backvault",
  "baseUrl": "https://backvault.example.com",
  "defaultTimezone": "Europe/Warsaw",
  "maxConcurrentRuns": 2,
  "defaultRetention": {"keepLast": 7, "keepHourly": 0, "keepDaily": 14, "keepWeekly": 8, "keepMonthly": 12, "keepYearly": 3, "maxAgeDays": 0},
  "runHistoryDays": 90,
  "auditHistoryDays": 365,
  "overdueCheckMinutes": 15,
  "defaultNotifyOn": ["run.failed", "run.warning", "job.overdue"]
}
```

`maxConcurrentRuns` is the setting to look at when backups slow each other down or saturate the
uplink. Retention is explained with worked examples in [retention.md](retention.md).

## Checking a configuration

```bash
backvault check
```

It prints the resolved configuration, creates the data and work directories if they are missing,
loads or generates the master key, and lists every external tool with its status, the drivers that
use it, its path and its version. It exits non zero when it found a configuration problem. Run it
after every change to the file or the unit, before restarting the service. See [troubleshooting.md](troubleshooting.md) when it reports something
unexpected.
