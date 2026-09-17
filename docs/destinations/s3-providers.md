# S3 providers

Endpoint, region and addressing settings for the S3 services Backvault is used with, with a ready
to paste destination config and the matching environment block for the standalone scripts.

The [S3 destination page](s3.md) covers the fields themselves, the object layout and the IAM
policy. This page is the per provider reference.

## Summary

| Provider | Endpoint | Region | Path style | Storage classes |
|---|---|---|---|---|
| AWS S3 | empty, derived from the region | the real region, for example `eu-central-1` | no | full set |
| MinIO | `https://minio.example.com:9000` | `us-east-1` unless configured otherwise | yes | none by default |
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | `us-west-004` and similar | no | none |
| Wasabi | `https://s3.<region>.wasabisys.com` | `eu-central-1` and similar | no | none |
| Cloudflare R2 | `https://<account-id>.r2.cloudflarestorage.com` | `auto` | yes | none |
| Hetzner Object Storage | `https://<location>.your-objectstorage.com` | the location, for example `fsn1` | yes | none |

Path style means `endpoint/bucket/key`. Virtual host style means `bucket.endpoint/key` and
needs wildcard DNS for the endpoint. In the standalone scripts, `S3_PATH_STYLE=auto` turns path
style on for any endpoint that is not an AWS endpoint, which matches the table above.

Server-side encryption is the `sse` field, one of `none`, `AES256` or `aws:kms`. Only AWS takes
`aws:kms`, and then `sse_kms_key_id` names the key. Everywhere else leave `sse` at `none` and
use Backvault's own age encryption, which the provider cannot read at all, see
[encryption](../encryption.md). In the standalone scripts the matching variable is `S3_SSE`,
which takes the header value directly, for example `AES256`.

## AWS S3

Leave the endpoint empty. The region determines both the endpoint and the signature.

```yaml
destinations:
  - name: AWS offsite
    kind: s3
    config:
      endpoint: ''
      region: eu-central-1
      bucket: acme-backups
      prefix: backvault/prod
      access_key: AKIAEXAMPLE
      secret_key: '********'
      path_style: false
      storage_class: STANDARD_IA
      sse: AES256
```

```ini
S3_ENDPOINT=
S3_REGION=eu-central-1
S3_BUCKET=acme-backups
S3_PREFIX=backvault/prod/db-prod
S3_ACCESS_KEY=AKIAEXAMPLE
S3_SECRET_KEY=...
S3_PATH_STYLE=0
S3_STORAGE_CLASS=STANDARD_IA
S3_SSE=AES256
```

Storage classes: `STANDARD`, `STANDARD_IA`, `ONEZONE_IA`, `INTELLIGENT_TIERING`, `GLACIER_IR`,
`GLACIER` and `DEEP_ARCHIVE`. `STANDARD_IA` and `GLACIER_IR` can be read immediately and are
sensible for backups. `GLACIER` and `DEEP_ARCHIVE` require a restore request at AWS before the
object can be downloaded, which means a Backvault restore of that artifact fails until the thaw is
finished, and verification reports the object as unreadable in the meantime.

`STANDARD_IA` and the archive classes bill a minimum storage duration (30 days for `STANDARD_IA`,
longer for the archive classes). Deleting an object sooner still costs the minimum, so align
retention with the class you pick, see [retention](../retention.md).

Use a dedicated IAM user or role with the policy from [the S3 page](s3.md). Turn on bucket
versioning only if you have a lifecycle rule that expires noncurrent versions, otherwise
retention frees no space.

## MinIO

Self-hosted, and the reference implementation for an S3 endpoint that is not AWS. It needs path
style addressing: virtual host style requires wildcard DNS, so it fails against a bare IP
address or a plain hostname.

```yaml
destinations:
  - name: MinIO
    kind: s3
    config:
      endpoint: https://minio.example.com:9000
      region: us-east-1
      bucket: backups
      prefix: backvault
      access_key: backvault
      secret_key: '********'
      path_style: true
      storage_class: ''
      sse: none
```

```ini
S3_ENDPOINT=https://minio.example.com:9000
S3_REGION=us-east-1
S3_BUCKET=backups
S3_PREFIX=backvault/db-prod
S3_ACCESS_KEY=backvault
S3_SECRET_KEY=...
S3_PATH_STYLE=1
```

The region defaults to `us-east-1` unless the server sets `MINIO_REGION`. The value has to match
the server, because Signature V4 signs it.

Create a bucket and a scoped user with the MinIO client:

```bash
mc alias set local https://minio.example.com:9000 admin "$MINIO_ROOT_PASSWORD"
mc mb local/backups
mc admin user add local backvault "$BACKVAULT_SECRET"
mc admin policy attach local readwrite --user backvault
```

For a stricter policy, write one that allows `s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`
on `arn:aws:s3:::backups/backvault/*` and `s3:ListBucket` on `arn:aws:s3:::backups`, as on
[the S3 page](s3.md).

The image on Docker Hub (`minio/minio`) is no longer pullable anonymously. Use the Quay image,
which is what the `demo` profile in `deploy/docker-compose.yml` does:

```bash
docker run -d --name minio -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=backvault -e MINIO_ROOT_PASSWORD=backvault-demo-secret \
  quay.io/minio/minio:latest server /data --address :9000 --console-address :9001
```

