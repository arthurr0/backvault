# Push from a remote script

A placeholder source for artifacts that arrive from somewhere else. Backvault never connects
out for a push job: a script on the other host uploads the finished dump through the ingest
API.

Kind `push`. Extension derived from the uploaded filename. Requires nothing.
Capabilities: test. This driver has no backup and no restore of its own.

Use it when the machine holding the data cannot be reached from Backvault: a laptop, a host
behind NAT, a customer network, or a database that must not accept inbound connections.

## Configuration fields

Options:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `expected_filename_extension` | string | no | `bin` | Hint for the artifact name when the pushing client sends no `X-Backvault-Filename` header. It never overrides a filename that was sent. |
| `note` | text | no | | Free text describing who pushes to this job and from where. It is shown in the panel and changes nothing. |

Neither field affects how an upload is handled. Everything that makes a push job work lives
on the job:

| Job field | Description |
|---|---|
| `slug` | Part of the ingest URL, `POST /api/v1/ingest/<slug>`. |
| `expectedIntervalMinutes` | How often an upload is expected. A push job whose last successful run is older than this is marked overdue and raises `job.overdue`. |
| `schedule` | Ignored. Push jobs are never scheduled. |
| `compression`, `encryption` | Applied to uploads that arrive without `?packed=1`. Uploads marked packed are stored exactly as sent. |
| `retention` | Applied to the artifacts the uploads produce, like any other job. |

## Example

```yaml
sources:
  - name: Laptop documents
    kind: push
    config:
      expected_filename_extension: tar.zst
      note: Pushed nightly by backup.sh on the laptop

jobs:
  - name: Laptop documents
    slug: laptop-documents
    sourceName: Laptop documents
    destinationNames:
      - Storage Box
    schedule: ""
    expectedIntervalMinutes: 1440
    retention:
      keepLast: 7
      keepMonthly: 6
```

## Uploading

An API token with the `ingest` scope, optionally restricted to this job slug, is all the
remote host needs. Create one under **Settings, API tokens**.

With curl:

```bash
curl --request POST \
  --header "Authorization: Bearer $BACKVAULT_TOKEN" \
  --header "X-Backvault-Filename: laptop-20260917-020000.tar.zst" \
  --header "X-Backvault-Sha256: $(sha256sum backup.tar.zst | cut -d' ' -f1)" \
  --upload-file backup.tar.zst \
  "https://backvault.example.com/api/v1/ingest/laptop-documents?packed=1"
```

With the script, which computes the checksum, retries with backoff and prints the run id:

```bash
scripts/backvault-push.sh --job laptop-documents --file backup.tar.zst --packed
```

Or straight from a producer, through a pipe:

```bash
tar --create --file - /home/me/documents | zstd -q \
  | scripts/backvault-push.sh --job laptop-documents --name laptop-20260917-020000.tar.zst --packed
```

The job detail page in the panel carries a **How to push** panel with these commands, with
the real URL and slug filled in.

## Restore

There is no `source` restore: Backvault has no way back into a host that it cannot reach.

`download` and `path` work as usual, and give you exactly what was uploaded, decrypted and
decompressed unless you ask for `?raw=1`. What you do with it depends on what the remote
script produced, so keep the producing command next to the job description.

An upload sent with `?packed=1` is stored byte for byte. If the remote script encrypted it
with `age` and a passphrase that Backvault does not hold, Backvault cannot unpack it either.
Either give the job the same passphrase, or accept that the artifact is opaque to Backvault
and unpack it yourself. See [../encryption.md](../encryption.md).

## Gotchas

**`?packed=1` matters.** With it, Backvault skips its pack stage and reads the compression and
encryption from the filename suffixes. Without it, the job's own compression and encryption
are applied to what you sent. Sending an already compressed file without the flag means
compressing twice, which mostly wastes time.

**The filename carries the extension.** `X-Backvault-Filename` is how Backvault learns that the
upload is a `dump`, `tar` or `sql`. Send a name in the same shape as the artifacts Backvault
produces, including the timestamp.

**Send the checksum.** `X-Backvault-Sha256` is verified after spooling. A mismatch fails the
run, which is how a truncated upload is caught instead of being stored.

**Overdue detection is the point.** A push job without
`expectedIntervalMinutes` never reports that the remote script stopped running, and a
backup that silently stops is the failure mode this product exists to prevent. Set it to a
little more than the real interval, so one skipped run does not raise an alert but two do.

**Restrict the token to the job.** An `ingest` token limited to one slug cannot write into
other jobs, which matters when the token lives on a machine you do not control.

**Clock skew shows up in filenames.** Backvault records the time it received the upload, but
the filename is the one you sent. Generate it in UTC.

## See also

- [../push-and-ingest.md](../push-and-ingest.md) for tokens, headers, overdue detection and
  cron examples.
- [../scripts.md](../scripts.md) for every flag of `backvault-push.sh` and the backup scripts
  that call it.
- [../api.md](../api.md) for the ingest endpoint and its responses.
