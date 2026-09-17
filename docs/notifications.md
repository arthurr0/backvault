# Notifications

Backvault sends notifications through channels: email, a generic webhook, Slack, Discord, Telegram and
ntfy, each filtered by the events you care about.

## How a notification is decided

Three filters combine, and all three must allow an event before it is sent:

1. **The job.** A job lists the channels it uses in `notificationChannelIds`, and its `notifyOn`
   array filters which events those channels get. An empty `notifyOn` means the job uses
   `settings.defaultNotifyOn`, and an empty `notificationChannelIds` means every enabled channel is
   a candidate.
2. **The channel.** Each channel has its own `events` array. A channel with
   `["run.failed", "job.overdue"]` never sends a success message, whatever a job asks for. An empty
   `events` array accepts every event.
3. **Enabled.** A channel with `enabled: false` sends nothing.

In short: the event must be in the job's effective `notifyOn` list and in the channel's `events`
list. The intersection is what arrives.

A common arrangement is one noisy channel and one quiet one. An `#ops-backups` Slack channel
subscribed to everything, and an on call webhook subscribed to `run.failed`, `job.overdue` and
`artifact.missing` only.

## Event types

| Event | Severity | When |
|---|---|---|
| `run.success` | info | A backup or ingest run finished with status `success`, or a verify run found everything in place |
| `run.warning` | warning | A backup run finished with status `warning`: the backup exists but something went wrong, for example one destination of several failed |
| `run.failed` | error | A backup or ingest run finished with status `failed`, or a verify run failed outright. There is no new backup |
| `job.overdue` | warning | The overdue watcher noticed a job missed its schedule window, or a push job has not been fed within `expectedIntervalMinutes` |
| `artifact.missing` | warning | A verify run finished with status `warning`, which is what a missing or mismatched artifact produces |
| `prune.done` | info | A prune run finished with status `success` |
| `restore.done` | info | A restore run finished with status `success` |
| `restore.failed` | error | A restore run finished with status `failed` |

Those eight ids are the complete list. Runs that end in any other state send nothing: a failed
prune run, for example, produces no event at all.

If you subscribe to one event, subscribe to `run.failed`. If you subscribe to two, add
`job.overdue`: a job that silently stopped running produces no failures at all, which is exactly why
it goes unnoticed.

`settings.defaultNotifyOn` is a sensible default for every job that does not override it. It ships
as `["run.failed", "run.warning", "job.overdue"]`, which is quiet while things work, and
`artifact.missing` is the usual fourth entry to add.

## What a message contains

Every channel renders the same underlying event, so the wording is the same everywhere and only the
formatting differs:

- The title, which for a run event is `<site name>: <job name> <status>`, so several Backvault
  installations are distinguishable.
- A one line summary of the run: kind, status, duration, size, destinations and the error.
- A list of fields, in this order: Site, Job, Status (or Event when the notification has no run),
  Run type, Duration, Size, Raw size, Destinations, then anything extra the event carried.
- The error, when there is one, in a preformatted block.
- A link back to the run, built from `settings.baseUrl` as `<baseUrl>/runs/<runId>`, or
  `<baseUrl>/jobs/<slug>` for an event without a run. If the base URL is empty, messages carry no
  link, so set it. See [configuration.md](configuration.md).

Severity picks the colour used by Slack, Discord and the HTML mail: green for `run.success`,
`prune.done` and `restore.done`, amber for `run.warning`, `job.overdue` and `artifact.missing`, red
for `run.failed` and `restore.failed`.

## Channels

All channel configuration field names are snake_case, which is what the API expects and what the
dynamic forms in the panel render. Secret fields are stored encrypted and returned as `********`;
sending `********` back in an update keeps the stored value.

