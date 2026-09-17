# HTTP API

Everything the admin panel does is done through this API, so anything you can click you can also script.

## Base path and content type

Almost every endpoint lives under `/api/v1`. Two of them are registered twice: `/healthz` and
`/readyz` answer both at the root and under `/api/v1`, so load balancers and container supervisors
can reach them without knowing about versioning. `/metrics` is registered **only** at the root,
there is no `/api/v1/metrics`.

Requests and responses are JSON encoded with UTF-8. Send `Content-Type: application/json` on any
request with a body. The exceptions are `POST /ingest/{jobSlug}`, which takes a raw binary stream,
`GET /artifacts/{id}/download`, which returns one, `GET /export` and `POST /import`, which are
YAML, `GET /runs/{id}/log`, which is `text/plain`, and the two event streams, which are
`text/event-stream`.

JSON bodies are read through a 4 MiB limit. `POST /import` reads at most 8 MiB of YAML, and the
ingest body is streamed with no limit at all.

Timestamps are RFC 3339 in UTC, for example `2026-03-14T02:00:00Z`. Identifiers are ULIDs, which
sort by creation time. Sizes are bytes, durations are milliseconds unless the field name says
otherwise (`timeoutMinutes`, `expectedIntervalMinutes`, `retryDelaySeconds`).

Every response under `/api/v1` carries `Cache-Control: no-store`. Any unknown path that starts with
`/api/` returns `404` with code `not_found` and the message `no such endpoint: <path>`, rather than
falling through to the panel; a known path called with the wrong method returns `405` with code
`bad_request` and the message `method not allowed`.

## Authentication

Two mechanisms are accepted on every authenticated endpoint.

**Session cookie.** `POST /auth/login` sets `backvault_session`, which is `HttpOnly`, `SameSite=Lax`
and `Path=/`, and carries the `Secure` flag when the configured base URL is `https`. The browser
panel uses this. Sessions live 30 days and slide forward while they are used. Sessions belong to a
user and inherit that user's role, `admin` or `viewer`.

**API token.** Send `Authorization: Bearer bvt_...`. The secret is `bvt_` followed by 32 base62
characters, 36 in total; only its sha256 is stored, plus the first 12 characters as the displayable
`prefix`. Tokens are created in the panel or with `backvault token create`, and the secret is shown
once at creation time. A token carries a set of scopes and, optionally, a list of job slugs it is
restricted to.

An `Authorization` header always wins over the cookie. A header that is not `Bearer` returns `401`
with the message `unsupported authorization scheme`, and an unknown or expired secret returns `401`
with `invalid or expired API token`.

| Scope | Grants |
|---|---|
| `read` | every read endpoint: jobs, runs, artifacts, dashboard, events, logs, settings |
| `run` | `POST /jobs/{id}/run`, `POST /runs/{id}/cancel`, `POST /artifacts/{id}/verify` |
| `ingest` | `POST /ingest/{jobSlug}` only, for the push scripts |
| `admin` | everything, including configuration, users, tokens, export with secrets |

Scopes imply each other in exactly two ways: `admin` satisfies every scope check, and `run` also
satisfies `read`. Nothing else is implied, so a token with only `read` cannot queue a run, and a
token with only `ingest` cannot list anything. A session for an `admin` user carries
`admin`, `read`, `run` and `ingest`; a session for a `viewer` carries `read` alone.

A handful of endpoints require authentication but no particular scope: `POST /auth/logout`,
`GET /auth/me`, every `GET /meta/*`, and `PUT /users/{id}/password`, which then checks inside the
handler that you are that user or an administrator.

When a token has a `jobSlugs` list, it is checked on `POST /ingest/{jobSlug}` and on
`POST /jobs/{id}/run`, and nowhere else. A token with `jobSlugs: ["db-prod"]` can ingest into
`db-prod` and run it, and gets `403` with `token is not allowed to use job <slug>` for any other
job, but the restriction does not cover prune, restore, verify, cancel, delete or any read
endpoint. An empty list means all jobs.

Requests without credentials get `401` with code `unauthenticated` and the message
`authentication required`. Requests with valid credentials but an insufficient scope get `403`
with code `forbidden` and the message `missing scope: <scope>`.

`POST /auth/login` is rate limited to 5 attempts per 60 seconds per client IP, as a sliding window.
Over the limit it answers `429` with code `rate_limited` and the message
`too many login attempts, try again in a minute`. There is no `Retry-After` header and no
`X-RateLimit-*` headers; a successful login resets the counter for that IP. No other endpoint is
rate limited, `POST /setup` included.

### CSRF

State changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) that authenticate with the session
cookie must also carry the header `X-Requested-With: backvault`. Without it the request is rejected
with `403`, code `forbidden` and the message `missing X-Requested-With: backvault header`. The panel
always sends the header. Requests authenticated with a bearer token are exempt, and so are
anonymous ones, because a cookie cannot be replayed from a third party site against a token
protected endpoint.

```bash
curl -sS -X POST https://backvault.example.com/api/v1/jobs/db-prod/run \
  -b cookies.txt \
  -H 'X-Requested-With: backvault'
```

The same call with a token needs no such header:

