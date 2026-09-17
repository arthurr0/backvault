# Remote command over SSH

Runs a command on another host over SSH and stores whatever it writes to standard output.
This is the driver for machines Backvault can reach but should not install anything on.

Kind `ssh`. Extension configurable, `tar` by default. Requires nothing on the Backvault host,
the SSH client is built in, so there is no `ssh` binary to install and no `binary_path`
field. Capabilities: test, restore when `restore_command` is set.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | | Remote host name or address. |
| `port` | port | no | `22` | SSH port. |
| `user` | string | yes | `root` | Remote user. |
| `auth` | select | no | `key` | `key` or `password`. Selects which credential field applies. |
| `private_key` | text | yes when `auth` is `key` | | PEM text of the key. OpenSSH, RSA, ECDSA and Ed25519 keys are supported. Stored encrypted, masked in every API response. Only shown when `auth` is `key`. |
| `key_passphrase` | secret | no | | Passphrase for an encrypted private key. Leave it empty for an unencrypted key. Only shown when `auth` is `key`. |
| `password` | secret | yes when `auth` is `password` | | Password for the remote user. Only shown when `auth` is `password`. |
| `host_key` | string | no | | Expected host key fingerprint, in `SHA256:...` form. When set it is verified on every run. When empty, the first key seen is trusted and the run log carries a warning with the fingerprint so you can pin it here. |

Backup:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | text | yes | | Command run on the remote host. Its standard output becomes the artifact. |
| `extension` | string | no | `tar` | Extension recorded for the artifact, for example `dump`, `sql`, `tar`. |
| `restore_command` | text | no | | Command run on the remote host during a `source` restore. The unpacked artifact is written to its standard input. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `connect_timeout` | int | no | `15` | Seconds to wait for the TCP connection and the SSH handshake. |

The remote command must exit with status 0. A non-zero exit fails the run even when output
was already produced, so a truncated dump never becomes an artifact. Everything the command
writes to standard error goes into the run log.

## Example

A remote PostgreSQL dump, without giving Backvault a database port:

```yaml
sources:
  - name: Customer database via SSH
    kind: ssh
    config:
      host: app.customer.example
      port: 22
      user: backup
      auth: key
      private_key: "********"
      host_key: SHA256:2Pw3sM1o0QeQ1x9cE3n7pB0YkT6VtF2rA8s5dJ4hQxU
      command: >-
        PGPASSWORD="$BACKUP_PG_PASSWORD" pg_dump --format=custom --compress=0 --dbname shop
      extension: dump
```

A remote tar of a directory:

```yaml
sources:
  - name: Customer uploads
    kind: ssh
    config:
      host: app.customer.example
      user: backup
      auth: key
      private_key: "********"
      command: tar --create --file - --directory /srv/uploads .
      extension: tar
```

## Restore

`download` and `path` behave like every other driver: you get the bytes the remote command
produced, unpacked, as described in [../restore.md](../restore.md).

`source` is only offered when `restore_command` is set. Backvault opens the same SSH
connection, runs that command and streams the unpacked artifact into its standard input.
For the PostgreSQL example above, the matching restore command is:

```text
pg_restore --clean --if-exists --dbname shop
```

For the tar example:

```text
tar --extract --file - --directory /srv/uploads
```

Backvault cannot check what the remote command does with the data. Test it against a scratch
target before you rely on it.

## Gotchas

**Set `host_key`.** Without it the first key seen is accepted, which is a window for a man
in the middle on the first run. Read the fingerprint on the remote host with
`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` and paste the `SHA256:...` value in.
Backvault rejects anything that does not start with `SHA256:`.

**The remote account should be restricted.** Give it a key with a `command=` restriction in
`authorized_keys`, or a forced command, so a leaked key cannot do more than produce the
dump.

**Secrets inside `command` end up in a process list on the remote host.** Put them in the
remote user's environment or a file that only that user can read, and reference them, as in
the `$BACKUP_PG_PASSWORD` example above.

**Only servers that allow a shell.** This driver needs to run a command. A host that offers
the sftp subsystem alone, such as a Hetzner Storage Box or an `atmoz/sftp` container,
refuses with `This service allows sftp connections only`. Those are destinations, not
sources: see [../destinations/sftp.md](../destinations/sftp.md).

**The command must not write to standard output for anything but the data.** A shell
profile that prints a banner corrupts the artifact. Keep `command` free of anything
chatty, and check the first bytes of a fresh artifact after you change it.

**Slow links dominate the run time.** The dump is produced remotely but travels over SSH
uncompressed unless the remote command compresses it. Compressing remotely saves transfer,
compressing in the job keeps the artifact checksum aligned with what Backvault stores. Pick
one, not both.

## See also

- [command.md](command.md) for the same idea on the Backvault host itself.
- [push.md](push.md) when the remote host cannot accept inbound SSH.
- [../restore.md](../restore.md) for restore modes.
