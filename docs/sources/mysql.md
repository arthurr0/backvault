# MySQL / MariaDB

Dumps one database, or every database, with `mysqldump` or `mariadb-dump` and streams the
SQL into the backup pipeline.

Kind `mysql`. Extension `sql`. Requires `mysqldump` or `mariadb-dump` on the Backvault host,
and `mysql` (or `mariadb`) for connection tests and restores. Capabilities: test, restore.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | `127.0.0.1` | Server host name or address. Use `127.0.0.1` rather than `localhost` to force TCP instead of a Unix socket. |
| `port` | port | no | `3306` | Server port. Ignored when `socket` is set. |
| `socket` | path | no | | Unix socket to connect through. When set, `host` and `port` are ignored. |
| `user` | string | yes | `root` | User to connect as. |
| `password` | secret | no | | Password for the user. Stored encrypted, masked in every API response. |
| `all_databases` | bool | no | `false` | Dump the whole server with `--all-databases`. The `mysql` schema is included. |
| `databases` | list | yes | | One database per line. Hidden and not required when `all_databases` is on. |

Options:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `single_transaction` | bool | no | `true` | Adds `--single-transaction`: a consistent dump of InnoDB tables without locking them. Turn it off for MyISAM-only servers. |
| `routines` | bool | no | `true` | Adds `--routines`. |
| `triggers` | bool | no | `true` | Adds `--triggers`. |
| `events` | bool | no | `true` | Adds `--events`. |
| `schema_only` | bool | no | `false` | Adds `--no-data`: table definitions without any rows. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `extra_args` | list | no | | Extra arguments appended to the dump command, one per line. |
| `binary_path` | path | no | | Directory holding `mysqldump` and `mysql`, or the full path to the dump binary. Empty auto-detects `mysqldump`, then `mariadb-dump`, on `PATH`. |

`databases` is a list, not a single name. One entry is dumped as a bare database argument,
which produces a dump without `CREATE DATABASE`. Two or more entries are dumped with
`--databases`, which does include the database statements.

There is no exclude option. Leave tables out with `--ignore-table=db.table` in `extra_args`.

The password never reaches a command line. Backvault writes a temporary defaults file with
mode 0600 and passes `--defaults-extra-file`, so `ps` on the Backvault host shows nothing
useful. The same file carries `host`, `port`, `protocol=TCP` or `socket`, and `user`.

## Example

```yaml
sources:
  - name: Shop database
    kind: mysql
    config:
      host: db.internal
      port: 3306
      user: backup
      password: "********"
      databases:
        - shop
      extra_args:
        - --ignore-table=shop.sessions
        - --ignore-table=shop.cache_items
```

## Restore

`download` gives you plain SQL once the artifact is unpacked, which you replay yourself:

```bash
mysql --host db.internal --user root --password shop < shop-20260917-020000.sql
```

`path` writes the unpacked SQL to a file on the Backvault host.

`source` pipes the SQL into `mysql` against the job's source, or against `targetSourceId`
when you send it to another `mysql` source. The restore parameter `database` names the
database the client selects; with no parameter Backvault uses the single entry in
`databases`, and falls back to letting the dump choose when the source lists several
databases or uses `all_databases`. A dump of a single database does not contain
`CREATE DATABASE`, so the target database has to exist first. A dump of several databases,
or one taken with `all_databases`, does contain the database statements and replaces what
it names.

## Gotchas

**The binary is not always called `mysqldump`.** MariaDB 11 ships `mariadb-dump` and only
provides `mysqldump` as a compatibility symlink on some builds. Backvault looks for
`mysqldump` first and falls back to `mariadb-dump`, and reports which one it found under
**Settings, Tools**. On a host with neither, set `binary_path`.

**Client and server families differ.** A MariaDB client dumping a MySQL 8 server, or the
other way round, can emit syntax the target refuses to replay. Keep the client family and
version close to the server, and test the restore.

**`--single-transaction` does not protect MyISAM.** It gives a consistent snapshot for
InnoDB only. A MyISAM table can change while the dump runs. Convert to InnoDB, or accept
that those tables are copied without a snapshot.

**`PROCESS` privilege.** Newer `mysqldump` releases read tablespace information and need
the `PROCESS` privilege. Without it the dump fails with `Access denied; you need (at least
one of) the PROCESS privilege(s)`. Either grant it, or add `--no-tablespaces` to
`extra_args`.

**The backup user needs more than `SELECT`.** `SELECT`, `SHOW VIEW`, `TRIGGER`,
`LOCK TABLES` and `EVENT` cover the flags Backvault passes. Missing `SHOW VIEW` or `TRIGGER`
gives a dump that restores into a database without views or triggers.

**`all_databases` includes the `mysql` schema.** Replaying it onto another server replaces
that server's accounts and grants. For a data-only migration, dump the application
database on its own.

## See also

- [../scripts.md](../scripts.md) for `backup-mysql.sh`, which builds the same defaults file
  on the database host.
- [../restore.md](../restore.md) for the restore modes and their parameters.
- [../troubleshooting.md](../troubleshooting.md) for privilege and connection errors.
