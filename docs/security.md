# Security

This page describes what Backvault protects, how it protects it, and what you have to do
yourself.

## Threat model in plain terms

Backvault holds the credentials to your databases and your storage, and it holds the backups
themselves. That makes it a high value target: whoever controls Backvault can read every
database it can reach and every artifact it has stored.

What Backvault defends against:

- An attacker who reads the SQLite database file, without the master key, learns job names
  and schedules but not passwords, access keys or passphrases.
- An attacker who gets an API token limited to `ingest` for one job can push artifacts to
  that job and nothing else.
- An attacker on the network between Backvault and a destination sees only encrypted transport,
  and with job encryption enabled, cannot read the artifact even if they obtain it.
- A stolen backup artifact encrypted with age is useless without the passphrase.

What it does not defend against, and you have to handle:

- Anyone with root on the Backvault host. They have the master key file and the database.
- Anyone with the master key and a copy of the data directory.
- A compromised source. Backvault backs up whatever the source gives it.
- Loss of the passphrase. There is no recovery path, by design.

## The master key

Backvault encrypts every secret field with AES-256-GCM using a 32 byte master key.

- By default the key lives at `master.key` in the data directory, created on first start
  with mode 0600 and owned by the service user.
- `BACKVAULT_MASTER_KEY_FILE` moves it somewhere else, for example onto a separate mount or a
  secrets volume.
- `BACKVAULT_MASTER_KEY` supplies it directly as 32 bytes in hex or base64, which is how you
  inject it from a secret manager. Passing it through the environment means it is visible to
  anything that can read `/proc/<pid>/environ` for the process, which is root and the
  service user itself.

If the key leaks, every stored credential must be treated as compromised: rotate the
database passwords, the storage access keys and the SSH keys that Backvault holds, and rotate
the job passphrases for future backups. Old artifacts encrypted with a leaked passphrase
stay readable by whoever has both, so they have to be deleted or re-encrypted.

If the key is lost, the secrets in the database cannot be decrypted. Job passphrases stored
in Backvault are gone with it, and artifacts encrypted with them are unreadable. Sources and
destinations have to be reconfigured with fresh credentials. This is why the key belongs in
your own password manager as well as on the host.

## Secrets at rest

Encrypted with the master key:

- Every driver field marked as a secret: database passwords, S3 secret keys, SFTP private
  keys and their passphrases, WebDAV passwords.
- `encryptionPassphrase` on a job.
- Secret fields on notification channels: SMTP passwords, webhook signing secrets, bot
  tokens.

User passwords are not encrypted, they are hashed with argon2id, which is one way by design.
API tokens are stored as hashes too, so a database dump does not yield usable tokens.

Everywhere a secret leaves the system it is replaced with `********`:

- API responses for sources, destinations and channels.
- The configuration export at `GET /api/v1/export`.
- Log lines and error messages.

Sending `********` back in an update means "keep the stored value", so the panel can edit an
object without ever handling the real secret. Only a value that actually changed is sent.

The one exception is `GET /api/v1/export?includeSecrets=1`, which an administrator can use
to produce a portable configuration file with real secrets in it. That export is written to
the audit log as `export.secrets`. Treat the result like a password file: it decrypts your
entire estate.

## API tokens

Tokens authenticate with `Authorization: Bearer bvt_...` and carry scopes:

| Scope | Allows |
| --- | --- |
| `read` | reading jobs, runs, artifacts, dashboards and event streams |
| `run` | queueing runs, cancelling runs, queueing a verify, and everything `read` allows |
| `ingest` | pushing artifacts to a push job |
| `admin` | everything, including configuration and user management |

`admin` satisfies every scope check and `run` also satisfies `read`. The other scopes are
matched exactly, so an `ingest` token cannot read the job list.

A token can also be restricted to a list of job slugs, which is how a host that pushes one
database gets a token that can do exactly that and nothing else. Read the restriction
narrowly: it is enforced on ingest (`POST /api/v1/ingest/{jobSlug}`) and on running a job
(`POST /api/v1/jobs/{id}/run`), and nowhere else. It does not restrict prune, restore,
verify, cancel, delete or any read endpoint, so a token with those scopes still reaches
every job. An empty slug list means every job.

The secret is `bvt_` followed by 32 base62 characters, 36 in total. It is shown once, when
the token is created. Backvault stores only its sha256 hash plus the first 12 characters as a
visible prefix, so a lost token cannot be recovered, only replaced. Rules worth following:

