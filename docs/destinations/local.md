# Local directory

Stores artifacts in a directory on the machine that runs Backvault, which is the simplest
destination and the fastest to restore from.

- Kind: `local`
- Needs: a directory the Backvault process can write to

Use it for the first copy of a backup, for a mounted external disk, or for an NFS or CIFS
share that is already attached to the host. Do not use it as your only destination: a
directory on the same server as the database it backs up does not survive that server.

## Configuration fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | path | yes | none | Absolute directory that holds the artifacts. Backvault creates it and any parent directories if they are missing. A relative path is rejected. |
| `dir_mode` | string | no | `0750` | Octal mode for the directories Backvault creates under `path`. |
| `file_mode` | string | no | `0640` | Octal mode applied to each finished artifact. |
| `prune_empty_dirs` | bool | no | `true` | After deleting an artifact, remove directories that became empty, up to `path` itself. |

`dir_mode`, `file_mode` and `prune_empty_dirs` sit in the advanced part of the form. Both mode
fields are parsed as octal, so `0640` and `640` mean the same thing and anything that is not an
octal number is refused when the destination is saved.

## Example

A destination as it appears in a `backvault export` document:

```yaml
destinations:
  - name: Local disk
    kind: local
    description: First copy on the backup volume
    config:
      path: /srv/backups
      dir_mode: '0750'
      file_mode: '0640'
      prune_empty_dirs: true
    tags:
      - onsite
```

## How Backvault writes objects

Artifacts are laid out under `path` as:

```text
<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]
```

The timestamp is UTC, `<ext>` comes from the source driver (`dump`, `sql`, `tar`, `archive`,
`rdb`, `sqlite`), and the suffixes appear only when the job compresses or encrypts. A files
job with slug `web-files`, zstd compression and age encryption produces:

```text
/srv/backups/web-files/web-files-20260917-020000.tar.zst.age
```

Writes are atomic. Backvault writes to a hidden temporary file in the target directory, named
`.<final name>.partial-<random>`, sets `file_mode` on it and renames it into place when the
transfer is complete, so a run that is cancelled or crashes never leaves a short file under the
final name. The rename is atomic only within one filesystem, which is why the temporary file is
created next to the final one rather than in `/tmp`. Directories created along the way get
`dir_mode`.

Deleting an object that is already gone is not an error. Retention treats a missing file as
already pruned and moves on, so a directory cleaned by hand does not break the next prune run.
With `prune_empty_dirs` on, a delete also removes the directories that just became empty,
walking up until it reaches `path` or hits a directory that still has content, so the job
directory disappears once its last artifact is pruned.

`List` walks the directory recursively and `Stat` reports size and modification time. Both are
used by verification and retention, so keep the directory free of unrelated files: everything
under `<path>/<jobSlug>/` is considered to belong to that job. The temporary `.partial-` files
of an upload in flight are skipped by `List`, so they are never taken for artifacts.

## Permissions

The directory must be writable by the user the Backvault service runs as, which is `backvault` for
both the systemd unit and the container image.

```bash
sudo install -d -o backvault -g backvault -m 0750 /srv/backups
```

Mode `0750` keeps the artifacts away from other local users, and it is also what `dir_mode`
uses for the directories Backvault creates below it. Each artifact ends up with `file_mode`,
`0640` by default. That matters when the job does not encrypt, because a plain dump is readable
by anyone who can read the file. See [security](../security.md).

For the container image, mount a host directory and keep the ownership right:

```bash
docker run -d --name backvault \
  -v backvault-data:/data \
  -v /srv/backups:/backups \
  -p 8080:8080 \
  ghcr.io/arthurr0/backvault:latest
```

The image runs as uid 1000, so `/srv/backups` on the host must be writable by uid 1000. Set
the destination `path` to `/backups`, the path inside the container, not the host path.

The systemd unit is hardened with `ProtectSystem=strict`, which makes the whole filesystem read
only apart from the paths named in `ReadWritePaths`. A local destination outside
`/var/lib/backvault` needs to be added there, otherwise uploads fail with a permission error even
though the directory looks writable:

```ini
[Service]
ReadWritePaths=/var/lib/backvault /srv/backups
```

Run `systemctl daemon-reload` and restart Backvault after changing the unit. See
[installation](../install.md).

## Gotchas

- **The same disk is not a backup.** A local destination protects against mistakes inside the
  database, not against the loss of the machine. Pair it with an offsite destination on the
  same job.
- **Free space is not checked in advance.** Backvault spools the packed artifact to the work
  directory first and then copies it, so a full disk fails the upload stage with an error from
  the filesystem. Watch free space on both the work directory and the destination.
- **The work directory and the destination can be the same disk.** During a run the artifact
  exists twice, once in `workDir` and once at the destination. Size the disk for that.
- **Network filesystems lie about atomicity.** On NFS a rename is atomic on the server, but a
  stale handle or a dropped mount surfaces as a failed upload rather than a silent truncation.
  Treat NFS and CIFS shares as remote destinations and turn verification on for the job.
- **Retention only sees what Backvault wrote.** Files placed in the job directory by other tools
  are listed but are not artifacts, and they are never pruned. Keep other content out of that
  directory.
- **Backing up Backvault itself needs a different target.** Do not point a local destination at
  the Backvault data directory. See the notes on backing up Backvault in [security](../security.md).