```bash
curl -sS -X POST https://backvault.example.com/api/v1/jobs/db-prod/run \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

## Errors

Every error response uses one envelope.

```json
{
  "error": {
    "code": "validation_failed",
    "message": "invalid job",
    "fields": {
      "schedule": "not a valid cron expression",
      "name": "required"
    }
  }
}
```

`fields` is omitted unless the handler filled it in, and maps the offending request field to a
short reason. It is safe to render next to form inputs. The content type is
`application/json; charset=utf-8`.

These are all the `code` values the server can produce. The code for a rejected body or query
parameter is the literal `validation_failed`, never `validation`.

| Code | HTTP | Meaning |
|---|---|---|
| `validation_failed` | 400, 422 | the request body or query is malformed or incomplete |
| `unauthenticated` | 401 | no credentials, or the session or token is invalid or expired |
| `forbidden` | 403 | authenticated, but the scope, the CSRF header or a job restriction blocks it |
| `not_found` | 404 | no object with that id or slug, or no such endpoint |
| `bad_request` | 405 | the path exists but not for this method |
| `conflict` | 409 | the change collides with existing state, for example a source still referenced by a job |
| `locked` | 423 | the job is already running and cannot be started again |
| `rate_limited` | 429 | too many login attempts, only on `POST /auth/login` |
| `internal` | 500, 502, 503 | an unexpected server error, a failing driver, or a full run queue |

`internal` covers three situations that are worth telling apart: `500` for an unexpected error,
`502` when a storage driver could not be reached for a download, a delete or a browse, and `503`
with the message `run queue is full` when the 256 slot task channel is saturated.

The only endpoint that returns `422` is ingest, where it means the run row was created and the
pipeline then failed. Error messages never contain secrets, connection strings with passwords, or
stack traces.

## Lists, pagination and filtering

Collection endpoints return a fixed envelope.

```json
{
  "items": [],
  "total": 0
}
```

`total` is the number of rows matching the filter, not the number returned in `items`. Page with
`?limit=` and `?offset=`. The default limit is 50. A limit above 500 is silently clamped to 500
rather than rejected. A limit that is negative or not a number is a `400` with code
`validation_failed`, the message `limit must be a non-negative integer` and
`fields: {"limit": "invalid"}`. `offset` follows the same rules, with its own message and field
name, and has no upper clamp.

Not every list is paginated. `GET /notifications/channels`, `GET /users`, `GET /tokens`,
`GET /destinations/{id}/browse`, the `meta` endpoints and `GET /jobs/{id}/schedule/preview` return
everything they have and set `total` to the length of `items`.

Timestamp filters (`since`, `until`) accept RFC 3339 with or without fractional seconds,
`2006-01-02T15:04:05`, `2006-01-02`, or bare Unix seconds, and are converted to UTC. Anything else
is a `400` with the message `invalid timestamp for <name>` and
`fields: {"<name>": "expected RFC3339 or YYYY-MM-DD"}`.

Boolean query parameters such as `?raw=1`, `?packed=1`, `?dryRun=1`, `?includeSecrets=1` and
`?deleteArtifacts=1` count as true for `1`, `true`, `yes` and `on`, and as false for everything
else.

Filters are query parameters named after the field they filter, for example
`/runs?job=db-prod&status=failed&since=2026-03-01T00:00:00Z`. Unknown query parameters are ignored.
The per endpoint lists below are exhaustive.

## Secrets in requests and responses

Driver configuration fields marked `secret` in the driver spec, the job encryption passphrase, and
notification channel secrets are encrypted at rest and never returned in clear text. Reads return
the literal string `********` in their place. A stored object whose `kind` is not a driver this
build knows about comes back with `config` as `{}`.

When you send an object back, any secret field whose value is exactly `********` keeps the value
already stored. This is what makes a round trip of `GET` then `PUT` safe: you can edit a name
without knowing the password. To change a secret, send the new value. To clear it, send an empty
string.

The same masking applies to `GET /export`. An admin can ask for the real values with
`?includeSecrets=1`, and that request is written to the audit log as `export.secrets`.

## Health and metrics

### GET /healthz and GET /api/v1/healthz

Liveness. No authentication, at either path. Always returns `200` as soon as the HTTP server is
accepting connections. It does not touch the database, so it stays `200` during a database problem.
Use it for container and process supervision.

```json
{"status": "ok", "version": "1.4.0", "time": "2026-03-14T09:15:00Z"}
```

### GET /readyz and GET /api/v1/readyz

Readiness. No authentication, at either path. Pings the database with a 3 second timeout and checks
that the scheduler loop is running. Use it for load balancer membership. There are exactly three
possible bodies.

```json
{"status": "ok", "database": "ok", "scheduler": true}
```

```json
{"status": "unavailable", "database": "ok", "scheduler": false}
```

```json
{"status": "unavailable", "database": "context deadline exceeded"}
```

The first is `200`, the other two are `503`. When the database ping fails there is no `scheduler`
key at all.

### GET /metrics

Root path only. Prometheus text format. Open by default. When `metrics_token` is configured the
endpoint accepts the token either as `?token=` or as `Authorization: Bearer <token>`, and returns
`401` with code `unauthenticated` and the message `metrics token required` without it. See
[monitoring.md](monitoring.md) for the metric list and a scrape configuration.

## Setup and authentication

### GET /setup/status

No authentication. Tells the panel whether to show the setup screen.

```json
{"needsSetup": true}
```

`needsSetup` is `true` only while the database holds no users.

### POST /setup

No authentication, and accepted only while no users exist. Creates the first admin account and logs
it in by setting the session cookie. Once a user exists this endpoint returns `409` with code
`conflict` and the message `setup has already been completed`, so it cannot be used to add a second
admin. Passwords are between 10 and 512 characters.

```json
{"name": "Ada Lovelace", "email": "ada@example.com", "password": "a long passphrase"}
```

Response `201` with `{"user": {...}}`. Status: 201, 400, 409.

### POST /auth/login

No authentication. Rate limited as described above: 5 attempts per minute per IP, then `429` with
code `rate_limited` and no `Retry-After` header.

```bash
curl -sS -c cookies.txt -X POST https://backvault.example.com/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email": "ada@example.com", "password": "a long passphrase"}'
```

```json
{
  "user": {
    "id": "01JQ2K9F7A0000000000000001",
    "email": "ada@example.com",
    "name": "Ada Lovelace",
    "role": "admin",
    "createdAt": "2026-01-04T09:12:00Z",
    "lastLoginAt": "2026-03-14T08:00:12Z"
  }
}
```

A wrong password returns `401` with code `unauthenticated` and the message
`invalid email or password`, the same as an unknown email, so the endpoint does not reveal which
accounts exist. Status: 200, 400, 401, 429.

### POST /auth/logout

Any authentication. Clears the cookie and deletes the session server side. Status: 204, 401.

### GET /auth/me

Any authentication. Tells a client what it is allowed to do, which the panel uses to hide actions it
cannot perform.

```json
{
  "authType": "session",
  "scopes": ["admin", "read", "run", "ingest"],
  "user": {"id": "01JQ2K9F7A0000000000000001", "email": "ada@example.com", "name": "Ada Lovelace", "role": "admin", "createdAt": "2026-01-04T09:12:00Z"}
}
```

For a token, `authType` is `token`, there is no `user` key at all, and a `token` object describes
the credential itself.

```json
{
  "authType": "token",
  "scopes": ["ingest"],
  "token": {"id": "01JQ2K9F7A0000000000000040", "name": "web01 push", "jobSlugs": ["web-files"]}
}
```

Status: 200, 401.

## Meta

These endpoints describe the server and its drivers. Any authentication, no scope. The panel
renders every driver form from them and hardcodes nothing per driver.

### GET /meta/version

```json
{
  "version": "1.4.0",
  "commit": "9f3c1ab",
  "buildDate": "2026-03-01T10:22:31Z",
  "goVersion": "go1.27.1",
  "startedAt": "2026-03-14T06:00:00Z"
}
```

### GET /meta/sources, GET /meta/destinations, GET /meta/notifiers

Each returns the list envelope of `DriverSpec` objects, sorted by label.

```json
{
  "items": [
    {
      "kind": "postgres",
      "label": "PostgreSQL",
      "description": "Logical dump of a PostgreSQL database with pg_dump, or of the whole cluster with pg_dumpall.",
      "icon": "database",
      "category": "Database",
      "capabilities": ["test", "restore"],
      "tools": ["pg_dump", "pg_dumpall", "pg_restore", "psql"],
      "fields": [
        {"name": "host", "label": "Host", "type": "string", "required": true, "secret": false, "default": "localhost", "group": "Connection",
         "help": "Hostname, IP address or path to the Unix socket directory."},
        {"name": "port", "label": "Port", "type": "port", "required": false, "secret": false, "default": 5432, "group": "Connection"},
        {"name": "password", "label": "Password", "type": "secret", "required": false, "secret": true, "group": "Connection"},
        {"name": "database", "label": "Database", "type": "string", "required": true, "secret": false, "group": "Connection",
         "showIf": {"all_databases": false}},
        {"name": "format", "label": "Format", "type": "select", "required": false, "secret": false, "default": "custom", "group": "Options",
         "showIf": {"all_databases": false},
         "options": [{"value": "custom", "label": "Custom (.dump, compressed, restores with pg_restore)"},
                     {"value": "plain", "label": "Plain SQL (.sql, restores with psql)"}]},
        {"name": "exclude_tables", "label": "Exclude tables", "type": "list", "required": false, "secret": false, "group": "Options",
         "showIf": {"all_databases": false}},
        {"name": "binary_path", "label": "Binary path", "type": "path", "required": false, "secret": false, "group": "Advanced", "advanced": true}
      ]
    }
  ],
  "total": 11
}
```

Field types are `string`, `text`, `secret`, `int`, `port`, `bool`, `select`, `list` and `path`.
Capabilities are `test`, `restore`, `browse` and `ingest`. `tools` lists external binaries the
driver shells out to, which is what `GET /meta/tools` reports on. `docsUrl`, `placeholder`, `help`,
`options`, `group`, `advanced`, `showIf` and `default` are all omitted when empty.

`showIf` makes a field conditional on the value of another field in the same config, for example
`{"auth": "password"}` or `{"all_databases": false}`. A required field is only required while its
`showIf` matches. What each field means per driver is documented in
[sources/README.md](sources/README.md) and [destinations/README.md](destinations/README.md), this
endpoint is the machine readable version of those pages.

### GET /meta/tools

Reports which external tools are present on the machine running Backvault, so the panel can warn
before a job fails at the dump stage.

```json
{
  "items": [
    {"name": "pg_dump", "available": true, "path": "/usr/bin/pg_dump", "version": "16.14", "usedBy": ["postgres"]},
    {"name": "mongodump", "available": false, "usedBy": ["mongodb"]}
  ],
  "total": 2
}
```

### GET /meta/timezones

`{"items": ["UTC", "Africa/Cairo", "..."], "total": 69}`, the IANA names offered for
`Job.timezone`. `UTC` comes first, the rest are sorted alphabetically.

## Dashboard

### GET /dashboard

Scope `read`. One request that fills the whole dashboard page. `?days=` sets the length of the
`daily` series and defaults to 30; a value outside `1..365`, or one that is not a number, is
ignored and 30 is used, without an error. `recentRuns` always holds the last 15 runs.

```json
{
  "jobs": 12,
  "jobsEnabled": 11,
  "jobsOverdue": 1,
  "jobsFailing": 0,
  "runsRunning": 1,
  "runs24hSuccess": 23,
  "runs24hFailed": 1,
  "artifacts": 318,
  "totalBytes": 41231872311,
  "destinations": [
    {"destinationId": "01JQ2K9F7A0000000000000010", "destinationName": "Hetzner Box", "kind": "sftp", "bytes": 28931872311, "artifacts": 210}
  ],
  "daily": [
    {"date": "2026-03-13", "success": 23, "failed": 1, "bytes": 1931872311}
  ],
  "recentRuns": [],
  "upcoming": [],
  "problemJobs": []
}
```

`recentRuns` holds full `Run` objects, `upcoming` and `problemJobs` hold full `Job` objects.
`daily` has one entry per day, including days with no runs. `runs24hFailed` counts failed and
warning runs together. Status: 200, 401, 403.

## Events

### GET /events/stream

Scope `read`, session or token. Server sent events, no query parameters. The stream opens with a
comment line, then emits three event types, and sends a `: ping` comment every 20 seconds so that
proxies do not close an idle connection.

```text
: connected