- One token per host or per automation, never a shared one, so revoking is surgical.
- The narrowest scope that works. A cron job that pushes a dump needs `ingest`, not `admin`.
- Restrict to job slugs whenever the token is used by a single job.
- Set an expiry where the API supports it, and rotate on a schedule.
- Revoke immediately when a host is decommissioned: `DELETE /api/v1/tokens/{id}`, or the
  Settings page. The token stops working at once.
- On a host, keep the token in a file with mode 0600 and pass it with `--token-file`, not on
  a command line.

## Sessions and CSRF

Browser sessions use the `backvault_session` cookie on `Path=/`: `HttpOnly` so scripts cannot
read it, `SameSite=Lax` so it is not sent on cross site form posts, and `Secure` when
`base_url` starts with `https://`. The cookie value is 32 random bytes and only its sha256 is
stored. A session lives 30 days and slides forward as it is used.

Logging in is rate limited to five attempts per minute per client address, as a sliding
window. Over the limit the answer is `429` with the code `rate_limited`, and there is no
`Retry-After` header to read, so a client has to back off on its own. A successful login
clears the counter for that address. Only `/auth/login` is rate limited, `/setup` is not.

Because `SameSite=Lax` still allows top level navigations, every state changing request
authenticated by the cookie must also carry `X-Requested-With: backvault`. A cross origin form
post cannot set that header without a preflight, which the browser refuses. The panel sends
it on every request. Token authenticated requests do not need it, because a token is never
sent automatically by a browser.

If a proxy strips unknown request headers, reads keep working and every write fails. That is
the signature of this problem.

## Passwords

Passwords are hashed with argon2id, with parameters chosen to be expensive enough on server
hardware. A password has to be at least 10 characters and at most 512. Changing your own
password requires the current one. An administrator can set another user's password without
it, which is an audited action. Either way every session of that user is deleted, so a
password change logs the account out everywhere. The last administrator account cannot be
deleted or demoted, so an installation can never lock itself out.

## Reverse proxy and client addresses

Backvault trusts `X-Forwarded-For` only from addresses listed in `BACKVAULT_TRUSTED_PROXIES`. This
matters in two places:

- Login rate limiting. Without the setting, every login attempt appears to come from the
  proxy, so one attacker exhausts the limit for everyone, or the limit protects nothing
  depending on which way you look at it.
- The audit log records the client address for every mutation. Without the setting, every
  entry says the proxy address and the log loses most of its value.

Configure it together with the proxy, and bind Backvault itself to localhost so nothing can
reach the port directly and forge the header:

```bash
BACKVAULT_LISTEN=127.0.0.1:8080
BACKVAULT_BASE_URL=https://backvault.example.com
BACKVAULT_TRUSTED_PROXIES=127.0.0.1,::1
```

## Network exposure

Do not put the panel on the public internet over plain HTTP. The session cookie has no
`Secure` flag on http, the login form sends a password in clear text, and every API token
used against it travels in clear text too.

Reasonable exposures, in order:

1. Not exposed at all, reachable over a VPN or an SSH tunnel.
2. Exposed over https with a certificate, behind a proxy, ideally with an allow list.
3. Exposed over https with an additional authentication layer in the proxy.

The ingest endpoint is the one that often has to be reachable from outside, because the
hosts that push backups live elsewhere. It authenticates with a bearer token, so it is safe
to expose over https, and the tokens that use it should be restricted to their job slugs.

## File permissions

| Path | Mode | Owner |
| --- | --- | --- |
| data directory | 0750 | service user |
| `master.key` | 0600 | service user |
| `backvault.db` and its `-wal` and `-shm` files | 0600 | service user |
| work directory | 0750 | service user |
| `/etc/backvault` | 0750 | root, service group |
| `/etc/backvault/backvault.yaml` and `backvault.env` | 0640 | root, service group |

The systemd unit sets `UMask=0077`, so files Backvault creates, including artifacts on a local
destination, are not readable by other accounts. Check the result after an install:

```bash
sudo ls -la /var/lib/backvault /etc/backvault
```

## The Docker socket

Mounting `/var/run/docker.sock` into the Backvault container, or adding the service user to the
`docker` group, grants root on the host. Anything that can talk to the daemon can start a
privileged container that mounts `/` and writes to it. The `docker` source driver needs this
access. Before you grant it, see whether a database level backup gets you the same result
without it, and read `install.md` section 3.

## Backing up Backvault itself

Backvault does not back itself up. If you lose the data directory you lose the master key, the
run history, the artifact index and every configured source, destination and job. The
artifacts on your storage survive, and without the master key the encrypted ones are
unreadable.

