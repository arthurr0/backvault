# WebDAV

Stores artifacts on a WebDAV share, which covers Nextcloud, ownCloud and the WebDAV interface
of a Hetzner Storage Box.

- Kind: `webdav`
- Needs: the URL of the endpoint, plus a user and a password where the share asks for one

WebDAV is the right choice when a service offers nothing better, for example a Nextcloud
instance you already run. Where the same storage is reachable over SFTP or S3, prefer those:
they handle large files and resumed transfers better, and they report checksums.

## Configuration fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `url` | string | yes | none | Full URL of the WebDAV endpoint, for example `https://cloud.example.com/remote.php/dav/files/backup`. It must be an `http` or `https` URL. |
| `user` | string | no | empty | Login name. |
| `password` | secret | no | empty | Password or app password. Stored encrypted, returned as `********`. |
| `base_path` | path | no | empty | Folder below the server URL that holds the artifacts. It is created if missing. |
| `skip_tls_verify` | bool | no | `false` | Accept any TLS certificate from the server. Only for self-signed certificates on a trusted network. |
| `timeout` | int | no | `60` | Seconds allowed for a metadata request: `PROPFIND`, `MKCOL`, `DELETE` and the connection test. Uploads and downloads are not limited by it. |

`password` is the only secret field. Sending exactly `********` in an update keeps the stored
value.

`user` and `password` are both optional, and Backvault only sends basic authentication when at
least one of them is set, so a share that needs no login works with both left empty. On any
real server, set them. `skip_tls_verify` and `timeout` sit in the advanced part of the form.

## Example

A destination as it appears in a `backvault export` document:

```yaml
destinations:
  - name: Nextcloud
    kind: webdav
    description: Backups folder on the company Nextcloud
    config:
      url: https://cloud.example.com/remote.php/dav/files/backup
      user: backup
      password: '********'
      base_path: backvault
      timeout: 60
    tags:
      - offsite
```

## How Backvault writes objects

The remote path is the base path followed by the artifact path:

```text
<base_path>/<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]
```

The timestamp is UTC. Missing collections are created with `MKCOL`, one level at a time,
because WebDAV has no recursive create. Creating a collection that exists returns 405 from most
servers, which Backvault treats as success rather than as an error.

Uploads use a single `PUT`. The file is written to `<name>.partial` and moved to `<name>` with
`MOVE` and `Overwrite: T` once the transfer completes, so an interrupted run does not leave a
short file under the final name. When the `PUT` or the `MOVE` fails, Backvault deletes the
`.partial` file itself, and a `.partial` left on the server is skipped by listing, so retention
never mistakes one for an artifact.

Deleting something that is not there is not an error: a 404 from `DELETE` is treated as already
deleted, so retention is safe to re-run. `PROPFIND` with `Depth: 1` provides listing for
retention and the destination browser, one collection at a time down the tree, and a `PROPFIND`
with `Depth: 0` backs `Stat`. The `getcontentlength` it reports is what verification compares
with the recorded size, and that size is the whole comparison.

## Permissions

The account needs to create collections and to read, write and delete inside `base_path`.
On Nextcloud that is an ordinary user with write access to the folder.

Use an app password rather than the account password, so that revoking Backvault's access does not
mean changing the password everywhere:

1. Sign in as the backup user.
2. Open Settings, then Security.
3. Create a new app password named `backvault` and copy the generated value into the `password`
   field.

If the Nextcloud instance enforces two factor authentication, an app password is the only thing
that works, since the WebDAV endpoint cannot prompt for a second factor.

Do not enable server side encryption for the folder on a Nextcloud that also serves these files
over other protocols, because it changes how the file is stored and read back, and a partially
supported combination is discovered at restore time rather than at backup time. Backvault's own
age encryption is applied before upload and is independent of anything the server does. See
[encryption](../encryption.md).

## Gotchas

- **Large files are the weak point.** A single `PUT` of tens of gigabytes through PHP is
  fragile, and many servers and reverse proxies cap the request body size. On Nextcloud raise
  `client_max_body_size` in nginx and `upload_max_filesize` plus `post_max_size` in PHP, or keep
  large jobs on an SFTP or S3 destination.
- **The base URL differs per product.** Nextcloud and ownCloud use
  `https://host/remote.php/dav/files/<user>`. A Hetzner Storage Box uses
  `https://u123456.your-storagebox.de`. Getting this wrong produces a 401 or a 404 on the very
  first request, which the connection test reports.
- **Timeouts show up as failed uploads.** A reverse proxy with a 60 second timeout kills a slow
  upload halfway. Raise the proxy timeouts for the WebDAV location, see
  [installation](../install.md). Backvault's own `timeout` field is not the one to raise for that:
  it bounds metadata requests only, and a `PUT` or a `GET` runs without a deadline of its own.
- **Checksums are usually absent.** Most WebDAV servers report only size and modification time,
  so verification compares size alone. Keep the job's sha256 as the real integrity check, which
  is what a download verifies against anyway.
- **No parallel range downloads.** Restoring a large artifact is one long sequential read. Plan
  the restore window accordingly, see [restore](../restore.md).
- **Quota errors arrive as 507.** Backvault reports them as a failed upload with the server
  message. Nextcloud also counts trashed files against the quota until the trash is emptied, so
  a folder that looks small can still be over quota.