event: run.updated
data: {"id":"01JQ2K9F7A0000000000000100","jobId":"01JQ2K9F7A0000000000000020","jobSlug":"db-prod","kind":"backup","status":"running","stages":[]}

event: job.updated
data: {"id":"01JQ2K9F7A0000000000000020","slug":"db-prod","name":"Shop database nightly","enabled":true,"overdue":false,"nextRunAt":"2026-03-15T02:00:00Z"}

event: artifact.updated
data: {"id":"01JQ2K9F7A0000000000000200","jobSlug":"db-prod","runId":"01JQ2K9F7A0000000000000100","status":"present","size":2411223}

: ping
```

Each `data:` line carries the bare object: a full `Run` for `run.updated`, a full `Job` for
`job.updated`, a full `Artifact` for `artifact.updated`, exactly as the matching `GET` endpoint
would return it. There is no wrapper around it, no `type` field and no id field beside it.

`run.updated` fires on every state change of a run, including each stage transition, so a client can
render the timeline without polling. The server never sends a `retry:` field, so the browser uses
its own default reconnect delay. There is no resume cursor: reconnect with a plain new request and
fetch the current state from `GET /runs` afterwards. The subscription buffer holds 256 events and
drops events for a consumer that cannot keep up, rather than queueing them.

Status: 200, 401, 403.

## Sources

A source describes what to back up. `kind` selects the driver and `config` is validated against that
driver's field list.

### GET /sources

Scope `read`. Query: `kind`, `q` (name substring), `limit`, `offset`. Returns the list envelope of
`Source` objects.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000001",
      "name": "Shop database",
      "kind": "postgres",
      "description": "Primary application database",
      "config": {"host": "db.internal", "port": 5432, "database": "shop", "user": "backup", "password": "********", "format": "custom"},
      "tags": ["production"],
      "createdAt": "2026-01-04T09:12:00Z",
      "updatedAt": "2026-03-02T11:40:00Z",
      "lastTestAt": "2026-03-02T11:40:05Z",
      "lastTestOk": true,
      "jobCount": 2
    }
  ],
  "total": 1
}
```

`lastTestError` is present only when the last test failed, and `lastTestOk` is then `false`.

### POST /sources

Scope `admin`. Body: `name`, `kind`, `config`, optional `description` and `tags`. `name` and `kind`
are required, and an unknown `kind` is a `400` with `fields: {"kind": "unknown"}`. The config is
validated against the driver spec, and a missing required field comes back as
`fields: {"config": "missing required fields: ..."}`.

```bash
curl -sS -X POST https://backvault.example.com/api/v1/sources \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
        "name": "Shop database",
        "kind": "postgres",
        "tags": ["production"],
        "config": {"host": "db.internal", "port": 5432, "database": "shop", "user": "backup", "password": "s3cret", "format": "custom"}
      }'
```

The response is the stored source with its secrets masked. A name that is already taken is a `409`
with code `conflict`. Status: 201, 400, 401, 403, 409.

### GET /sources/{id}

Scope `read`. `{id}` is the ULID, sources have no slug. Status: 200, 401, 403, 404.

### PUT /sources/{id}

Scope `admin`. Full replacement of the mutable fields. Secrets sent as `********` keep their stored
value. Omitting `kind` keeps the stored kind; changing it drops the stored secrets, because they
belong to the old driver. Status: 200, 400, 401, 403, 404, 409.

### DELETE /sources/{id}

Scope `admin`. Refused with `409`, code `conflict` and the message `source is used by jobs` while
any job references the source. Delete or repoint those jobs first. Status: 204, 401, 403, 404, 409.

### POST /sources/test

Scope `admin`. Tests a configuration that has not been saved yet, which is what the "Test
connection" button in the source form calls.

```json
{"kind": "postgres", "config": {"host": "db.internal", "port": 5432, "database": "shop", "user": "backup", "password": "s3cret"}}
```

```json
{"ok": false, "message": "connection refused: dial tcp 10.0.0.9:5432", "durationMs": 1203}
```

The endpoint returns `200` with `ok: false` for a failed connection and for a config the driver
rejects. A `400` means the request itself was wrong: a body that is not JSON, or a `kind` no driver
answers to, which comes back as `fields: {"kind": "unknown"}`. Status: 200, 400, 401, 403.

