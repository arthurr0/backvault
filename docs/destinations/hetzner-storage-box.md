# Hetzner Storage Box

A complete walkthrough for using a Hetzner Storage Box as a Backvault destination, from creating a
sub-account to the cron job that prunes it.

A Storage Box is cheap bulk storage that speaks SFTP, SCP, rsync, Samba, BorgBackup and WebDAV.
It has no compute, no S3 API and no shell. For Backvault it is an [SFTP](sftp.md) destination, and
for hosts that push without Backvault it is a target for
[`scripts/upload-sftp.sh`](../scripts.md).

## Sub-accounts, one per host

The main account (`u123456`) owns the whole box and every protocol. Do not give it to Backvault.
Create a sub-account instead, which gets:

- its own login name, `u123456-sub1`, `u123456-sub2` and so on
- its own password and its own authorized keys
- a home directory you choose, for example `/home/backvault`
- per protocol switches: SSH/SFTP, Samba, WebDAV, and external reachability
- optional read only access

One sub-account per host, or per environment, has two effects worth the small amount of extra
work. A leaked key from one server cannot read or delete another server's backups, and revoking
access is a single toggle rather than a password change everywhere.

Quotas are a property of the box, not of the sub-account. Sub-accounts share the same space, so
one job that grows without retention still fills the box for everyone. Retention is what keeps
that in check, see [retention](../retention.md).

Create the sub-account in the Hetzner console under the Storage Box, in the sub-accounts
section. Set a home directory, enable SSH/SFTP, enable external reachability, and leave Samba
and WebDAV off unless you need them.

## Port 23, not port 22

From outside the Hetzner network, a Storage Box accepts SSH, SFTP, SCP and rsync on **port 23**.
Port 22 is only reachable from inside Hetzner. This is the single most common reason a Storage
Box destination times out.

Check the connection before you configure anything in Backvault:

```bash
sftp -P 23 u123456-sub1@u123456.your-storagebox.de
```

Record the host key fingerprint while you are there, so the destination can verify it later:

```bash
ssh-keyscan -p 23 u123456.your-storagebox.de 2>/dev/null | ssh-keygen -lf -
```

Copy the `SHA256:...` value for the `host_key` field.

## Uploading an SSH key

Generate a key that exists only for this backup path:

```bash
ssh-keygen -t ed25519 -f ./backvault_storagebox -C backvault -N ''
```

A Storage Box has no general shell, so the ordinary `ssh-copy-id` cannot append to
`authorized_keys`. Use its SFTP mode with `-s`:

```bash
ssh-copy-id -s -p 23 -i ./backvault_storagebox.pub u123456-sub1@u123456.your-storagebox.de
```

The Hetzner console can also take a public key for a sub-account directly, which avoids typing
the password at all. Either way, confirm that the key works before moving on:

```bash
sftp -P 23 -i ./backvault_storagebox u123456-sub1@u123456.your-storagebox.de
```

If that lands in the sub-account's home directory without asking for a password, the key is
installed.

## Configuring the Backvault destination

Create a destination of kind `sftp` with these values:

| Field | Value |
|---|---|
| `host` | `u123456.your-storagebox.de` |
| `port` | `23`, not the default `22` |
| `user` | `u123456-sub1` |
| `auth` | `key`, changed from the default `password` |
| `private_key` | contents of `backvault_storagebox`, pasted whole, shown once `auth` is `key` |
| `key_passphrase` | empty, unless the key has one |
| `host_key` | the `SHA256:...` value from `ssh-keyscan`, in the advanced section |
| `base_path` | `backups` |

As it appears in a `backvault export` document:

```yaml
destinations:
  - name: Storage Box
    kind: sftp
    description: Hetzner Storage Box, sub-account sub1
    config:
      host: u123456.your-storagebox.de
      port: 23
      user: u123456-sub1
      auth: key
      private_key: '********'
      host_key: 'SHA256:9UFZ8pPcUu9mVe3oGkGtCaE9kBWjWeQPXDbFHBjNiM0'
      base_path: backups
    tags:
      - offsite
```

Use "Test connection" on the destination form before attaching it to a job. Then delete the
local copy of the private key: it is stored encrypted in Backvault and nothing else needs it. See
[security](../security.md).

Artifacts then land as:

```text
backups/<jobSlug>/<jobSlug>-<YYYYMMDD>-<HHMMSS>.<ext>[.gz|.zst][.age]
```

`base_path` is relative to the sub-account home directory, and it defaults to `.`, the home
directory itself. Backvault creates `backups` and the job directory below it one level at a time,
because SFTP has no recursive mkdir. Files are uploaded as `<name>.partial` and renamed when the
transfer completes, so a failed run never leaves a short file that looks like a finished backup.
Once the last artifact of a job is pruned, the empty job directory is removed as well, up to
`base_path`.

## The WebDAV alternative

A Storage Box also serves WebDAV at `https://u123456.your-storagebox.de`, with the same login
and password. Configure it as a [WebDAV](webdav.md) destination:

```yaml
destinations:
  - name: Storage Box over WebDAV
    kind: webdav
    config:
      url: https://u123456.your-storagebox.de
      user: u123456-sub1
      password: '********'
      base_path: backups
```

