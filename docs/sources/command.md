# Custom command

Runs a shell command on the Backvault host and stores what it writes to standard output. This
is the escape hatch for everything without a driver of its own.

Kind `command`. Extension configurable, `bin` by default. Requires whatever the command
itself needs. Capabilities: test, restore when `restore_command` is set.

## Configuration fields

Command:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | text | yes | | Command run on the Backvault host. It must write the backup to standard output and exit with code 0. |
| `extension` | string | no | `bin` | Extension recorded for the artifact, for example `tar`, `sql`, `dump` or `json`. A leading dot is stripped. |
| `working_dir` | path | no | | Directory the command runs in. It has to exist; the connection test says so when it does not. |
| `env` | list | no | | One `KEY=VALUE` per line, added to the environment of the command. Values of three characters or more are registered as redacted strings and never reach the run log. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `test_command` | text | no | | Run by the **Test connection** button instead of the backup command. When it is empty, only the shell itself is checked. |
| `restore_command` | text | no | | Command run during a `source` restore. The unpacked artifact is written to its standard input. |
| `shell` | string | no | `/bin/sh` | Interpreter used for every command above, called as `<shell> -c <command>`. |

The command runs through a shell, so pipes and redirections work. It must exit with status
0. A non-zero exit fails the run even when output was already produced. Everything it
writes to standard error goes into the run log, which makes it the right place for progress
messages.

`env` is not a replacement for the process environment: the entries are added to what the
Backvault service already has, and a malformed line without `=` is rejected when the source is
saved.

## Example

```yaml
sources:
  - name: InfluxDB export
    kind: command
    config:
      command: influxd backup -portable /tmp/influx >/dev/null && tar --create --file - --directory /tmp/influx .
      extension: tar
      test_command: influxd version
      restore_command: tar --extract --file - --directory /tmp/influx-restore
```

An API export that needs a token:

```yaml
sources:
  - name: Billing export
    kind: command
    config:
      command: >-
        curl --silent --show-error --fail
        --header "Authorization: Bearer $BILLING_TOKEN"
        https://billing.internal/api/export
      extension: json
      working_dir: /var/lib/backvault
      env:
        - BILLING_TOKEN=********
```

## Restore

`download` and `path` give you the bytes the command produced, unpacked.

`source` is offered only when `restore_command` is set, either on the source or as a
`restore_command` restore parameter that overrides it for one run. Backvault runs it with the
same shell, `working_dir` and `env` as the backup command, and streams the unpacked
artifact into its standard input. Nothing checks that the command does the right thing, so
test it against a scratch target first.

## Gotchas

**The command runs as the Backvault service user.** That user is deliberately unprivileged.
Under the hardened systemd unit, most of the file system is read only and `/home` is not
visible at all, so a command that writes to a temporary directory outside
`/var/lib/backvault` fails. See [../install.md](../install.md).

**Anything on standard output is part of the artifact.** A warning printed by a tool ends
up in the middle of the data. Redirect noise to standard error, or to `/dev/null`, as in
the InfluxDB example above.

**Pipelines hide failures.** `dump | gzip` reports the exit status of `gzip`, so a dump that
died halfway still looks successful. Start the command with `set -o pipefail;` when you use
a pipe, or keep the compression in the job's pack stage where the checksum is taken.

**Secrets belong in the environment, not in the command.** The command line is visible to
every user on the host. Read them from a file the service user owns, or from the
environment file the unit loads.

**Set `extension` honestly.** It ends up in the artifact filename and tells anyone
restoring what the file is. `dump` for a database dump, `tar` for an archive, `json` or
`csv` for an export.

**Temporary files are yours to clean up.** The engine removes its own spool directory, not
whatever your command left in `/tmp`. Clean up inside the command, including on failure.

## See also

- [ssh.md](ssh.md) for the same idea on another host.
- [../restore.md](../restore.md) for restore modes.
- [../troubleshooting.md](../troubleshooting.md) when a command works in a shell but not in
  a run.
