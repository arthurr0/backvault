# PostgreSQL

Dumps one database with `pg_dump`, or a whole cluster with `pg_dumpall`, and streams the
result into the backup pipeline.

Kind `postgres`. Extension `dump` for the custom format, `sql` for plain format and for
`pg_dumpall`. Requires `pg_dump` (and `pg_dumpall` for cluster dumps) on the Backvault host,
`pg_restore` and `psql` for restores. Capabilities: test, restore, remote.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | `localhost` | Host name, IP address or path to the Unix socket directory. |
| `port` | port | no | `5432` | Server port. |
| `user` | string | yes | `postgres` | Role to connect as. It needs read access to everything you want backed up. |
| `password` | secret | no | | Password for the role. Passed through `PGPASSWORD`, never on the command line. Leave it empty for peer, trust or `.pgpass` authentication. |
| `database` | string | yes | | Database to dump. Hidden and not required when `all_databases` is on. |
| `all_databases` | bool | no | `false` | Dump the whole cluster with `pg_dumpall`, including roles and tablespaces. The output is always plain SQL. |
| `sslmode` | select | no | `prefer` | Sets `PGSSLMODE`: `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`. |

Options:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `format` | select | no | `custom` | `custom` produces a `dump` file for `pg_restore`, `plain` produces `sql` for `psql`. Hidden when `all_databases` is on. |
| `schema_only` | bool | no | `false` | Adds `--schema-only`: table definitions without any rows. |
| `exclude_tables` | list | no | | Table patterns to leave out, one per line, passed as `--exclude-table`. Hidden when `all_databases` is on. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `extra_args` | list | no | | Extra arguments appended to the dump command, one per line. |
| `binary_path` | path | no | | Directory holding `pg_dump`, `pg_dumpall`, `pg_restore` and `psql`, or the full path to `pg_dump`, when they are not on `PATH`. |

The dump is never compressed by `pg_dump` itself. Compression happens once, in the job's
pack stage, where the artifact checksum is taken. Backvault also sets `PGCONNECT_TIMEOUT=15`
and `PGCLIENTENCODING=UTF8` for every call.

## Example

```yaml
sources:
  - name: Shop database
    kind: postgres
    config:
      host: db.internal
      port: 5432
      database: shop
      user: backup
      password: "********"
      sslmode: require
      format: custom
      exclude_tables:
        - public.sessions
        - public.job_queue
```

`backvault export` masks `password` as `********`. On import, a field that still carries the
mask keeps the value already stored, so an exported file can be re-imported without
handing the password around.

## Run on a host

Pick a host in the **Run on** select and `pg_dump` (or `pg_dumpall`) runs on that machine, so
`host: localhost` then means the database on the host itself and no port has to be exposed to
the network. The command is the same one described above, prefixed with the environment:

```
env PGPASSWORD=... PGSSLMODE=... PGCONNECT_TIMEOUT=15 PGCLIENTENCODING=UTF8 pg_dump ...
```

The password is passed through the environment, never as an argument, so it does not appear in
the host's process list. `binary_path` refers to a directory or a binary on the host, which is
the usual way to select a versioned client such as `/usr/lib/postgresql/18/bin`.

**Test connection** checks that `psql` exists on the host and runs `select 1` through it, and
falls back to `pg_isready` when only that is installed. Restores run `pg_restore` or `psql`
on the host with the artifact on standard input.

## Restore

Three modes, all described in [../restore.md](../restore.md).

`download` streams the artifact to your machine, decrypted and decompressed unless you
pass `?raw=1`. Restore it yourself:

```bash
pg_restore --clean --if-exists --dbname shop shop-20260917-020000.dump
psql --dbname shop --file shop-20260917-020000.sql
```

`path` writes the unpacked dump to a file on the Backvault host.

`source` pipes the unpacked dump straight back into PostgreSQL. Backvault runs `pg_restore`
for custom-format artifacts and `psql` for plain ones, against the source the job uses, or
against `targetSourceId` when you point the restore at another `postgres` source.

Restore parameters, all optional:

| Parameter | Applies to | Default | Effect |
|---|---|---|---|
| `database` | both | the source's `database`, else `postgres` | Database the restore connects to. |
| `clean` (or `drop`) | `pg_restore` | `false` | Adds `--clean`. |
| `if_exists` | `pg_restore` | follows `clean` | Adds `--if-exists`. |
| `create` | `pg_restore` | `false` | Adds `--create`. |
| `no_owner` | `pg_restore` | `true` | Adds `--no-owner`. Set it to `false` to keep the original ownership. |
| `stop_on_error` | `psql` | `true` | Adds `-v ON_ERROR_STOP=1`. |
| `single_transaction` | `psql` | `false` | Adds `--single-transaction`. |

A cluster dump from `pg_dumpall` is plain SQL and is replayed with `psql` against the
`postgres` database. It contains `CREATE ROLE` statements, so replaying it into a cluster
that already has those roles reports errors for each one.

## Gotchas

**Client version skew cuts both ways.** `pg_dump` must be at least as new as the server it
reads. A PostgreSQL 16 server dumped with a `pg_dump` 18 client works. Restoring that
artifact into a PostgreSQL 16 server does not: `pg_restore` replays settings the older
server does not know and stops with

```text
pg_restore: error: could not execute query: ERROR:  unrecognized configuration parameter "transaction_timeout"
```

Keep the client and the server on the same major version whenever you expect to restore in
place, and test a restore before you need one.

**The Docker image ships a PostgreSQL 18 client.** It is installed from the PGDG apt
repository, so a containerised Backvault dumps servers from 9.2 up to 18. Restoring a dump
taken with an 18 client into an older server is still a problem, see the skew note above.
When you run the binary directly, install the client package yourself and point
`binary_path` at it if it is not on `PATH`.

**`-w` is always passed.** That is `--no-password`: a missing or wrong password fails
immediately instead of hanging on a prompt. Check `pg_hba.conf` when a connection that
works interactively fails from Backvault.

**Custom format is the better default.** It supports selective restore, parallel restore
and index reordering. Choose plain only when something downstream needs readable SQL.

**Excluded tables still need their schema.** `--exclude-table` leaves out the rows and the
table definition. A dump without a table that the application expects will not restore into
a working system on its own.

**Large objects and extensions.** `pg_dump` includes large objects in the custom format by
default. Extensions are dumped as `CREATE EXTENSION`, so the target server needs the same
extension packages installed.

## See also

- [../scripts.md](../scripts.md) for `backup-postgres.sh`, which runs on the database host
  and needs nothing from Backvault but an API token.
- [../push-and-ingest.md](../push-and-ingest.md) for the inverted, push-based setup.
- [../troubleshooting.md](../troubleshooting.md) for connection and permission failures.