Copy these, from the data directory:

| File | Why |
| --- | --- |
| `master.key` | without it every encrypted secret and artifact is lost |
| `backvault.db` | configuration, artifact index, run history, users and tokens |
| `backvault.db-wal`, `backvault.db-shm` | recent transactions not yet folded into the main file |
| `/etc/backvault/backvault.yaml`, `/etc/backvault/backvault.env` | how this instance is configured |

The work directory does not need to be copied. It only holds spool files for runs in
progress.

The clean way, with the service stopped:

```bash
sudo systemctl stop backvault
sudo tar czf /srv/offsite/backvault-$(date -u +%Y%m%d-%H%M%S).tar.gz \
  -C /var/lib backvault \
  -C /etc backvault
sudo systemctl start backvault
```

The consistent way without stopping anything, using SQLite's own online backup, which is
safe while the database is in use and folds the write ahead log into one file:

```bash
sudo -u backvault sqlite3 /var/lib/backvault/backvault.db ".backup '/var/lib/backvault/backvault-backup.db'"
sudo tar czf /srv/offsite/backvault-$(date -u +%Y%m%d-%H%M%S).tar.gz \
  -C /var/lib/backvault backvault-backup.db master.key \
  -C /etc backvault
sudo rm -f /var/lib/backvault/backvault-backup.db
```

Do not copy `backvault.db` with `cp` while the service is running and keep only that file. In
WAL mode the most recent transactions live in `backvault.db-wal`, and a copy without it can be
missing the last minutes of work or be unreadable.

Store the result somewhere Backvault does not control, and keep the master key in your password
manager as well, separately from the archive. An encrypted archive whose key is inside the
archive is one artifact, not two.

You can automate this with Backvault itself: a `command` source that runs the `sqlite3 .backup`
line above, or a `files` source over the data directory with the service stopped in a pre
command hook, writing to a destination other than the local disk. Restoring then means
unpacking the archive into a fresh install's data directory before the first start.

## The standalone scripts

The scripts in `scripts/` run on hosts that do not have Backvault installed, usually from cron.
Their security rules:

- Secrets are read from files, never taken as command line arguments, because a command line
  is visible to every account on the host through `ps`. The API token comes from
  `--token-file` or the environment, the S3 keys from the environment, the database password
  from `--password-file`, and the age or openssl passphrase from `--passphrase-file`.
- The MySQL password goes into a temporary defaults file with mode 0600 and is passed with
  `--defaults-extra-file`. The MongoDB password goes into a temporary tools config file the
  same way. Both files are removed when the script exits, including on failure.
- The scripts warn when a passphrase file, a private key or a password file is readable by
  other accounts. Fix it rather than ignoring the warning:

  ```bash
  chmod 600 /etc/backvault/passphrase /etc/backvault/sftp_key /etc/backvault/agent.env
  chown backup:backup /etc/backvault/agent.env
  ```

- An environment file sourced from a cron entry must be 0600 and owned by the account that
  runs the job. Everything in it, the API token, the S3 secret key, the database password,
  is readable by anyone who can read the file.
- SFTP host key checking is on by default. Use `--known-hosts` with a file you populated
  yourself, or `--host-key-checking accept-new` on first contact. Turning it off entirely
  removes the protection against a man in the middle, and the scripts log a warning when you
  do.
- Artifacts encrypted with `--encrypt age` or `--encrypt openssl` are encrypted before they
  leave the host, so the destination never sees the plaintext. See `encryption.md`.

`scripts.md` documents every flag and environment variable.

## Hardening checklist

- [ ] The panel is behind https, or not reachable from the internet at all.
- [ ] `BACKVAULT_BASE_URL` matches the URL users type.
- [ ] `BACKVAULT_TRUSTED_PROXIES` lists the proxy, and Backvault listens on localhost.
- [ ] The master key is backed up somewhere other than the Backvault host.
- [ ] The data directory is 0750 and `master.key` is 0600.
- [ ] Each automation has its own token, with the narrowest scope and a job slug restriction.
- [ ] Job passphrases are stored in a password manager, not only in Backvault.
- [ ] At least one destination is somewhere a compromise of the Backvault host cannot delete.
- [ ] The Docker socket is not mounted unless the `docker` source driver is genuinely needed.
- [ ] Notifications are configured, so a failing job is noticed.
- [ ] `systemd-analyze security backvault.service` has been reviewed after any drop in override.
- [ ] A restore has been performed from an artifact, without using Backvault, in the last quarter.