### POST /sources/{id}/test

Scope `admin`. Same response shape, but it tests the stored configuration, takes no body, and
records the outcome in `lastTestAt`, `lastTestOk` and `lastTestError`. Audited as `source.test`.
Status: 200, 401, 403, 404.

## Destinations

A destination describes where artifacts go. The shape mirrors sources, with storage counters added.

### GET /destinations

Scope `read`. Query: `kind`, `q`, `limit`, `offset`.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000010",
      "name": "Hetzner Box",
      "kind": "sftp",
      "description": "Storage Box u123456",
      "config": {"host": "u123456.your-storagebox.de", "port": 23, "user": "u123456-sub1", "auth": "key", "private_key": "********", "host_key": "SHA256:GxQ...", "base_path": "backups/backvault"},
      "tags": ["offsite"],
      "createdAt": "2026-01-04T09:20:00Z",
      "updatedAt": "2026-01-04T09:20:00Z",
      "lastTestAt": "2026-03-14T02:04:11Z",
      "lastTestOk": true,
      "jobCount": 3,
      "usedBytes": 28931872311,
      "artifactCount": 210
    }
  ],
  "total": 1
}
```

### POST /destinations, GET /destinations/{id}, PUT /destinations/{id}, DELETE /destinations/{id}

Same rules as sources: scope `admin` for writes, scope `read` for the reads, `409` with
`destination is used by jobs` on delete while a job references it. Status: as for sources.

### POST /destinations/test and POST /destinations/{id}/test

Scope `admin`. The unsaved variant takes `{"kind", "config"}`, the stored variant takes no body and
records `lastTestAt`, `lastTestOk` and `lastTestError`. Both answer `200` with
`{"ok", "message", "durationMs"}`, including when the test itself failed. What the test does is up
to the driver: `local` and `sftp` create and remove a probe file, so a success means the credentials
can really write, while `s3` checks that the bucket is reachable.

### GET /destinations/{id}/browse

Scope `read`. Query: `prefix` (default empty). Lists what is really on the destination, which is how
you check whether an artifact Backvault lost track of is still there.

```json
{
  "items": [
    {"path": "backvault/db-prod/", "size": 0, "modTime": "2026-03-14T02:04:00Z", "isDir": true},
    {"path": "backvault/db-prod/db-prod-20260314-020000.dump.zst.age", "size": 2411223, "modTime": "2026-03-14T02:04:09Z", "isDir": false}
  ],
  "total": 2
}
```

A driver that cannot list returns `502` with code `internal` and the driver's own message. Status:
200, 401, 403, 404, 502.

## Jobs

A job binds one source to one or more destinations, with a schedule, packing, retention and
notification settings. See [concepts.md](concepts.md).

### GET /jobs

Scope `read`. Query: `sourceId`, `destinationId`, `tag`, `q`, `enabled`, `limit`, `offset`.

`enabled` has one quirk worth knowing: it filters for enabled jobs only when the value is `1` or
`true` (in any case), and any other non empty value, `0`, `no` and `false` included, filters for
**disabled** jobs. Leave it out entirely to get both.

### POST /jobs

Scope `admin`. `slug` is derived from `name` when omitted, must match
`^[a-z0-9]+(?:-[a-z0-9]+)*$`, and must be unique. A slug you send that is already taken is caught by
validation and comes back as a `400` with `fields: {"slug": "already in use"}`; only a collision the
database catches later is a `409`. A slug derived from the name is de-duplicated automatically with
`-2`, `-3` and so on. The cron expression, the timezone, the retention
numbers, the notification events, the referenced source, destinations and channels, and the
encryption passphrase are all validated before the job is stored: `encryption: "age"` without
`encryptionPassphrase` comes back as
`fields: {"encryptionPassphrase": "required when encryption is age"}`.

```bash
curl -sS -X POST https://backvault.example.com/api/v1/jobs \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
        "name": "Shop database nightly",
        "slug": "db-prod",
        "sourceId": "01JQ2K9F7A0000000000000001",
        "destinationIds": ["01JQ2K9F7A0000000000000010", "01JQ2K9F7A0000000000000011"],
        "schedule": "0 2 * * *",
        "timezone": "Europe/Warsaw",
        "enabled": true,
        "compression": "zstd",
        "compressionLevel": 0,
        "encryption": "age",
        "encryptionPassphrase": "a long passphrase",
        "retention": {"keepLast": 7, "keepHourly": 0, "keepDaily": 14, "keepWeekly": 8, "keepMonthly": 12, "keepYearly": 3, "maxAgeDays": 0},
        "notificationChannelIds": ["01JQ2K9F7A0000000000000030"],
        "notifyOn": ["run.failed", "run.warning"],
        "timeoutMinutes": 120,
        "retries": 2,
        "retryDelaySeconds": 60,
        "verifyAfterUpload": true,
        "preCommand": "",
        "postCommand": "",
        "expectedIntervalMinutes": 0,
        "tags": ["production"]
      }'
```

The response is the stored job, with the read only fields filled in.

```json
{
  "id": "01JQ2K9F7A0000000000000020",
  "slug": "db-prod",
  "name": "Shop database nightly",
  "description": "",
  "sourceId": "01JQ2K9F7A0000000000000001",
  "destinationIds": ["01JQ2K9F7A0000000000000010", "01JQ2K9F7A0000000000000011"],
  "schedule": "0 2 * * *",
  "timezone": "Europe/Warsaw",
  "enabled": true,
  "compression": "zstd",
  "compressionLevel": 0,
  "encryption": "age",
  "encryptionPassphrase": "********",
  "retention": {"keepLast": 7, "keepHourly": 0, "keepDaily": 14, "keepWeekly": 8, "keepMonthly": 12, "keepYearly": 3, "maxAgeDays": 0},
  "notificationChannelIds": ["01JQ2K9F7A0000000000000030"],
  "notifyOn": ["run.failed", "run.warning"],
  "timeoutMinutes": 120,
  "retries": 2,
  "retryDelaySeconds": 60,
  "preCommand": "",
  "postCommand": "",
  "verifyAfterUpload": true,
  "expectedIntervalMinutes": 0,
  "tags": ["production"],
  "createdAt": "2026-01-04T09:30:00Z",
  "updatedAt": "2026-03-02T11:41:00Z",
  "sourceName": "Shop database",
  "sourceKind": "postgres",
  "destinationNames": ["Hetzner Box", "MinIO"],
  "lastRun": {
    "id": "01JQ2K9F7A0000000000000100",
    "kind": "backup",
    "status": "success",
    "startedAt": "2026-03-14T02:00:00Z",
    "finishedAt": "2026-03-14T02:04:11Z",
    "durationMs": 251000,
    "bytes": 2411223
  },
  "nextRunAt": "2026-03-15T02:00:00Z",
  "overdue": false,
  "artifactCount": 28,
  "totalBytes": 67514244
}
```

An empty `schedule` means manual only, and `nextRunAt` is then absent. A job whose source kind is
`push` is never scheduled and uses `expectedIntervalMinutes` for overdue detection; a `schedule` on
such a job is accepted and stored, it is simply never acted on.

Status: 201, 400, 401, 403, 409.

### GET /jobs/{id}

Scope `read`. `{id}` accepts the ULID or the slug, so `GET /jobs/db-prod` works, and the same is
true of every other `/jobs/{id}` route. Status: 200, 401, 403, 404.

### PUT /jobs/{id}

Scope `admin`. Full replacement, with the same validation as create. Changing `slug` changes the
ingest URL and the artifact path prefix for future runs, existing artifacts keep their stored path.
Status: 200, 400, 401, 403, 404, 409.

### DELETE /jobs/{id}

Scope `admin`. The job row is removed and its artifacts stay listed with the job name preserved, so
history is not lost. Pass `?deleteArtifacts=1` to delete the stored objects on the destinations as
well, which cannot be undone. Status: 204, 401, 403, 404.

### POST /jobs/{id}/run

Scope `run`. Queues a backup run and returns it immediately with status `queued`. The body is
ignored. This is the one write endpoint besides ingest where a token's `jobSlugs` list is enforced.
Returns `423` with code `locked` and the message `job is already running` when a run for this job
holds the lock, and `503` with code `internal` and `run queue is full` when the queue is saturated.

```bash
curl -sS -X POST https://backvault.example.com/api/v1/jobs/db-prod/run \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