### Email

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `smtp_host` | string | yes | none | Hostname of the SMTP server. |
| `smtp_port` | port | no | `587` | 587 for STARTTLS, 465 for implicit TLS, 25 for an unencrypted relay. |
| `tls_mode` | select | no | `starttls` | Encryption: `starttls`, `tls` or `none`. |
| `username` | string | no | empty | Leave empty for a relay that does not require authentication. |
| `password` | secret | no | empty | Stored encrypted, returned as `********`. |
| `from` | string | yes | none | Sender address, for example `Backvault <backups@example.com>`. |
| `to` | list | yes | none | One recipient per line. |
| `subject_prefix` | string | no | empty | Optional text put in front of every subject line. |
| `skip_tls_verify` | bool | no | `false` | Accept a certificate that does not verify. |
| `allow_insecure_auth` | bool | no | `false` | Send the password over a plain connection. Only for a relay on localhost or a trusted private network. |
| `helo_name` | string | no | empty | Name announced to the SMTP server. Defaults to the hostname of this machine. |
| `timeout` | int | no | `30` | Timeout in seconds for the whole SMTP conversation. |

```json
{
  "name": "Ops mailbox",
  "kind": "email",
  "enabled": true,
  "events": ["run.failed", "run.warning", "job.overdue", "artifact.missing"],
  "config": {
    "smtp_host": "smtp.example.com",
    "smtp_port": 587,
    "tls_mode": "starttls",
    "username": "backvault@example.com",
    "password": "********",
    "from": "Backvault <backups@example.com>",
    "to": ["ops@example.com", "dba@example.com"],
    "subject_prefix": "[Backvault]"
  }
}
```

The message is `multipart/alternative`, plain text and HTML. The subject is the message title with
`subject_prefix` and a space in front of it, for example `[Backvault] Acme Backups: shop-db failed`,
and it carries an `X-Backvault-Event` header with the run status. Mail filters key off the subject, so
it is stable.

With a provider that requires an app password, use that, not the account password. `tls_mode: none`
sends everything in clear, use it only for a relay on localhost, and note that authentication over
an unencrypted connection is refused unless you also turn on `allow_insecure_auth`.

### Webhook

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `url` | string | yes | none | Target URL, `http` or `https`, https in production. |
| `method` | select | no | `POST` | `POST`, `PUT` or `PATCH`. |
| `headers` | list | no | empty | Extra headers, one `Key: Value` per line. |
| `secret` | secret | no | empty | Signing key for `X-Backvault-Signature`. |
| `timeout` | int | no | `15` | Request timeout in seconds. |

Every request carries `Content-Type: application/json`, `User-Agent: Backvault`, `X-Backvault-Event` with
the event type and `X-Backvault-Timestamp` with the event time as Unix seconds, plus your own
`headers` and the signature when a secret is set.

The body is the event as JSON:

```json
{
  "type": "run.failed",
  "severity": "error",
  "title": "Acme Backups: Shop database failed",
  "message": "backup run of Shop database finished with status failed after 31s. Destinations: Hetzner box, MinIO. Error: pg_dump exited with code 1",
  "time": "2026-09-17T02:00:31Z",
  "siteName": "Acme Backups",
  "baseUrl": "https://backvault.example.com",
  "job": {
    "id": "01JJOB0000000000000000000",
    "slug": "shop-db",
    "name": "Shop database",
    "sourceKind": "postgres",
    "destinationNames": ["Hetzner box", "MinIO"]
  },
  "run": {
    "id": "01JRUN0000000000000000000",
    "jobId": "01JJOB0000000000000000000",
    "jobSlug": "shop-db",
    "jobName": "Shop database",
    "kind": "backup",
    "trigger": "schedule",
    "status": "failed",
    "queuedAt": "2026-09-17T02:00:00Z",
    "startedAt": "2026-09-17T02:00:00Z",
    "finishedAt": "2026-09-17T02:00:31Z",
    "durationMs": 31284,
    "bytes": 0,
    "rawBytes": 0,
    "attempt": 2,
    "error": "pg_dump exited with code 1"
  },
  "fields": {
    "job": "Shop database",
    "kind": "backup",
    "status": "failed",
    "duration": "31s",
    "size": "0 B",
    "destinations": "Hetzner box, MinIO",
    "error": "pg_dump exited with code 1"
  },
  "link": "https://backvault.example.com/runs/01JRUN0000000000000000000"
}
```

`job` and `run` are the full objects from the API, so everything in [api.md](api.md) is available to
your handler. `fields` holds preformatted strings for display. Treat any field as optional: `job`,
`run`, `fields` and `link` are all omitted when empty, and a `job.overdue` event has no run at all.

