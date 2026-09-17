# Sources

A source describes what to back up. The `kind` field selects a driver, and the rest of the
configuration is validated against that driver's field schema.

Every source is reusable: several jobs can point at the same source with different
schedules, destinations and retention. Create sources under **Sources** in the panel, or
through `POST /api/v1/sources`. Before saving, use **Test connection** so a broken
credential shows up now rather than at two in the morning.

## Available drivers

| Kind | Label | Extension | External tools | Restore |
|---|---|---|---|---|
| [`postgres`](postgres.md) | PostgreSQL | `dump` or `sql` | `pg_dump`, `pg_dumpall`, `pg_restore`, `psql` | yes |
| [`mysql`](mysql.md) | MySQL / MariaDB | `sql` | `mysqldump` or `mariadb-dump`, `mysql` or `mariadb` | yes |
| [`mongodb`](mongodb.md) | MongoDB | `archive` | `mongodump`, `mongorestore` | yes |
| [`redis`](redis.md) | Redis | `rdb` | `redis-cli` | no |
| [`sqlite`](sqlite.md) | SQLite | `sqlite` | `sqlite3` (optional) | yes |
| [`files`](files.md) | Files and directories | `tar` | none | yes |
| [`ssh`](ssh.md) | Remote command over SSH | `tar` by default, configurable | none | optional |
| [`docker`](docker.md) | Docker volume or container | `tar` for volumes, `dump` by default in exec mode | `docker` (or `podman`) | volumes only |
| [`command`](command.md) | Custom command | `bin` by default, configurable | none | optional |
| [`push`](push.md) | Push from a remote script | derived from the upload | none | no |

The extension ends up in the artifact filename:
`<job-slug>/<job-slug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]`. Compression and
encryption are job settings, not source settings, and are described in
[../encryption.md](../encryption.md).

## Which external tools are installed

`GET /api/v1/meta/tools`, and the **Settings, Tools** page in the panel, report every
binary the registered drivers shell out to, whether it was found on `PATH` and which
version answered. `backvault check` prints the same list from the command line.

The Docker image ships a PostgreSQL 18 client from the PGDG repository, `mariadb-client`,
the MongoDB Database Tools, `redis-tools`, `sqlite3`, `openssh-client`, `age` and the
Docker CLI, so every driver on this page works out of the box. See
[../install.md](../install.md) for the full list. If you run the binary directly, install
the client for each database you back up.

## Sources on hosts Backvault cannot reach

Backvault connects out from the machine it runs on. When a database sits behind a firewall,
on a customer network, or on a host that must not accept inbound connections, invert the
direction: create a [`push`](push.md) source and let a script on that host upload the dump
through the ingest API. See [../push-and-ingest.md](../push-and-ingest.md) and
[../scripts.md](../scripts.md).
