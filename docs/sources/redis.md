# Redis

Asks a Redis server for a fresh RDB snapshot with `redis-cli --rdb` and stores that
snapshot as the artifact.

Kind `redis`. Extension `rdb`. Requires `redis-cli` on the Backvault host. Capabilities: test.
This driver has no restore support.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | `127.0.0.1` | Server host name or address. |
| `port` | port | no | `6379` | Server port. |
| `user` | string | no | | ACL user, passed as `--user`. Only needed on Redis 6 or newer with ACLs. Leave it empty for password-only authentication. |
| `password` | secret | no | | Password. Stored encrypted, masked in every API response. |
| `tls` | bool | no | `false` | Adds `--tls`, for managed Redis that requires it. |
| `tls_skip_verify` | bool | no | `false` | Adds `--insecure`, accepting a self-signed certificate. Only shown when `tls` is on. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `extra_args` | list | no | | Extra arguments appended to `redis-cli`, one per line. |
| `binary_path` | path | no | | Full path to `redis-cli`, or the directory holding it, when it is not on `PATH`. |

The password is passed through the `REDISCLI_AUTH` environment variable rather than
`-a`, which keeps it out of the process list and out of the "Warning: Using a password with
-a is insecure" message. Backvault also passes `--no-auth-warning`.

## Example

```yaml
sources:
  - name: Session cache
    kind: redis
    config:
      host: cache.internal
      port: 6379
      password: "********"
      tls: false
```

With a TLS endpoint that presents a private certificate:

```yaml
sources:
  - name: Managed cache
    kind: redis
    config:
      host: cache.provider.example
      port: 6380
      user: backup
      password: "********"
      tls: true
      tls_skip_verify: false
      extra_args:
        - --cacert=/etc/ssl/redis-ca.pem
```

## Restore

This driver cannot restore. Redis reads its dataset from an RDB file at startup, so
restoring is a file operation on the Redis host, not something Backvault can drive over the
protocol.

Download the artifact, unpack it, then:

```bash
systemctl stop redis
install -o redis -g redis -m 0660 shop-20260917-020000.rdb /var/lib/redis/dump.rdb
systemctl start redis
```

Check the Redis log after the start. A file Redis refuses to load leaves you with an empty
instance, and it says so in the log.

The restore modes that stay available are `download` and `path`, both described in
[../restore.md](../restore.md). `source` is not offered because the driver does not
advertise the restore capability.

## Gotchas

**`--rdb` asks the server to fork.** The server runs a `SYNC` and writes a snapshot, which
briefly needs memory for the copy on write pages. On an instance that is already close to
its memory limit, that is the moment it starts evicting or fails.

**`stop-writes-on-bgsave-error`.** When a previous background save failed, Redis refuses
writes and the snapshot request fails too. Fix the underlying disk problem first.

**The snapshot is not the same file Redis keeps.** `redis-cli --rdb` produces a new dump at
the moment you ask. It does not copy `dump.rdb` from disk, so it does not depend on the
server's own `save` schedule.

**Cache data may not be worth backing up.** If Redis holds only derived state, back up what
produces it instead and leave Redis out of the schedule. If it holds queues or sessions,
back it up and be clear with yourself about how much loss a restore implies.

**Cluster mode.** `redis-cli --rdb` talks to one node. A Redis Cluster needs one source per
master node, each with its own job, or a different tool entirely.

## See also

- [../restore.md](../restore.md) for downloading and unpacking artifacts.
- [../retention.md](../retention.md) for how long to keep snapshots that go stale quickly.