```json
{"run": {"id": "01JQ2K9F7A0000000000000101", "jobId": "01JQ2K9F7A0000000000000020", "jobSlug": "db-prod", "jobName": "Shop database nightly", "kind": "backup", "trigger": "api", "status": "queued", "queuedAt": "2026-03-14T09:15:00Z", "durationMs": 0, "bytes": 0, "rawBytes": 0, "stages": [], "artifactIds": [], "attempt": 1}}
```

Status: 202, 401, 403, 404, 423, 500, 503.

### POST /jobs/{id}/enable and POST /jobs/{id}/disable

Scope `admin`. Return the updated job. Disabling does not cancel a run already in flight. Status:
200, 401, 403, 404.

### POST /jobs/{id}/prune

Scope `admin`. Queues a prune run that applies the job's retention policy now, body ignored. See
[retention.md](retention.md). Returns `{"run": {...}}` with `kind: "prune"`. Prune takes the same
job lock as a backup, so it can also answer `423`. Status: 202, 401, 403, 404, 423, 500, 503.

### POST /jobs/{id}/duplicate

Scope `admin`. Copies the job, including the encryption passphrase, under the slug `<slug>-copy`
(de-duplicated with `-2`, `-3` and so on when that is taken), named `<name> (copy)` and disabled.
Returns `201` with the new job. Status: 201, 401, 403, 404.

### GET /jobs/{id}/runs

Scope `read`. Query: `status`, `kind`, `limit`, `offset`. The same list as `/runs` with the job
filter applied. Status: 200, 401, 403, 404.

### GET /jobs/{id}/artifacts

Scope `read`. Query: `status`, `destination` (a destination ID), `limit`, `offset`. Status: 200,
401, 403, 404.

### GET /jobs/{id}/schedule/preview

Scope `read`. Expands the job's cron expression and returns the next firing times, which is what the
"next runs" box in the job form shows. `?count=` defaults to 5 and is accepted only when it is a
number in `1..50`; anything else falls back to 5 without an error. The times are computed in the
job's own timezone, or in `settings.defaultTimezone` when the job does not set one.

```bash
curl -sS "https://backvault.example.com/api/v1/jobs/db-prod/schedule/preview?count=3" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

```json
{
  "items": ["2026-03-15T02:00:00Z", "2026-03-16T02:00:00Z", "2026-03-17T02:00:00Z"],
  "total": 3
}
```

A job with no schedule returns `{"items": [], "total": 0}`. A schedule that cannot be expanded is a
`400` with code `validation_failed` and `fields: {"schedule": "<reason>"}`. Status: 200, 400, 401,
403, 404.

## Runs

A run is one execution. Backups, restores, prunes, verifies and ingests are all runs, distinguished
by `kind`.

### GET /runs

Scope `read`. Query: `job` (accepts an ID or a slug, and falls back to matching the stored slug when
neither resolves), `status`, `kind`, `since`, `until`, `limit`, `offset`. Sorted newest first.
`status` and `kind` are matched literally and are not validated, so a typo returns an empty list
rather than an error.

### GET /runs/{id}

Scope `read`. `{id}` is the run ULID only.

```json
{
  "id": "01JQ2K9F7A0000000000000100",
  "jobId": "01JQ2K9F7A0000000000000020",
  "jobSlug": "db-prod",
  "jobName": "Shop database nightly",
  "kind": "backup",
  "trigger": "schedule",
  "status": "success",
  "queuedAt": "2026-03-14T02:00:00Z",
  "startedAt": "2026-03-14T02:00:00Z",
  "finishedAt": "2026-03-14T02:04:11Z",
  "durationMs": 251000,
  "bytes": 2411223,
  "rawBytes": 19883122,
  "sha256": "9f2c4d1e7b0a5c8e3f6d9b2a4c7e1f8d0b3a6c9e2f5d8b1a4c7e0f3d6b9a2c5e",
  "filename": "db-prod-20260314-020000.dump.zst.age",
  "stages": [
    {"name": "prepare", "status": "success", "startedAt": "2026-03-14T02:00:00Z", "finishedAt": "2026-03-14T02:00:00Z"},
    {"name": "pre-command", "status": "skipped"},
    {"name": "dump", "status": "success", "startedAt": "2026-03-14T02:00:00Z", "finishedAt": "2026-03-14T02:03:40Z", "message": "19.0 MiB read"},
    {"name": "pack", "status": "success", "startedAt": "2026-03-14T02:03:40Z", "finishedAt": "2026-03-14T02:03:55Z", "message": "zstd, age"},
    {"name": "upload:hetzner-box", "status": "success", "startedAt": "2026-03-14T02:03:55Z", "finishedAt": "2026-03-14T02:04:05Z"},
    {"name": "upload:minio", "status": "success", "startedAt": "2026-03-14T02:04:05Z", "finishedAt": "2026-03-14T02:04:09Z"},
    {"name": "verify", "status": "success", "startedAt": "2026-03-14T02:04:09Z", "finishedAt": "2026-03-14T02:04:10Z"},
    {"name": "retention", "status": "success", "startedAt": "2026-03-14T02:04:10Z", "finishedAt": "2026-03-14T02:04:11Z", "message": "1 artifact pruned"},
    {"name": "post-command", "status": "skipped"},
    {"name": "notify", "status": "success", "startedAt": "2026-03-14T02:04:11Z", "finishedAt": "2026-03-14T02:04:11Z"}
  ],
  "artifactIds": ["01JQ2K9F7A0000000000000200", "01JQ2K9F7A0000000000000201"],
  "attempt": 1,
  "meta": {"sourceKind": "postgres", "extension": "dump"},
  "createdBy": "schedule"
}
```

`bytes` is the size of what was uploaded, after compression and encryption, and it is what `sha256`
is computed over. `rawBytes` is what the source produced before packing. `error` is present only on
a failed run. Stage names for uploads are `upload:` followed by the destination slug or name.

Run status values are `queued`, `running`, `success`, `warning`, `failed` and `canceled`. `warning`
means the run produced an artifact but something was wrong, typically one destination out of several
failed, or a verify mismatch. Run kinds are `backup`, `restore`, `prune`, `verify` and `ingest`.
Stage status values are `pending`, `running`, `success`, `failed` and `skipped`. Triggers are
`schedule`, `manual`, `api`, `ingest` and `retry`.

Status: 200, 401, 403, 404.

### POST /runs/{id}/cancel

Scope `run`. Cancels the run context, which asks the driver to stop and removes the spool directory.
The body is ignored, and so is the response: a successful cancel is `202` with an **empty body**,
not a run object. A run that already finished returns `409` with `run has already finished`, and a
run that is not executing on this server returns `409` with `run is not active on this server`.
Status: 202, 401, 403, 404, 409.

There is no retry endpoint. Backvault never re-runs a run on request; a failed backup is retried by the
engine itself according to the job's own `retries` and `retryDelaySeconds`, and those attempts show
up as `trigger: "retry"` with an increasing `attempt`. To start over by hand, queue a fresh run with
`POST /jobs/{id}/run`.

### GET /runs/{id}/log

Scope `read`. Returns `text/plain; charset=utf-8`, the whole log. `?offset=` returns from that byte
offset, which is how a poller tails without refetching. An offset past the end returns an empty
body rather than an error; a negative or unparsable offset is a `400` with
`fields: {"offset": "invalid"}`. The response carries `X-Backvault-Log-Size` with the full length of
the log in bytes, so a poller knows where to continue, plus the usual `Content-Length` of the slice
it received.

```bash
curl -sS "https://backvault.example.com/api/v1/runs/01JQ2K9F7A0000000000000100/log?offset=4096" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