A local MinIO over plain HTTP is fine for trying Backvault out. For anything real, put it behind
TLS, since Signature V4 protects the request but not the body on the wire.

## Backblaze B2

Use the S3 compatible endpoint. The native B2 API is a different protocol and does not work with
this driver.

Find the endpoint in the B2 console next to the bucket, in the form
`s3.us-west-004.backblazeb2.com`. The region is the middle part of that hostname.

```yaml
destinations:
  - name: Backblaze B2
    kind: s3
    config:
      endpoint: https://s3.us-west-004.backblazeb2.com
      region: us-west-004
      bucket: acme-backups
      prefix: backvault/prod
      access_key: 004abcdef0123456789000000001
      secret_key: '********'
      path_style: false
      storage_class: ''
      sse: none
```

```ini
S3_ENDPOINT=https://s3.us-west-004.backblazeb2.com
S3_REGION=us-west-004
S3_BUCKET=acme-backups
S3_PREFIX=backvault/prod/db-prod
S3_ACCESS_KEY=004abcdef0123456789000000001
S3_SECRET_KEY=...
S3_PATH_STYLE=auto
```

Create an application key restricted to the one bucket rather than a master key. The key id is
the access key and the application key is the secret.

B2 has no storage classes, so leave `storage_class` empty. Lifecycle rules live on the bucket in
the B2 console. Note that B2 keeps hidden versions of deleted files until a lifecycle rule
removes them, so "keep only the last version" is the setting that makes Backvault retention free
actual space.

## Wasabi

```yaml
destinations:
  - name: Wasabi
    kind: s3
    config:
      endpoint: https://s3.eu-central-1.wasabisys.com
      region: eu-central-1
      bucket: acme-backups
      prefix: backvault/prod
      access_key: AKIAEXAMPLE
      secret_key: '********'
      path_style: false
      storage_class: ''
      sse: none
```

```ini
S3_ENDPOINT=https://s3.eu-central-1.wasabisys.com
S3_REGION=eu-central-1
S3_BUCKET=acme-backups
S3_PREFIX=backvault/prod/db-prod
S3_ACCESS_KEY=AKIAEXAMPLE
S3_SECRET_KEY=...
S3_PATH_STYLE=auto
```

The endpoint hostname contains the region, and the two must agree. There are no storage classes.

Wasabi bills a minimum storage duration per object. Deleting an artifact before that window is
over is allowed, and it still costs the remainder. Frequent backups with aggressive retention
therefore cost more than the stored size suggests. Pick a retention policy that keeps daily
artifacts at least as long as the minimum duration, or accept the difference knowingly.

## Cloudflare R2

The endpoint contains the account id, and the region is the literal string `auto`.

```yaml
destinations:
  - name: Cloudflare R2
    kind: s3
    config:
      endpoint: https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com
      region: auto
      bucket: acme-backups
      prefix: backvault/prod
      access_key: 0123456789abcdef
      secret_key: '********'
      path_style: true
      storage_class: ''
      sse: none
```

```ini
S3_ENDPOINT=https://0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com
S3_REGION=auto
S3_BUCKET=acme-backups
S3_PREFIX=backvault/prod/db-prod
S3_ACCESS_KEY=0123456789abcdef
S3_SECRET_KEY=...
S3_PATH_STYLE=1
```

R2 does not accept the AWS storage class values, so leave `storage_class` empty. It has no
egress fees, which makes it attractive for backups that are restored occasionally to somewhere
else.

Create the credential as an R2 API token scoped to the single bucket with object read and write.
The token page shows the access key id and secret once.

## Hetzner Object Storage

Buckets live in a location, and the endpoint is built from it: `fsn1`, `nbg1` or `hel1`.

```yaml
destinations:
  - name: Hetzner Object Storage
    kind: s3
    config:
      endpoint: https://fsn1.your-objectstorage.com
      region: fsn1
      bucket: acme-backups
      prefix: backvault/prod
      access_key: HETZNEREXAMPLE
      secret_key: '********'
      path_style: true
      storage_class: ''
      sse: none
```

```ini
S3_ENDPOINT=https://fsn1.your-objectstorage.com
S3_REGION=fsn1
S3_BUCKET=acme-backups
S3_PREFIX=backvault/prod/db-prod
S3_ACCESS_KEY=HETZNEREXAMPLE
S3_SECRET_KEY=...
S3_PATH_STYLE=1
```

Credentials are created per project in the Hetzner Cloud console. There are no storage classes.
Use path style: a bucket name containing a dot breaks certificate validation in virtual host
style, and path style avoids the question entirely.

This is a different product from a [Hetzner Storage Box](hetzner-storage-box.md), which speaks
SFTP and WebDAV and has no S3 API.

## Choosing between them

- Restores are what matter. A provider with no egress fees or with instant retrieval makes a
  restore a decision rather than a budget question.
- Keep the offsite copy at a different company from the one that hosts the data. A bucket in the
  same account as the server it backs up shares a blast radius with it.
- Encrypt with age when the provider is not yours, so the bucket holds ciphertext even if the
  credential leaks, see [encryption](../encryption.md).
- Whatever you pick, restore from it once before you rely on it, see [restore](../restore.md).