#### Signature

When `secret` is set, the request carries:

```text
X-Backvault-Signature: <lowercase hex HMAC-SHA256 of the exact request body, keyed with the secret>
```

Verify against the raw body bytes, before any JSON parsing, and compare in constant time.

In bash:

```bash
#!/usr/bin/env bash
set -Eeuo pipefail

body=$(cat)
expected=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$BACKVAULT_WEBHOOK_SECRET" -r | awk '{print $1}')

if [ "$expected" = "$HTTP_X_BACKVAULT_SIGNATURE" ]; then
  printf 'ok\n'
else
  printf 'signature mismatch\n' >&2
  exit 1
fi
```

In Python, with Flask:

```python
import hmac
import hashlib
import os

from flask import Flask, request, abort

app = Flask(__name__)
SECRET = os.environ["BACKVAULT_WEBHOOK_SECRET"].encode()

@app.post("/backvault")
def backvault_event():
    signature = request.headers.get("X-Backvault-Signature", "")
    expected = hmac.new(SECRET, request.get_data(), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(expected, signature):
        abort(401)

    event = request.get_json()
    if event["type"] in ("run.failed", "job.overdue"):
        page_on_call(event["title"], event.get("link", ""))
    return "", 204
```

Reject unsigned requests when you configured a secret. A webhook endpoint that accepts anything is
an open door into your alerting.

### Slack

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `webhook_url` | secret | yes | none | Incoming webhook URL from a Slack app. |
| `username` | string | no | empty | Bot name shown on the message. |
| `icon_emoji` | string | no | empty | Icon shown on the message, for example `:lock:`. |
| `channel` | string | no | empty | Channel override, for example `#alerts`. Only works for legacy webhooks that allow it. |
| `timeout` | int | no | `15` | Request timeout in seconds. |

```json
{
  "name": "#ops-backups",
  "kind": "slack",
  "events": ["run.failed", "run.warning", "job.overdue"],
  "config": {
    "webhook_url": "********",
    "username": "Backvault",
    "icon_emoji": ":lock:"
  }
}
```

Messages arrive as a single attachment with a colour bar: green for success, amber for warning, red
for failure. The title is the message title and links to the run, the summary is the attachment
text, and site, job, status, duration, size and destinations are fields below it. An error is
appended as a fenced field, truncated at 1500 characters. Create the webhook under **Incoming
Webhooks** in your Slack app settings, and note that the URL is the credential: anyone who has it
can post to that channel. The channel is fixed when the webhook is created, which is why `channel`
only works for older webhooks.

### Discord

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `webhook_url` | secret | yes | none | Channel webhook URL from Channel settings, Integrations, Webhooks. |
| `username` | string | no | empty | Bot name shown on the message. |
| `avatar_url` | string | no | empty | Avatar shown on the message. |
| `timeout` | int | no | `15` | Request timeout in seconds. |

The message is an embed, coloured by severity, with the same fields as Slack and the title linking
to the run. Discord rate limits webhooks per channel, so a burst of failures across many jobs may be
delayed rather than dropped.

### Telegram

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `bot_token` | secret | yes | none | Token from @BotFather, in the form `123456789:AA...`. |
| `chat_id` | string | yes | none | Numeric id of the chat, group or channel. |
| `message_thread_id` | string | no | empty | Topic id, only for forum groups with topics. |
| `disable_notification` | bool | no | `false` | Deliver the message without a sound. |
| `api_base` | string | no | `https://api.telegram.org` | Change only when you run a local Bot API server. |
| `timeout` | int | no | `15` | Request timeout in seconds. |

```json
{
  "name": "Telegram on call",
  "kind": "telegram",
  "events": ["run.failed", "job.overdue"],
  "config": {
    "bot_token": "********",
    "chat_id": "-1001234567890",
    "disable_notification": false
  }
}
```

To find a group id, add the bot to the group, send a message, then read
`https://api.telegram.org/bot<token>/getUpdates`. Group ids are negative. The bot must be a member
of the group, and for a channel it must be an administrator.

Messages are sent with `parse_mode: HTML` and every value escaped, so a database name containing an
underscore or an angle bracket does not break formatting or swallow part of the message. Link
previews are disabled, the error block is cut at 1500 characters and the whole message at 4000.
Delivery errors are scrubbed of the bot token before they are stored on the channel.