Status: 200, 400, 401, 403, 404.

### GET /runs/{id}/log/stream

Scope `read`. Server sent events, no query parameters. One `line` event per log line, then one
`done` event when the run reaches a terminal status, after which the server closes the stream. The
`data` of both is raw text, not JSON and not quoted: a log line as it was written, and for `done`
the bare status `success`, `warning`, `failed` or `canceled`. Empty lines are skipped.

```text
event: line
data: 2026-03-14T02:03:40Z dump finished, 19.0 MiB read

event: done
data: success
```

New lines are polled every 400 ms and a `: ping` comment keeps the connection open every 20 seconds.
Unlike `/events/stream` this one sends no `: connected` preamble. An unknown run id is answered with
the ordinary JSON `404` before any stream headers are written. Status: 200, 401, 403, 404.

## Artifacts

An artifact is one stored object on one destination. A backup run with two destinations produces two
artifacts sharing the same `runId`, `filename` and `sha256`.

### GET /artifacts

Scope `read`. Query: `job` (ID or slug), `destination` (a destination ID), `status`, `run` (a run
ID), `q` (filename substring), `since`, `until`, `limit`, `offset`.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000200",
      "jobId": "01JQ2K9F7A0000000000000020",
      "jobSlug": "db-prod",
      "jobName": "Shop database nightly",
      "runId": "01JQ2K9F7A0000000000000100",
      "destinationId": "01JQ2K9F7A0000000000000010",
      "destinationName": "Hetzner Box",
      "destinationKind": "sftp",
      "path": "backvault/db-prod/db-prod-20260314-020000.dump.zst.age",
      "filename": "db-prod-20260314-020000.dump.zst.age",
      "size": 2411223,
      "sha256": "9f2c4d1e7b0a5c8e3f6d9b2a4c7e1f8d0b3a6c9e2f5d8b1a4c7e0f3d6b9a2c5e",
      "compression": "zstd",
      "encryption": "age",
      "sourceKind": "postgres",
      "extension": "dump",
      "status": "present",
      "createdAt": "2026-03-14T02:04:05Z",
      "verifiedAt": "2026-03-14T02:04:10Z"
    }
  ],
  "total": 1
}
```

Artifact status values are `present`, `missing` (verify could not find it or the size did not
match), `pruned` (removed by retention) and `deleted` (removed on request).

### GET /artifacts/{id}

Scope `read`. Status: 200, 401, 403, 404.

### DELETE /artifacts/{id}

Scope `admin`. Deletes the object on the destination and marks the row `deleted`, keeping the record
for history. Deleting an object that is already gone succeeds; a destination that cannot be reached
returns `502` with code `internal`. Status: 204, 401, 403, 404, 502.

### GET /artifacts/{id}/download

Scope `read`. Streams the object from the destination. By default Backvault decrypts and decompresses
on the way through, so what you receive is the dump as the source produced it. `?raw=1` streams the
stored bytes untouched, which is what you want when copying an archive to cold storage.
`?passphrase=` overrides the job passphrase, for artifacts encrypted with a passphrase that has
since been rotated.

```bash
curl -sS -L -o shop.dump \
  "https://backvault.example.com/api/v1/artifacts/01JQ2K9F7A0000000000000200/download" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

The response carries `Content-Type: application/octet-stream`, `Content-Disposition` with the
filename, and `X-Backvault-Sha256` with the checksum of the stored object. `Content-Length` is set only
with `?raw=1`, or when the artifact is neither compressed nor encrypted, because otherwise the
unpacked length is not known in advance.

An artifact whose status is not `present` returns `409` with code `conflict` and the message
`artifact status is <status>`. A wrong passphrase, or any other failure while opening the object,
returns `502` with code `internal`. Status: 200, 401, 403, 404, 409, 502.

### POST /artifacts/{id}/restore

Scope `admin`. Queues a restore run. See [restore.md](restore.md).

```json
{
  "mode": "source",
  "targetPath": "",
  "extract": false,
  "targetSourceId": "01JQ2K9F7A0000000000000002",
  "passphrase": "",
  "params": {"clean": true, "create": false}
}
```

`mode` is required and must be `path` or `source`, otherwise the request is a `400` with
`fields: {"mode": "must be path or source"}`. With `mode: "path"` the unpacked artifact is written
to `targetPath` on the Backvault host, with `extract` unpacking tar archives, and an empty
`targetPath` is a `400` with `fields: {"targetPath": "required"}`. With `mode: "source"` it is piped
into the source driver's restore function: `targetSourceId` may be left empty to restore into the
job's own source, and an id that does not exist is a `400` with
`fields: {"targetSourceId": "source does not exist"}`. `passphrase` overrides the job passphrase,
and `params` is handed to the driver. Downloading is not a restore run, it is the download endpoint
above.

Returns `202` with `{"run": {...}}` and `kind: "restore"`. A restore does not take the job lock, so
it never returns `423`. Status: 202, 400, 401, 403, 404, 500, 503.

### POST /artifacts/{id}/verify

Scope `run`. Queues a verify run for that one artifact, body ignored: stat it on the destination,
compare the size, and compare the checksum where the destination exposes one. A mismatch marks the
artifact `missing` and the run `warning`. Returns `202` with `{"run": {...}}`. Like restore, verify
does not take the job lock. Status: 202, 401, 403, 404, 500, 503.

## Ingest

### POST /ingest/{jobSlug}

Scope `ingest`, and the slug must be allowed by the token's `jobSlugs` when that list is set. The
request body is the artifact itself, streamed. This is how machines that Backvault cannot reach push
their own backups. The call is synchronous: it returns when the run has finished, not when it has
been queued.

| Header | Required | Meaning |
|---|---|---|
| `Authorization` | yes | `Bearer bvt_...` with the `ingest` scope |
| `Content-Length` | yes | the exact body size, so the server can spool without buffering |
| `X-Backvault-Filename` | no | the original filename, used to derive the extension and the suffixes |
| `X-Backvault-Sha256` | no | lowercase hex sha256 of the body, verified after spooling |

