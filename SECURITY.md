# Security policy

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | yes |
| older | no releases exist |

Backvault is at 0.1.x. Fixes go into the next patch release on that line, and there is no
backporting to earlier tags. Run the latest 0.1.x release before reporting a problem.

## Reporting a vulnerability

Report privately, not in a public issue.

1. Open <https://github.com/arthurr0/backvault/security/advisories/new>, which is the
   "Report a vulnerability" button under the Security tab of the repository.
2. Describe what an attacker can do, the version you tested and the steps to reproduce it. A
   proof of concept, a request log or a short patch all help.
3. You get an acknowledgement within 7 days and an assessment within 14 days.

If GitHub private vulnerability reporting is unavailable to you, open a normal issue that
says only that you have a security report and asks for a private channel, with no details in
it.

Please give a reasonable window to ship a fix before publishing. Credit is given in the
advisory and the changelog unless you ask otherwise.

## In scope

- Authentication and session handling: login, the session cookie, the CSRF header
  requirement, the login rate limit, the setup endpoint after the first administrator exists.
- API tokens: scope enforcement, the per-token job restrictions on `run` and `ingest`, token
  storage.
- Secrets at rest: anything that causes a stored credential, a job passphrase or the master
  key to be written unencrypted, logged, or returned by the API.
- The ingest endpoint: path handling of the supplied filename, checksum handling, resource
  use with a hostile upload.
- Restore and download: writing outside the requested target path, path traversal from an
  artifact name, and the extraction of tar artifacts.
- Command construction in the source drivers: anything that turns a configured field into an
  unintended command or argument on the host.
- The panel: cross site scripting, clickjacking, anything that lets one authenticated user
  act with another user's privileges.
- The container image and the systemd unit: a default that grants more than the documented
  privileges.

## Out of scope

- Anyone with root on the Backvault host, or with both the master key and a copy of the data
  directory. That is explicitly outside the threat model in
  [docs/security.md](docs/security.md).
- Mounting the Docker socket into the container. The documentation states that this is
  equivalent to root on the host, and it is never a default.
- Running the panel over plain HTTP on a public network, or setting `BACKVAULT_BASE_URL` to
  something other than the URL users actually type.
- Backups being readable at a destination you configured without compression, encryption or
  bucket level controls.
- Reports produced only by a scanner, denial of service through sheer volume, missing
  hardening headers with no exploit path, and issues in a dependency that Backvault does not
  reach.

## The key and secret model in short

Every secret field, that is database passwords, storage access keys, SSH keys, WebDAV
passwords, notification tokens and job encryption passphrases, is encrypted with AES-256-GCM
using a 32 byte master key that lives in `master.key` in the data directory, or comes from
`BACKVAULT_MASTER_KEY_FILE` or `BACKVAULT_MASTER_KEY`, and the API never returns a decrypted
secret. Backup artifacts are encrypted separately with an age passphrase per job, applied on
the way to the destination, so an artifact taken from storage is useless without that
passphrase, and losing the master key means losing both the stored credentials and any
passphrase Backvault held for you.

The full model, including key rotation and what to do if the key leaks, is in
[docs/security.md](docs/security.md).