### ntfy

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `server_url` | string | no | `https://ntfy.sh` | Base URL of the ntfy server. |
| `topic` | string | yes | none | Topic name, without spaces or slashes. |
| `token` | secret | no | empty | Bearer token for a protected topic. |
| `tags` | list | no | empty | Emoji short codes or plain words added to every message, one per line. |
| `priority_success` | select | no | `low` | Priority for info events: `min`, `low`, `default`, `high` or `urgent`. |
| `priority_warning` | select | no | `default` | Priority for warnings, same values. |
| `priority_error` | select | no | `high` | Priority for failures, same values. |
| `timeout` | int | no | `15` | Request timeout in seconds. |

```json
{
  "name": "Phone",
  "kind": "ntfy",
  "events": ["run.failed", "job.overdue"],
  "config": {
    "server_url": "https://ntfy.example.com",
    "topic": "backvault-prod",
    "token": "********",
    "tags": ["floppy_disk"],
    "priority_success": "min",
    "priority_error": "urgent"
  }
}
```

Priority follows severity through the three priority fields, so with the defaults a failed backup
arrives as `high` and breaks through a quiet phone while a successful one arrives as `low`. Backvault
also adds a tag per severity, `white_check_mark`, `warning` or `rotating_light`, in front of your
own `tags`, and sets the run link as the notification click action. On the public `ntfy.sh` server a
topic name is the only access control, so pick something unguessable, or self host and use a token.

## Creating and testing a channel

In the panel, open **Notifications** and add a channel. The form is generated from the driver
schema, so it shows exactly the fields above, with secrets masked once saved.

Test before you rely on it. The test sends a real `run.success` event titled
`Backvault test notification` through the real transport, with no job and no run attached:

```bash
curl -X POST -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  "https://backvault.example.com/api/v1/notifications/channels/$CHANNEL_ID/test"
```

To test a configuration you have not saved yet, post the whole channel body instead:

```bash
curl -X POST -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"kind":"slack","config":{"webhook_url":"https://hooks.slack.com/services/..."}}' \
  "https://backvault.example.com/api/v1/notifications/channels/test"
```

A channel comes back from the API as `id`, `name`, `kind`, `config`, `enabled`, `events`,
`createdAt` and `updatedAt`, plus `lastSentAt` and `lastError` once it has been used. Both are
visible in the list, so a channel that stopped working is easy to spot without digging through logs.

Channels are part of `backvault export` as well, under `channels`, with the same snake_case config:

```yaml
channels:
  - name: Ops mailbox
    kind: email
    enabled: true
    events:
      - run.failed
      - run.warning
      - job.overdue
    config:
      smtp_host: smtp.example.com
      smtp_port: 587
      tls_mode: starttls
      username: backvault@example.com
      password: '********'
      from: Backvault <backups@example.com>
      to:
        - ops@example.com
      subject_prefix: '[Backvault]'
```

## Attaching channels to a job

In the job editor, under Notifications, pick the channels and optionally narrow the events. From the
API:

```json
{
  "notificationChannelIds": ["01JCHN0000000000000000000"],
  "notifyOn": ["run.failed", "run.warning"]
}
```

Leave `notifyOn` empty to follow `settings.defaultNotifyOn`, which is the option that keeps a large
installation consistent. In an export the same job refers to channels by name, under
`notificationChannels`.

## When nothing arrives

| Symptom | Check |
|---|---|
| No message at all | Is the channel enabled, is it attached to the job, do the event lists intersect |
| Success messages only | The event you expect is missing from the channel's `events` |
| Messages without a link | `settings.baseUrl` is empty |
| Email rejected | `tls_mode` does not match the port, or the relay refuses the `from` address |
| Email authentication refused | The relay is unencrypted and `allow_insecure_auth` is off |
| Webhook 401 from your side | Signature verified against a parsed body instead of the raw bytes |
| Nothing after a host move | The channel's `lastError` field, and [troubleshooting.md](troubleshooting.md) |

Failing to deliver a notification never fails the backup. The error is recorded on the channel and
in the run log, and the run keeps its own status.
