# Destinations

A destination is a place Backvault stores artifacts, and every job writes to one or more of them.

Each destination has a `kind` that selects a driver, and a `config` object validated against
that driver's field schema. The admin panel renders the form for a destination from
`GET /api/v1/meta/destinations`, so the fields listed on these pages are the fields you see
in the interface.

## Available destinations

| Kind | Label | What it needs |
|---|---|---|
| [`local`](local.md) | Local directory | A writable path on the Backvault host, usually a mounted disk or NFS share |
| [`s3`](s3.md) | S3-compatible storage | Bucket, plus an endpoint for anything that is not AWS, and a key pair unless the host already has AWS credentials |
| [`sftp`](sftp.md) | SFTP | Host, port, user, and either a password or a private key |
| [`webdav`](webdav.md) | WebDAV | Server URL, and a user and password where the share asks for one |

## Provider guides

| Guide | Covers |
|---|---|
| [Hetzner Storage Box](hetzner-storage-box.md) | Sub-accounts, port 23, SSH key upload, the WebDAV alternative |
| [S3 providers](s3-providers.md) | AWS, MinIO, Backblaze B2, Wasabi, Cloudflare R2, Hetzner Object Storage |

## How destinations are used

A backup run uploads the packed artifact to every destination on the job, one after another.
Each upload is retried according to the job's `retries` and `retryDelaySeconds`. If some
destinations succeed and others fail, the run finishes with status `warning` and Backvault
records artifacts only for the destinations that succeeded. If every destination fails, the
run fails.

One run that writes to two destinations produces two artifacts. They share the same run, the
same filename and the same sha256, because the checksum is taken of the packed file that is
uploaded, not of the raw dump.

After uploading, two further stages touch the destination:

- `verify`, when the job has verification enabled, calls `Stat` on each artifact and compares
  the size. See [restore and verification](../restore.md).
- `retention` lists and deletes older artifacts of the same job on the same destination. See
  [retention](../retention.md).

## Choosing destinations

Keep at least one copy off the machine that holds the data. A local directory on the same
server protects against a dropped table, not against a lost server. A common arrangement is a
local directory for fast restores and an S3 bucket or a Storage Box for the offsite copy, both
attached to the same job.

Encryption happens before upload, so the destination never sees plaintext when the job uses
age. See [encryption](../encryption.md).

## Testing a destination

Every destination driver implements a connection test. In the panel, use "Test connection" on
the destination form. Over the API:

```bash
curl -sS -X POST https://backvault.example.com/api/v1/destinations/dst_01j.../test \
  -H "Authorization: Bearer $BACKVAULT_TOKEN"
```

The answer is `{"ok": true, "message": "connection successful", "durationMs": 412}`, and a
failing test returns `ok: false` with the underlying error as the message, which is the fastest
way to tell a wrong key from a wrong bucket name. See
[troubleshooting](../troubleshooting.md).

What the test does depends on the driver. `local`, `sftp` and `webdav` prove that they can
write: they create the base directory if it is missing, write a small probe file whose name
starts with `.backvault-write-test` and delete it again. `s3` only checks that the bucket answers,
with `HeadBucket` and a one-key listing as a fallback, so it does not prove that uploads are
permitted. Nothing is read back in either case, and the whole test is given 30 seconds before it
is cancelled.
