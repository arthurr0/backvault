# Files and directories

Packs a list of paths into a tar archive. The archive is built in Go, so this driver needs
no external tools and works the same on every host.

Kind `files`. Extension `tar`. Requires nothing beyond read access to the paths.
Capabilities: test, restore, remote.

## Configuration fields

Source:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `paths` | list | yes | | Absolute files and directories to archive, one per line. Directories are archived recursively. |
| `base_dir` | path | no | | Absolute directory the archive entries are stored relative to. Leave it empty to store each path under its own name, like `tar -C parent`. |

Options:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `exclude` | list | no | | Glob patterns matched against the path inside the archive, one per line. `**` matches across directories. A pattern without a slash also matches any file name. |
| `follow_symlinks` | bool | no | `false` | Archive the contents a symlink points at instead of the link itself. Loops are detected and skipped. |
| `one_file_system` | bool | no | `false` | Skip anything that lives on a different mount than the path it was reached from. |
| `strict` | bool | no | `false` | By default unreadable files are skipped with a warning. Turn this on to fail the whole backup instead. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `restore_ownership` | bool | no | `false` | Apply the stored user and group on restore. Needs root; failures are logged and ignored. |

## Example

```yaml
sources:
  - name: Web root and nginx config
    kind: files
    config:
      paths:
        - /srv/www
        - /etc/nginx
      exclude:
        - "**/cache/**"
        - "**/node_modules/**"
        - "*.log"
      one_file_system: true
      strict: false
```

With `base_dir`, the same source stores shorter paths. The entries in `paths` stay
absolute, `base_dir` only decides what they are stored relative to:

```yaml
sources:
  - name: Web root
    kind: files
    config:
      base_dir: /srv
      paths:
        - /srv/www
      exclude:
        - "**/cache/**"
```

That archive holds `www/index.html` rather than `srv/www/index.html`. A path outside
`base_dir` is stored under its own base name instead, so keep the two consistent.


## Run on a host

Pick a host in the **Run on** select and the archive is built by `tar` on that machine and
streamed back over SSH. The paths are read on the host, so `/srv/www` means the host's
`/srv/www`. The command is:

```
tar -C <base_dir or /> -cf - [--one-file-system] [-h] --exclude=<pattern>... <paths>
```

Three differences from the Go-native local mode are worth knowing:

- The host needs `tar`. Any reasonably complete GNU or BSD tar works.
- Without a `base_dir` the archive is relative to `/`, so `/var/www/html` is stored as
  `var/www/html`. Set `base_dir` when you want shorter entries, exactly as you would locally.
- Exclude patterns are handed to `tar --exclude` unchanged, so they follow that tool's glob
  rules rather than the Go matcher. `**/cache/**` and `*.log` behave the same way in both,
  more exotic patterns may not.

**Test connection** checks that `tar` exists on the host and that every configured path is
readable there, and lists the ones that are not.

GNU tar exits with a non-zero status when a file changes while it is being read, and the run
then fails at the end of the dump with that message in the log. Exclude the paths that change
constantly, or back them up with the driver that owns them.

A restore into a host runs `mkdir -p <target> && tar -C <target> -xf -` on it, with
`--no-same-owner` unless `restore_ownership` is on. The target directory is a path on the
host, not on the Backvault server.

## Restore

`download` gives you an ordinary tar archive once it is unpacked:

```bash
tar -tvf www-20260917-020000.tar
tar -xf www-20260917-020000.tar -C /tmp/check
```

`path` writes the archive to `targetPath` on the Backvault host, or, with `extract: true`,
extracts it into that directory. Extraction into a directory that already holds files
overwrites matching entries and leaves everything else alone, so a partial restore of one
subdirectory is a matter of extracting into a scratch directory first and copying what you
need.

`source` extracts the archive back over the configured paths on the Backvault host, using
`base_dir` the same way the backup did. Only use it when you mean to overwrite the live
files.

## Gotchas

**Permissions decide what ends up in the archive.** Backvault reads as the user the service
runs as. By default files that user cannot read are skipped and the skip is recorded in the
run log, which is the one place to check after adding a new path. Set `strict: true` when a
partial archive is worse than no archive: the run then fails on the first unreadable entry.
Under systemd, the hardened unit also blocks paths outside the data directory: see the
notes on `ProtectSystem` and `ProtectHome` in [../install.md](../install.md).

**Excludes match the stored path.** Patterns are matched against the entry as it goes into
the archive, so with `base_dir: /srv` you exclude `www/cache/**`, not `/srv/www/cache/**`.
A single `*` stops at a slash and `**` crosses directories, so `**/cache/**` is usually
what you want. A pattern with no slash in it, such as `*.log`, is also matched against the
bare file name at any depth. The directory entry itself may still be stored even when its
contents are excluded, which is why a restored tree can contain empty `cache` directories.

**`follow_symlinks` can duplicate or loop.** A tree with several links to the same large
file stores it once per link. A link that points back into the tree is a cycle. Leave the
option off unless you know why you need it.

**An archive of a live database is not a database backup.** Files under `/var/lib/mysql`
copied while the server runs are a torn copy. Use the matching database driver, and let the
`files` source cover configuration and uploads.

**Archives of many small files are slow before they are big.** The cost is in walking the
tree, not in the bytes. Narrow the paths, or split one job into several with different
schedules.

**Ownership and ACLs.** The archive stores numeric uid, gid and mode. Extended attributes
and POSIX ACLs are not included. On restore the mode is applied but the owner is not,
unless `restore_ownership` is on and Backvault runs as root; a failure to change the owner is
logged and the restore continues. On a host with different accounts, plan on a `chown`
afterwards either way.

## See also

- [../scripts.md](../scripts.md) for `backup-files.sh`, the standalone equivalent for hosts
  Backvault cannot reach.
- [docker.md](docker.md) for archiving a Docker volume rather than a host path.
- [../restore.md](../restore.md) for the restore modes and the `extract` parameter.
