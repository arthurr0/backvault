# SFTP

Stores artifacts on any host that offers the SSH file transfer subsystem, including a Hetzner
Storage Box, a rented seedbox or a plain Linux server.

- Kind: `sftp`
- Needs: host, port, user, and either a private key or a password

For a Hetzner Storage Box, read [Hetzner Storage Box](hetzner-storage-box.md) as well, which
covers sub-accounts and the non-standard port.

## Configuration fields

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `host` | string | yes | none | Hostname or address of the SFTP server. |
| `port` | port | no | `22` | TCP port. A Hetzner Storage Box listens on `23` from outside Hetzner. |
| `user` | string | yes | none | Login name. |
| `auth` | select | no | `password` | `password` or `key`. Decides which credential fields are shown and used. |
| `password` | secret | when `auth` is `password` | none | Login password. Only shown when `auth` is `password`. Stored encrypted. |
| `private_key` | text | when `auth` is `key` | none | PEM text of the key, pasted whole including the header and footer lines. OpenSSH, RSA, ECDSA and Ed25519 keys are supported. Only shown when `auth` is `key`. Stored encrypted. |
| `key_passphrase` | secret | no | empty | Passphrase for an encrypted private key. Only shown when `auth` is `key`. Leave empty for an unencrypted key. Stored encrypted. |
| `host_key` | string | no | empty | Expected host key fingerprint, for example `SHA256:abc...`. When set, a mismatch aborts the connection. When empty, the first key seen is accepted and its fingerprint is written to the run log as a warning so you can pin it here. |
| `base_path` | path | no | `.` | Remote directory that holds the artifacts. Relative paths start in the login directory. Missing directories are created. |
| `connect_timeout` | int | no | `20` | Seconds to wait for the SSH connection itself. It does not limit a transfer that is already running. |
| `concurrent_requests` | int | no | `32` | Concurrent SFTP requests per file, between 1 and 256. Higher values speed up transfers on long-distance links. Lower it if the server complains. |

`private_key`, `key_passphrase` and `password` are secret fields. Sending exactly `********`
in an update keeps the stored value.

`host_key`, `connect_timeout` and `concurrent_requests` sit in the advanced part of the form.
A value in `host_key` has to start with `SHA256:`, which is what `ssh-keygen -lf` prints;
the older MD5 form is refused when the destination is saved.

## Example

A destination as it appears in a `backvault export` document:

```yaml
destinations:
  - name: Storage Box
    kind: sftp
    description: Offsite copy on a Hetzner Storage Box
    config:
      host: u123456.your-storagebox.de
      port: 23
      user: u123456-sub1
      auth: key
      private_key: '********'
      key_passphrase: '********'
      host_key: 'SHA256:9UFZ8pPcUu9mVe3oGkGtCaE9kBWjWeQPXDbFHBjNiM0'
      base_path: backups
      connect_timeout: 20
      concurrent_requests: 32
    tags:
      - offsite
```

## How Backvault writes objects

The remote path is the base path followed by the artifact path:

```text
<base_path>/<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]
```

The timestamp is UTC. A job with slug `db-prod` and base path `backups` writes:

```text
backups/db-prod/db-prod-20260917-020000.dump.zst.age
```

Backvault creates missing directories one level at a time. The SFTP protocol has no recursive
mkdir, so `backups/db-prod` is created as `backups` first and then `db-prod`, and an attempt to
create a directory that already exists is ignored rather than treated as an error. The
standalone `scripts/upload-sftp.sh` does the same with one `-mkdir` per level, which was
verified against an `atmoz/sftp` container including a base path containing a space.

Uploads are atomic from the reader's point of view. The file is written as
`<name>.partial` and renamed to `<name>` once the transfer finishes, so a half-transferred
backup is never mistaken for a complete one. The rename uses the POSIX rename extension where
the server offers it, and falls back to a delete of the target plus a plain rename where it does
not. When an upload fails, Backvault removes its own `.partial` file, so one is only left behind
when the process is killed outright, and the next attempt overwrites it anyway. Listing ignores
`.partial` files, so retention never sees them, and neither does `scripts/prune-sftp.sh`.

Deleting a file that is already gone is not an error. After a successful delete Backvault also
removes the directories that just became empty, walking up until it reaches `base_path` or hits
a directory that still has content. `List` reads the job directory and `Stat` reports size and
modification time, which is what verification and retention use: verification compares the size
alone, since SFTP reports no checksum.

## Permissions

The account needs to create directories and to read, write and delete files under `base_path`.
That is the normal state for a home directory on a Linux host and for a Storage Box
sub-account with write access.