Prefer SFTP. WebDAV on a Storage Box is useful in two situations: the host running Backvault is
behind a firewall that allows outbound HTTPS and nothing else, or you want the same folder
mounted by a desktop tool that speaks WebDAV. It is slower for large files, it authenticates
with a password rather than a key, and it reports no checksum, so verification compares size
only.

WebDAV has to be enabled explicitly on the sub-account, like SSH.

## Pushing from a host without Backvault

A server that only runs cron can dump and upload on its own, then let a push job in Backvault
track whether it arrived. That pattern is described in [push and ingest](../push-and-ingest.md). To upload
straight to the Storage Box instead, use the standalone scripts.

Put the settings in `/etc/backvault/agent.env`, mode 600, owned by root:

```ini
SFTP_HOST=u123456.your-storagebox.de
SFTP_PORT=23
SFTP_USER=u123456-sub1
SFTP_KEY=/etc/backvault/storagebox_key
SFTP_KNOWN_HOSTS=/etc/backvault/known_hosts
SFTP_BASE_PATH=backups/db-prod
```

Create the `known_hosts` file once, so host key checking stays on:

```bash
ssh-keyscan -p 23 u123456.your-storagebox.de > /etc/backvault/known_hosts
chmod 600 /etc/backvault/storagebox_key
```

A nightly dump, upload and prune:

```bash
scripts/backup-postgres.sh \
  --database shop \
  --password-file /etc/backvault/pgpass \
  --to sftp

scripts/prune-sftp.sh --keep 14
```

`backup-postgres.sh` dumps, compresses, optionally encrypts, and hands the finished file to
`upload-sftp.sh`, which uses the same `SFTP_*` variables. `prune-sftp.sh` then keeps the newest
14 files in `SFTP_BASE_PATH`.

Install it with the cron helper, which writes one tagged entry and can remove it again:

```bash
scripts/install-cron.sh \
  --script backup-postgres.sh \
  --schedule "20 2 * * *" \
  --env-file /etc/backvault/agent.env \
  --args "--database shop --password-file /etc/backvault/pgpass --to sftp" \
  --tag db-prod \
  --log-file /var/log/backvault/db-prod.log \
  --yes

scripts/install-cron.sh \
  --script prune-sftp.sh \
  --schedule "50 2 * * *" \
  --env-file /etc/backvault/agent.env \
  --args "--keep 14" \
  --tag db-prod-prune \
  --log-file /var/log/backvault/db-prod.log \
  --yes
```

Run the prune after the upload, not before, so a failed upload does not cause the oldest good
copy to be deleted in the same night. `prune-sftp.sh` orders files by the `YYYYMMDD-HHMMSS`
timestamp in the name, never deletes a file whose name has no such timestamp, and never deletes
a `.partial` file. Check what it would do first:

```bash
scripts/prune-sftp.sh --keep 14 --dry-run
```

See [scripts](../scripts.md) for every flag.

## Use the sftp mode, not the ssh mode

`upload-sftp.sh` has a second transfer mode, `--mode ssh`, which streams a file through
`ssh host 'cat > file'`. A Storage Box does not offer a general shell, only the SFTP subsystem
and a small fixed set of maintenance commands, so that mode fails there. The same is true of an
`atmoz/sftp` container, which answers "This service allows sftp connections only", and that is
what the test suite for these scripts confirmed. Keep the default `--mode sftp`.

The default mode also handles the missing recursive mkdir: it issues one `-mkdir` per path
level and ignores the failures for levels that already exist, so a base path several directories
deep works on the first run.

## Snapshots are not retention, and retention is not snapshots

The Hetzner side can take snapshots of the whole Storage Box, manually or on a schedule. They
are worth having, and they are not a replacement for Backvault retention:

- A snapshot protects against a delete that should not have happened, including one caused by a
  wrong retention setting or a stolen key. It does not give you a database dump from a
  particular Tuesday unless a dump existed on the box that Tuesday.
- Retention decides which dumps exist at all. It cannot protect what it has already deleted,
  because the delete is what it was asked to do.
- Snapshots count against the quota. A box that is nearly full with backups does not have room
  for many snapshots of them.

Use both: retention to control how many dumps are kept, snapshots as a short window of undo for
the retention policy itself.

## Gotchas

- **Port 23.** Say it once more, because the symptom is a timeout with no useful message.
- **Quotas are shared.** Sub-accounts do not have separate quotas. Watch the used space on the
  box, not per job.
- **No shell means no remote commands.** Anything that expects to run a command on the target
  host, including `--mode ssh` and remote tar, does not work against a Storage Box.
- **External reachability is a switch.** A sub-account with external access turned off works
  from a Hetzner server and times out from anywhere else.
- **Read only sub-accounts break retention.** Uploads succeed and the prune stage fails, leaving
  runs in `warning`. That is a reasonable setup if you prune from elsewhere, and a confusing one
  otherwise.
- **A `.partial` file left on the box is rare.** Backvault deletes its own `.partial` when an
  upload fails, so one survives only if the process was killed mid transfer. It is overwritten
  by the next attempt and ignored by retention and by `prune-sftp.sh`. Delete it by hand only if
  you are cleaning up a job you removed.
- **Deleting a sub-account deletes its home directory.** Move the data first if you still need
  the artifacts, and remember that Backvault will report those artifacts as missing at the next
  verification, see [restore](../restore.md).