| Query | Meaning |
|---|---|
| `packed=1` | the body is already compressed and encrypted, skip the pack stage and read the compression and encryption from the filename suffixes |

Without `packed=1` the job's own compression and encryption settings are applied to what you send.
The extension is taken from `X-Backvault-Filename`, and falls back to `bin` when nothing there
identifies one. The `expected_filename_extension` field of a `push` source is documentation for
whoever reads the job, the ingest pipeline does not consult it.

```bash
curl -sS -X POST "https://backvault.example.com/api/v1/ingest/db-prod?packed=1" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H "X-Backvault-Filename: db-prod-20260314-020000.dump.zst" \
  -H "X-Backvault-Sha256: $(sha256sum dump.zst | cut -d' ' -f1)" \
  --upload-file dump.zst
```

A `201` response carries the run and the artifacts it produced. The run id is at `run.id`.

```json
{
  "run": {"id": "01JQ2K9F7A0000000000000102", "jobId": "01JQ2K9F7A0000000000000020", "jobSlug": "db-prod", "kind": "ingest", "trigger": "ingest", "status": "success", "bytes": 2411223, "rawBytes": 2411223, "sha256": "9f2c...", "filename": "db-prod-20260314-020000.dump.zst", "durationMs": 1840, "stages": [], "artifactIds": ["01JQ2K9F7A0000000000000202"], "attempt": 1},
  "artifacts": [{"id": "01JQ2K9F7A0000000000000202", "destinationId": "01JQ2K9F7A0000000000000010", "destinationName": "Hetzner Box", "size": 2411223, "status": "present"}]
}
```

When the run row was created and the pipeline then failed, for example on a checksum mismatch or a
destination that refused the upload, the answer is `422`. The message is the run's own error and the
run id is in `error.fields.runId`, so a client can fetch the log of the failed run.

```json
{
  "error": {
    "code": "validation_failed",
    "message": "sha256 mismatch: expected 9f2c..., received 41ab...",
    "fields": {"runId": "01JQ2K9F7A0000000000000103"}
  }
}
```

`423` with code `locked` means a run for that job already holds the job lock. There is no size limit
on the ingest body, so `413` never happens, and ingest never returns `409` either.

Retry on `423` and on any `5xx`. Do not retry any other `4xx`, the request will fail the same way
every time. `scripts/backvault-push.sh` implements exactly this contract, including the checksum
header, the `packed` query and exponential backoff, and prints the run id on stdout so a cron job
can log it. See [push-and-ingest.md](push-and-ingest.md) and [scripts.md](scripts.md).

Status: 201, 400, 401, 403, 404, 422, 423, 500.

## Notifications

### GET /notifications/channels

Scope `read`. Unpaginated, `total` is the number of channels.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000030",
      "name": "Ops Slack",
      "kind": "slack",
      "config": {"webhook_url": "********", "username": "Backvault", "icon_emoji": ":lock:"},
      "enabled": true,
      "events": ["run.failed", "run.warning", "job.overdue"],
      "createdAt": "2026-01-04T09:40:00Z",
      "updatedAt": "2026-01-04T09:40:00Z",
      "lastSentAt": "2026-03-13T02:10:00Z"
    }
  ],
  "total": 1
}
```

Event names are `run.success`, `run.failed`, `run.warning`, `job.overdue`, `artifact.missing`,
`prune.done`, `restore.done` and `restore.failed`. An unknown name in `events` is a `400` with
`fields: {"events": "unknown event: <name>"}`.

### POST /notifications/channels, GET, PUT and DELETE /notifications/channels/{id}

Scope `admin` for writes, scope `read` for the reads. The body is `name`, `kind`, `config` and
optional `enabled` and `events`; `enabled` defaults to `true` on create and keeps its stored value
when omitted on update. A channel name that is already taken is a `409`. Deleting a channel removes
it from every job that referenced it. Status: 201 on create, 200 on get and update, 204 on delete,
plus 400, 401, 403, 404, 409.

### POST /notifications/channels/test and POST /notifications/channels/{id}/test

Scope `admin`. Sends a real test notification through the channel and returns `200` with
`{"ok", "message", "durationMs"}`, including when delivery failed. The unsaved variant takes
`{"kind", "config"}` in the body, the stored variant takes no body. See
[notifications.md](notifications.md).

## Settings

### GET /settings

Scope `read`.

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

### PUT /settings

Scope `admin`. Full replacement, audited as `settings.update`, and the response is the stored
settings. Fields you leave out keep their current value, because the incoming JSON is decoded on top
of the stored record. `maxConcurrentRuns` must be between 1 and 64, `overdueCheckMinutes` between 1
and 1440, the retention numbers and the history days must not be negative, `defaultTimezone` must be
a known IANA name, and `baseUrl` must start with `http://` or `https://` when it is not empty.
Lowering `runHistoryDays` prunes older runs on the next maintenance pass. See
[configuration.md](configuration.md) for the difference between these settings and the file and
environment configuration. Status: 200, 400, 401, 403.

## Users

### GET /users, POST /users, GET /users/{id}, PUT /users/{id}, DELETE /users/{id}

Scope `admin`. `role` is `admin` or `viewer`, and defaults to `viewer` when omitted. The list is
unpaginated. `GET /users/{id}`, `POST /users` and `PUT /users/{id}` return a bare user object, not
an envelope. Passwords are never returned, and `PUT /users/{id}` does not accept one: it takes
`email`, `name` and `role` only.

```json
{"name": "Grace Hopper", "email": "grace@example.com", "password": "another long passphrase", "role": "viewer"}
```

An email that already belongs to another account is a `409`. Deleting the last admin is refused with
`409` and the message `the last administrator cannot be deleted`, demoting them with
`the last administrator cannot be demoted`. Status: 200 on list, get and update, 201 on create, 204
on delete, plus 400, 401, 403, 404, 409.

### PUT /users/{id}/password

Any authentication, and then the handler requires that you are that user or an administrator;
someone else's password with a non admin principal is a `403`. Changing your own password requires
`currentPassword`, an admin changing someone else's does not. The new password goes in
`newPassword`, and must be between 10 and 512 characters. Every session of that user is destroyed,
including your own when you changed your own password.

```json
{"currentPassword": "old passphrase", "newPassword": "new passphrase"}
```

A wrong `currentPassword` is a `400` with `fields: {"currentPassword": "does not match"}`. Status:
204, 400, 401, 403, 404.

## API tokens

### GET /tokens

Scope `admin`. Unpaginated. The secret is never returned again after creation, only `prefix`, which
is the first 12 characters of the secret.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000040",
      "name": "web01 push",
      "prefix": "bvt_7f3a9c2e",
      "scopes": ["ingest"],
      "jobSlugs": ["web-files"],
      "createdAt": "2026-02-01T10:00:00Z",
      "createdBy": "ada@example.com",
      "lastUsedAt": "2026-03-14T01:00:03Z"
    }
  ],
  "total": 1
}
```

`lastUsedAt` is refreshed at most once a minute, and `expiresAt` is absent for a token that never
expires.

### POST /tokens

Scope `admin`. Body: `name`, `scopes`, optional `jobSlugs` and `expiresAt`. At least one scope is
required and each must be `read`, `run`, `ingest` or `admin`; every slug in `jobSlugs` must name an
existing job; `expiresAt` must be in the future. The response carries the secret once, in a separate
field from the token record.

```bash
curl -sS -X POST https://backvault.example.com/api/v1/tokens \
  -b cookies.txt -H 'X-Requested-With: backvault' \
  -H 'Content-Type: application/json' \
  -d '{"name": "web01 push", "scopes": ["ingest"], "jobSlugs": ["web-files"]}'
