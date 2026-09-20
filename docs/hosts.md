# Hosts

A host is a reusable SSH connection to another machine. Point a source at a host and the
driver runs its work there instead of on the Backvault server: `pg_dump` runs on the
database machine, `tar` runs on the file server, and only the finished stream travels back
over the SSH connection into the backup pipeline.

Nothing is installed on the host. Backvault opens an SSH session, starts one command, reads
its standard output and records its standard error in the run log. The host needs an SSH
server, the tools the driver calls, and an account allowed to read what you back up.

![The hosts list with a tested host, the tools found on it and the number of sources that run there](images/hosts.png)

## What a host is good for

- A database that only listens on `localhost` on its own machine.
- A file server whose contents are far larger than the pipe between the two machines, where
  you want compression and encryption to happen before the bytes travel.
- A machine where the dump tool is already installed and configured, with its own `.pgpass`,
  socket paths and versions.
- Several sources on the same machine sharing one connection, one key and one test button.

When the machine cannot accept inbound connections at all, invert the direction instead and
use a [push source](sources/push.md) with a script on that machine.

## Adding a host

1. Open **Hosts** in the sidebar and choose **New host**.
2. Fill in the name, address and user. The port defaults to 22.
3. Choose **Private key** (recommended) or **Password** authentication.
4. Press **Generate key**. Backvault creates an ed25519 key pair, keeps the private half
   encrypted in its database and shows you the public half with a copy button.
5. Paste the public key into `~/.ssh/authorized_keys` of that user on the host:

   ```bash
   ssh backup@db01.example.com
   mkdir -p ~/.ssh && chmod 700 ~/.ssh
   printf '%s\n' 'ssh-ed25519 AAAA... backvault' >> ~/.ssh/authorized_keys
   chmod 600 ~/.ssh/authorized_keys
   ```

   ![The host dialog with a generated key pair, the public key and the authorized_keys snippet](images/host-dialog.png)

6. Press **Test connection**. Backvault reports the operating system it found and which of
   the known tools are installed there.
7. Copy the host key fingerprint from the run log into the **Host key fingerprint** field and
   test again, so later connections are pinned.

You can also bring your own key: switch authentication to **Private key** and paste the PEM
text of an existing key, with its passphrase if it has one.

## Pointing a source at a host

Every source form for a driver that supports remote execution has a **Run on** select at the
top: **This server** or one of your hosts. Choosing a host hides the fields that only make
sense locally and keeps everything else. Paths, database names, ports and credentials are
then read as they are on the host: `/var/lib/app/app.db` is a path on the host, and
`localhost` in a PostgreSQL source means the host's own loopback address.

A source keeps working when you move it: switch **Run on** back to **This server** and the
same configuration runs locally again, as long as the paths and credentials also exist there.

## Which drivers can run on a host

| Driver | What runs on the host | Tools needed there |
|---|---|---|
| [`files`](sources/files.md) | `tar -C <base> -cf - ...`, restore extracts with `tar -xf -` | `tar` |
| [`postgres`](sources/postgres.md) | `pg_dump` or `pg_dumpall`, restores with `pg_restore` or `psql` | `pg_dump`, `pg_dumpall`, `pg_restore`, `psql` |
| [`mysql`](sources/mysql.md) | `mysqldump` with a temporary defaults file, restores with `mysql` | `mysqldump` or `mariadb-dump`, `mysql` or `mariadb` |
| [`mongodb`](sources/mongodb.md) | `mongodump --archive`, restores with `mongorestore` | `mongodump`, `mongorestore` |
| [`redis`](sources/redis.md) | `redis-cli --rdb -` | `redis-cli` |
| [`sqlite`](sources/sqlite.md) | `sqlite3 ".backup"` into a temporary file, then `cat` | `sqlite3` |
| [`docker`](sources/docker.md) | the same `docker run` or `docker exec` command | `docker` or `podman` |
| [`command`](sources/command.md) | your command, through `sh -c` | whatever the command uses |
| [`ssh`](sources/ssh.md) | your command, with the host's connection instead of its own fields | whatever the command uses |

The `push` source never runs anywhere: it receives uploads. A driver without the `remote`
capability refuses a host with HTTP 400 and the field name `hostId`.

Every remote command is a POSIX shell command line. The host needs a working `/bin/sh`,
`command -v`, `mktemp` and the tools listed above. A minimal container image without a shell
cannot act as a host.

## Sudo

Turn **Use sudo** on when the account you connect with is not the account that may read the
data. Every command is then wrapped as:

```
sudo -n -- sh -c '<the command>'
```

`-n` means sudo never asks for a password, so the entry in `/etc/sudoers` must be
passwordless. Keep it narrow:

```
backup ALL=(ALL) NOPASSWD: /bin/sh
```

