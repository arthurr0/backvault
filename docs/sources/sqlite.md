# SQLite

Copies a SQLite database file consistently while the application keeps using it.

Kind `sqlite`. Extension `sqlite`. Uses `sqlite3` when it is available, otherwise performs
an online backup through the built-in driver. Capabilities: test, restore.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | path | yes | | Absolute path to the `.db` or `.sqlite` file on the Backvault host. Relative paths are rejected. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `method` | select | no | `auto` | `auto` uses `sqlite3 .backup` when the binary is on `PATH` and falls back to `VACUUM INTO`. `sqlite3` requires the binary and fails without it. `vacuum` always uses `VACUUM INTO`, which also compacts the copy. Both methods are consistent. |
| `binary_path` | path | no | | Full path to `sqlite3`, or the directory holding it, when it is not on `PATH`. |

## Example

```yaml
sources:
  - name: Application database
    kind: sqlite
    config:
      path: /var/lib/app/app.db
      method: auto
```

## Restore

`download` gives you a `.sqlite` file that is a complete database, openable with any SQLite
client:

```bash
sqlite3 app-20260917-020000.sqlite '.tables'
sqlite3 app-20260917-020000.sqlite 'pragma integrity_check;'
```

`path` writes the database to a file on the Backvault host.

`source` copies the database over the file named in `path`. Stop the application first. A
running process holding the old file open keeps writing to the inode it already has, and
its next write undoes the restore.

## Gotchas

**Copying the file with `cp` is not a backup.** A plain copy of a database that is being
written to can be torn: the main file may miss pages that are still in the write ahead log.
Backvault uses the online backup API (`.backup`, or `VACUUM INTO`), which produces a
consistent file without stopping the application.

**The `-wal` and `-shm` files are not in the artifact, and do not need to be.** The online
backup folds committed transactions from the write ahead log into the copy. Restoring means
putting back one file, not three. Delete any stale `app.db-wal` next to a restored file
before starting the application.

**The copy is written to the work directory, not next to the database.** Both methods
write into a temporary directory under the configured work directory and stream the
finished file from there, so the database's own directory only has to be readable. The
work directory does need room for a full copy of the database.

**`vacuum` produces a smaller file, `sqlite3` is usually faster.** `VACUUM INTO` rebuilds
the database and reclaims free pages; `sqlite3 .backup` copies pages as they are. Pick
`vacuum` when the file has grown far beyond its data, and `sqlite3` when the database is
large and the copy has to finish quickly.

**File permissions after a restore.** A restored file belongs to the Backvault user. Set the
owner the application expects before starting it again.

**Run `pragma integrity_check` after a restore.** It is fast on small databases and it is
the only cheap way to find out that an artifact is sound before you rely on it.

## See also

- [../restore.md](../restore.md) for restore modes and their parameters.
- [files.md](files.md) when you want the whole application directory rather than one
  database file.