Prefer key authentication. `auth` defaults to `password`, so switch it to `key` before the
private key field appears. Password authentication also answers a keyboard-interactive prompt
with the same password, which is what servers that ask for the password in a challenge expect.

Generate a key that exists only for this purpose, without a passphrase if Backvault is the only
consumer, since a passphrase stored next to the key in the same database adds nothing:

```bash
ssh-keygen -t ed25519 -f ./backvault_sftp -C backvault -N ''
ssh-copy-id -i ./backvault_sftp.pub -p 22 backup@storage.example.com
```

Paste the contents of `backvault_sftp` (the private key, not the `.pub` file) into the
`private_key` field, then delete the local copy. The key is stored encrypted with the master
key, like every other secret. See [security](../security.md).

Record the host key fingerprint so the destination cannot be silently redirected:

```bash
ssh-keyscan -p 22 storage.example.com 2>/dev/null | ssh-keygen -lf -
```

Copy the `SHA256:...` value into `host_key`.

Restrict what the key can do on the server side where you control it. For an account used only
by Backvault, forcing the internal SFTP subsystem and a chroot keeps a stolen key from becoming a
shell:

```text
Match User backup
    ChrootDirectory /srv/backup-chroot
    ForceCommand internal-sftp
    PermitTunnel no
    AllowTcpForwarding no
    X11Forwarding no
```

The chroot directory itself must be owned by root and not writable by the user, so create a
writable subdirectory inside it and point `base_path` at that. An `atmoz/sftp` container
behaves the same way, which is why its documented usage creates a writable subdirectory.

## Using SFTP from the standalone scripts

`scripts/upload-sftp.sh` and `scripts/prune-sftp.sh` cover hosts that push straight to an SFTP
server:

```bash
export SFTP_HOST=u123456.your-storagebox.de
export SFTP_PORT=23
export SFTP_USER=u123456-sub1
export SFTP_KEY=/etc/backvault/sftp_key
export SFTP_KNOWN_HOSTS=/etc/backvault/known_hosts
export SFTP_BASE_PATH=backups/db-prod

scripts/upload-sftp.sh --file /var/tmp/db-prod-20260917-020000.dump.zst
scripts/prune-sftp.sh --keep 14
```

`upload-sftp.sh` has two modes. The default `--mode sftp` drives `sftp -b` with a batch file
and works against any SFTP server. `--mode ssh` streams the file through `ssh host 'cat >
file'`, which is slightly faster but needs shell access on the server. Servers that force the
SFTP subsystem, which includes a Hetzner Storage Box and the `atmoz/sftp` image, reject it with
"This service allows sftp connections only". Use the default mode there.

Password authentication in the scripts needs `sshpass`, and the password is read from a file
and handed over on a file descriptor rather than on a command line. Where `sshpass` is not
installed, key authentication is the only option. See [scripts](../scripts.md).

## Gotchas

- **Port 23 is not a typo on Hetzner.** Storage Boxes take SSH and SFTP on port 23 from outside
  the Hetzner network. Port 22 is only reachable internally. The field default is 22, so change
  it.
- **Host key trust on first use.** With `host_key` empty, Backvault accepts whatever
  key the server presents the first time and logs a warning. Fill the field in for anything
  that leaves your network.
- **`base_path` is relative to the login directory.** It defaults to `.`, the login directory
  itself, so leaving it empty puts the job directories straight into the home directory. A
  leading slash makes it absolute, which is usually wrong on a chrooted server where the login
  directory is already the root of what you can see.
- **`auth` starts at `password`.** A destination saved without touching it expects a password,
  and the private key field is not even shown until you pick `key`. Saving `key` without a
  private key is refused.
- **A full quota fails mid upload.** The run fails at the upload stage and Backvault deletes the
  `.partial` file it was writing, so the space it took is given back. Storage Box quotas are per
  box, not per sub-account, so one noisy job can fill the space every other job needs.
- **Key format.** Paste the OpenSSH private key exactly as generated, including
  `-----BEGIN OPENSSH PRIVATE KEY-----` and the trailing newline. A key mangled by a text
  editor fails with an unhelpful parse error.
- **Rate limits and connection limits.** Some providers cap concurrent SSH sessions per
  account. If several jobs run at once against the same account, lower
  `settings.maxConcurrentRuns` or give each host its own sub-account.
- **Listing a directory with many files is slow.** SFTP has no server side filtering, so
  retention reads the whole job directory. Keep one job per directory, which is what the
  `<jobSlug>/` layout does by default.