```

```json
{
  "token": {"id": "01JQ2K9F7A0000000000000040", "name": "web01 push", "prefix": "bvt_7f3a9c2e", "scopes": ["ingest"], "jobSlugs": ["web-files"], "createdAt": "2026-02-01T10:00:00Z"},
  "secret": "bvt_7f3a9c2e1b5d8f0a3c6e9b2d5f8a1c4e"
}
```

Store the secret where the pushing host can read it, for example `/etc/backvault/agent.env` with mode
600. See [security.md](security.md). Status: 201, 400, 401, 403.

### DELETE /tokens/{id}

Scope `admin`. Takes effect immediately for new requests. Status: 204, 401, 403, 404.

## Audit

### GET /audit

Scope `admin`. Query: `actor`, `action`, `objectType`, `since`, `limit`, `offset`. There is no
`until`.

```json
{
  "items": [
    {
      "id": "01JQ2K9F7A0000000000000300",
      "time": "2026-03-14T09:15:00Z",
      "actorId": "01JQ2K9F7A0000000000000001",
      "actorLabel": "ada@example.com",
      "action": "job.run",
      "objectType": "job",
      "objectId": "01JQ2K9F7A0000000000000020",
      "objectName": "Shop database nightly",
      "details": {"runId": "01JQ2K9F7A0000000000000101"},
      "ip": "203.0.113.24"
    }
  ],
  "total": 1
}
```

`actorLabel` is the user's email, or `token:<name>` for a token, or `anonymous`. Action names follow
`object.verb`, and this is the complete set: `user.create`, `user.update`, `user.delete`,
`user.password`, `token.create`, `token.delete`, `source.create`, `source.update`, `source.delete`,
`source.test`, `destination.create`, `destination.update`, `destination.delete`,
`destination.test`, `job.create`, `job.update`, `job.delete`, `job.enable`, `job.disable`,
`job.run`, `job.prune`, `job.duplicate`, `artifact.delete`, `artifact.restore`, `artifact.verify`,
`run.cancel`, `channel.create`, `channel.update`, `channel.delete`, `channel.test`,
`settings.update`, `export`, `export.secrets` and `import`.

## Export and import

### GET /export

Scope `admin`. Returns YAML, not JSON, holding sources, destinations, jobs and notification
channels. The response carries `Content-Type: application/yaml; charset=utf-8` and
`Content-Disposition: attachment; filename="backvault-export.yaml"`. Secrets are masked as `********`
unless `?includeSecrets=1`, which is audited as `export.secrets` instead of `export`.

```bash
curl -sS https://backvault.example.com/api/v1/export \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" > backvault-config.yaml
```

The document always starts with `version: 1` and `exportedAt`, then the four lists. Jobs reference
their source, destinations and channels by **name**, not by id, which is what makes the file
portable to another Backvault instance.

```yaml
version: 1
exportedAt: 2026-03-14T09:20:00Z
sources:
  - name: Shop database
    kind: postgres
    config:
      host: db.internal
      database: shop
      password: "********"
jobs:
  - slug: db-prod
    name: Shop database nightly
    source: Shop database
    destinations: [Hetzner Box, MinIO]
    schedule: "0 2 * * *"
    enabled: true
    retention: {keepLast: 7, keepDaily: 14}
```

Status: 200, 401, 403.

### POST /import

Scope `admin`. Takes the same YAML as a request body with `Content-Type: application/yaml`, read
through an 8 MiB limit. JSON is accepted too, since it is valid YAML. Objects are matched by slug
for jobs and by name for sources, destinations and channels, then created or updated, in that order:
sources, destinations, channels, jobs. Import never deletes anything and is not transactional, so a
failure halfway leaves the earlier objects written. `?dryRun=1` validates everything and returns the
plan without writing, reloading the schedule or recording an audit entry.

```bash
curl -sS -X POST "https://backvault.example.com/api/v1/import?dryRun=1" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H 'Content-Type: application/yaml' \
  --data-binary @backvault-config.yaml
```

```json
{
  "dryRun": true,
  "changes": [
    {"kind": "source", "name": "Shop database", "action": "update"},
    {"kind": "job", "name": "db-prod", "action": "create", "note": "unknown destination: MinIO"}
  ],
  "total": 2
}
```

`kind` is `source`, `destination`, `channel` or `job`. `action` is only ever `create` or `update`.
`name` is the object name, except for jobs, where it is the slug. `note` appears only on job entries
in a dry run, and only as `unknown source: <name>` or `unknown destination: <name>`; a real import
fails the whole request on the same problem instead.

An unknown driver kind is a `400` with code `validation_failed`, a message like
`unknown source kind "postgress"` and `fields: {"kind": "unknown"}`. Malformed YAML is a `400` with
`invalid YAML: <reason>`. An import that carries masked secrets keeps the values already stored, and
fails for objects being created for the first time, because there is nothing to keep. Status: 200,
400, 401, 403.

## End to end example

Create a push job, a token for it, and push an artifact from another machine.

```bash
export BACKVAULT_URL=https://backvault.example.com
export BACKVAULT_TOKEN=bvt_admin_token

SOURCE_ID=$(curl -sS -X POST "$BACKVAULT_URL/api/v1/sources" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name": "web01 uploads", "kind": "push", "config": {"expected_filename_extension": "tar.zst", "note": "Pushed nightly by backup.sh on web01"}}' | jq -r .id)

DEST_ID=$(curl -sS -X POST "$BACKVAULT_URL/api/v1/destinations" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name": "MinIO", "kind": "s3", "config": {"endpoint": "https://minio.internal:9000", "region": "us-east-1", "bucket": "backups", "prefix": "backvault", "access_key": "backvault", "secret_key": "s3cret", "path_style": true, "sse": "none", "part_size_mb": 16, "concurrency": 4}}' | jq -r .id)

curl -sS -X POST "$BACKVAULT_URL/api/v1/jobs" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"name\": \"web01 files\", \"slug\": \"web-files\", \"sourceId\": \"$SOURCE_ID\", \"destinationIds\": [\"$DEST_ID\"], \"schedule\": \"\", \"expectedIntervalMinutes\": 1440, \"compression\": \"zstd\", \"encryption\": \"none\", \"retention\": {\"keepLast\": 14}}"

PUSH_TOKEN=$(curl -sS -X POST "$BACKVAULT_URL/api/v1/tokens" \
  -H "Authorization: Bearer $BACKVAULT_TOKEN" -H 'Content-Type: application/json' \
  -d '{"name": "web01 push", "scopes": ["ingest"], "jobSlugs": ["web-files"]}' | jq -r .secret)

BACKVAULT_TOKEN=$PUSH_TOKEN scripts/backvault-push.sh --job web-files --file /tmp/web01.tar.zst --packed
```

The same flow from a browser session would send `-b cookies.txt -H 'X-Requested-With: backvault'`
instead of the `Authorization` header on every one of those `POST` calls.