is broad enough to run anything; a tighter rule that only allows the dump command you need is
better when the account is shared with anything else. A sudo rule with `requiretty` set breaks
non-interactive sessions, so make sure `Defaults !requiretty` applies to this user.

The run log prints every remote command exactly as it is sent, so a host with sudo turned on
shows the wrapper too:

```
running tar on edge-01 host=edge-01 command=sudo -n -- sh -c '/usr/bin/tar -C /srv/data -cf - .'
```

Copy that line into a shell on the host when a sudo rule needs checking.

## Host key pinning

The first connection to a host with an empty **Host key fingerprint** is accepted and the
fingerprint is written to the run log as a warning:

```
accepting unverified ssh host key, copy it into the host key field to pin it
host=db01.example.com:22 type=ssh-ed25519 fingerprint=SHA256:abc...
```

Copy that value into the field. From then on a changed host key fails the connection instead
of trusting it, which is what you want if the machine is ever replaced or impersonated. You
can also read the fingerprint on the host itself:

```bash
ssh-keyscan -t ed25519 db01.example.com | ssh-keygen -lf -
```

## Security notes

- The private key, the key passphrase and the password are encrypted at rest with the master
  key, masked as `********` in every API response and never written to a log. See
  [security.md](security.md).
- Credentials for the databases behind a host travel in the environment of the remote command
  (`PGPASSWORD`, `REDISCLI_AUTH`) or in a temporary file created with `umask 077` and removed
  afterwards (MySQL). They are never part of a command line, so they do not show up in the
  host's process list.
- The one exception is the MongoDB source: `mongodump` only accepts its connection URI as an
  argument, so a user on the host who can list processes can see it while the dump runs. Use a
  dedicated backup user for that URI, or run `mongodump` yourself through a
  [`command`](sources/command.md) source that reads the URI from a file on the host.
- Backvault scrubs the secrets it knows about from remote output before it reaches the run
  log, but a command that prints its own credentials will still print them. Keep `--verbose`
  flags off in production.
- Give the host account read access and nothing more. A backup account rarely needs write
  access anywhere except the restore target.
- A restore runs on the host as well, and writes there. `files` restores extract into
  `targetPath` on the host, and `sqlite` replaces the database file on the host. Check which
  machine a restore is aimed at before starting it. In the restore dialog that path is the
  **Target directory** (or **Target file**) field of the **Restore into the source** mode;
  left empty it writes over the directory or database the source reads.

## Troubleshooting

**Permission denied (publickey).** The public key is not in the right `authorized_keys`, or
its permissions are wrong. `~/.ssh` must be `700` and `~/.ssh/authorized_keys` `600`, owned by
the user you connect as. On the host, `sudo journalctl -u ssh -n 50` shows the reason the
server rejected the key. A freshly created account with no password is often locked, and
sshd then refuses the key with `account is locked` in its own log even though the key is
correct; `passwd -u backup` or `usermod -p '*' backup` unlocks it without giving it a
password.

**Permission denied (password).** Password authentication is often disabled in
`/etc/ssh/sshd_config` (`PasswordAuthentication no`). Use a key instead, which is better
anyway.

**`host key mismatch`.** The host presented a different key than the pinned fingerprint. If
the machine was genuinely reinstalled, verify the new fingerprint on the console of that
machine and paste it into the field. Never clear the field to make the error go away without
checking.

**`tar is not installed on host ...`.** The tool is missing or not on the `PATH` of a
non-interactive shell. Install it, or set the driver's **Binary path** field to the full path
on the host. Remember that a non-login SSH command does not read `~/.bash_profile`, so a tool
installed under `/opt` needs its full path.

**`docker: permission denied while trying to connect to the Docker socket`.** The host account
is not in the `docker` group. Add it, or turn **Use sudo** on for that host.

**`sudo: a password is required`.** The sudoers entry is not `NOPASSWD`, or it does not cover
the command. Test it on the host with the exact wrapper Backvault uses:

```bash
sudo -n -- sh -c 'tar --version'
```

**The connection works but the backup is empty.** The command ran as an account that cannot
read the data. Check the run log: unreadable paths are reported there, and `tar` writes its
own warnings to standard error, which ends up in the same log.

**Timeouts on a slow link.** The connect timeout only covers opening the connection. A long
dump is bounded by the job's `timeoutMinutes` instead. Raise it for large hosts, and consider
compressing on the host with a [`command`](sources/command.md) source when the link is the
bottleneck.

## See also

- [concepts.md](concepts.md) for how hosts fit next to sources, jobs and runs.
- [sources/README.md](sources/README.md) for the driver list and what each one needs.
- [security.md](security.md) for secrets at rest and the master key.
- [push-and-ingest.md](push-and-ingest.md) for machines that cannot accept connections.
