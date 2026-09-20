# Install

This page installs Backvault from a binary, a container image, docker compose or a systemd
unit, puts it behind a reverse proxy and explains how to upgrade it.

## 1. Requirements

Backvault runs on Linux, on x86_64 or arm64. It needs:

- A writable data directory. Everything Backvault owns lives there: the SQLite database, the
  master key and the working directory used to spool backups before upload.
- Enough free space in the working directory for the largest single backup. The engine
  spools a complete artifact to disk before it uploads, so a 40 GB database dump needs 40 GB
  of free space, less whatever compression saves.
- Outbound network access to your destinations.

No database server, no message broker and no external cache are required.

### External tools

Backvault shells out to the standard tool for each database. The tool is only needed if you use
that source driver, and it has to be installed on the host that runs Backvault (or inside the
container, see below).

| Source driver | Needs |
| --- | --- |
| `postgres` | `pg_dump`, `pg_dumpall`, and `pg_restore` or `psql` to restore |
| `mysql` | `mysqldump` or `mariadb-dump`, and `mysql` to restore |
| `mongodb` | `mongodump`, and `mongorestore` to restore |
| `redis` | `redis-cli` |
| `sqlite` | `sqlite3` if present, otherwise Backvault uses its built in SQLite backup |
| `docker` | the `docker` CLI and access to the Docker socket |
| `files`, `ssh`, `command`, `push` | nothing beyond the binary |

Ask Backvault what it can see:

```bash
backvault check
```

It reports each external tool, whether it was found on `PATH`, its path and version, and
which drivers use it, plus any problem it found in the configuration. The same information
is in the panel under Settings, Tools, and over the API at `GET /api/v1/meta/tools`.

The container image ships all of these, so every driver works out of the box. It is built on
`debian:bookworm-slim` and installs:

| Package | Provides |
| --- | --- |
| `postgresql-client-18` from the PGDG repository | `pg_dump`, `pg_dumpall`, `pg_restore`, `psql` |
| `mariadb-client` | `mariadb-dump`, `mysqldump`, `mariadb`, `mysql` |
| `mongodb-database-tools`, downloaded as a `.deb` for the build architecture | `mongodump`, `mongorestore` |
| `redis-tools` | `redis-cli` |
| `sqlite3` | `sqlite3` |
| `openssh-client` | `ssh` |
| `docker-ce-cli` from the Docker repository | `docker` |
| `age`, `zstd`, `gzip`, `tar` | unpacking an artifact by hand inside the container |
| `ca-certificates`, `tzdata`, `curl` | TLS, time zones, the healthcheck |

`backvault check` lists `ssh`, `tar`, `zstd`, `gzip` and `age` alongside the database clients,
but Backvault does not shell out to them: the SSH client, the tar writer, both compressors and
age are compiled into the binary. They are in the image so that you can inspect or unpack an
artifact from a shell in the container, and so that `backvault check` shows a clean list.

The PostgreSQL client is version 18, which dumps servers from 9.2 up to 18, so a
containerised Backvault handles a modern PostgreSQL without any extra work. Restoring into an
older server is still a separate question, see [troubleshooting.md](troubleshooting.md).


Both `linux/amd64` and `linux/arm64` are built. The apt repositories are selected with
`dpkg --print-architecture`. The MongoDB Database Tools are the exception: MongoDB does not
publish them for arm64 in its Debian apt repository, so the Dockerfile downloads the
matching `.deb` directly, pinned by the `MONGO_TOOLS_VERSION` build argument. The tools
Backvault does not use (`bsondump`, `mongoexport`, `mongofiles`, `mongoimport`, `mongostat`,
`mongotop`) are removed afterwards to keep the image small. Check the exact list in
`deploy/Dockerfile`, and run `backvault check` in the container to see what it found:

```bash
docker run --rm ghcr.io/arthurr0/backvault:latest check
```

## 2. Binary install

Every release at <https://github.com/arthurr0/backvault/releases> carries one archive per
platform plus a `checksums.txt`. The archives are named
`backvault_<version>_<os>_<arch>.tar.gz`, where the version has no leading `v`, the operating
system is `linux` or `darwin` and the architecture is `amd64` or `arm64`. Each archive
contains the `backvault` binary with the panel already embedded, `LICENSE`, `README.md` and
the `scripts/` directory.

```bash
version=0.1.0
arch=amd64
base=https://github.com/arthurr0/backvault/releases/download/v${version}

curl -fsSLO "${base}/backvault_${version}_linux_${arch}.tar.gz"
curl -fsSLO "${base}/checksums.txt"
sha256sum -c checksums.txt --ignore-missing

tar xzf "backvault_${version}_linux_${arch}.tar.gz"
sudo install -m 0755 backvault /usr/local/bin/backvault
backvault version
```

`sha256sum -c` must print `OK` for the archive before you unpack it. On macOS use
`shasum -a 256 -c checksums.txt --ignore-missing`.

If you would rather not pick the version yourself, `deploy/install.sh --download` resolves the
latest release, matches the architecture of the host, verifies the checksum and installs the
systemd service in one step. See section 5.

Start it with a data directory:

```bash
backvault serve --data-dir ./data --listen :8080
```

On the first start Backvault creates the data directory, generates a 32 byte master key at
`./data/master.key` with mode 0600 and creates the SQLite database. The log line about the
generated key appears once. Back that file up before you put anything real into Backvault,
because artifacts encrypted with a passphrase stored in the database cannot be read without
it. See `security.md`.

Open `http://localhost:8080`. A fresh install redirects to `/setup`, where you create the
first administrator. After that, `/setup` refuses to create another one.

For a permanent installation use the systemd unit in section 5 rather than running the
binary from a shell.

## 3. Docker

The image runs as the non-root user `backvault` (uid 1000), exposes port 8080 and stores
everything in the `/data` volume.

```bash
docker volume create backvault-data

docker run -d \
  --name backvault \
  --restart unless-stopped \
  -p 8080:8080 \
  -v backvault-data:/data \
  -e BACKVAULT_BASE_URL=https://backvault.example.com \
  -e BACKVAULT_LOG_FORMAT=json \
  -e TZ=Europe/Warsaw \
  ghcr.io/arthurr0/backvault:latest
```

`BACKVAULT_DATA_DIR=/data` and `BACKVAULT_LISTEN=:8080` are already set in the image. The image
has a healthcheck that requests `/healthz` every 30 seconds, so `docker ps` shows whether
Backvault considers itself healthy.

The entrypoint is the `backvault` binary and the default command is `serve`, so any other
subcommand works too:

```bash
docker exec -it backvault backvault check
docker run --rm -v backvault-data:/data ghcr.io/arthurr0/backvault:latest user list
```

### The Docker socket mount

The `docker` source driver archives a named volume by starting a helper container, which
means the Backvault container needs to talk to the Docker daemon:

```bash
docker run -d \
  --name backvault \
  -p 8080:8080 \
  -v backvault-data:/data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/arthurr0/backvault:latest
```

The image runs as user `backvault` (uid 1000), while the socket on the host is usually owned
by `root:docker` with mode 660, so the container also needs the host's `docker` group id:

```bash
docker run -d \
  --name backvault \
  -p 8080:8080 \
  -v backvault-data:/data \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  ghcr.io/arthurr0/backvault:latest
```

In `docker compose` the same thing is `group_add: ["<gid>"]` on the service, where the gid comes
from `getent group docker | cut -d: -f3`. Without it every `docker` call from a job fails with
`permission denied while trying to connect to the Docker daemon socket`. Then create a `docker`
source in the panel with mode `volume` and the volume name; the helper container that reads the
volume is started on the host daemon, so the volume does not have to be mounted into Backvault.

Understand what this costs before you do it. Access to the Docker socket is equivalent to
root on the host: anything that can talk to the socket can start a container that mounts the
host filesystem and writes to it. If Backvault is compromised, or a job runs a command you did
not intend, the socket is the path from the container to the host.

Safer options, in order of preference:

1. Do not use the `docker` source driver. Back the data up from inside the application
   instead, for example with a `postgres` source pointed at the container's published port.
2. Run Backvault directly on the host with systemd and give the service user access to the
   Docker group, which has the same power but at least does not add a container boundary
   that gives false comfort.
3. Put a socket proxy in front of the daemon that allows only the calls the driver makes.

`deploy/docker-compose.yml` deliberately does not carry the socket mount. Adding it is a
decision you make, not a default you inherit.

## 4. docker compose

The compose file lives at `deploy/docker-compose.yml`. From the `deploy` directory:

```bash
docker compose up -d
docker compose logs -f backvault
```

The `backvault` service publishes port 8080, keeps state in the `backvault-data` volume and sets
`BACKVAULT_BASE_URL=http://localhost:8080`. Change that to the URL you actually use before you
put the panel behind a proxy, because the base URL decides whether the session cookie gets
the `Secure` flag and what links in notifications point at.

To build the image from the checkout instead of pulling it:

```bash
docker compose build
docker compose up -d
```

### The demo profile

Two extra services, MinIO and PostgreSQL, sit behind the `demo` profile so they never start
by accident:

```bash
docker compose --profile demo up -d
```

This gives you an S3 endpoint at `http://localhost:9000` (access key `backvault`, secret key
`backvault-demo-secret`, path style addressing, any region) and a PostgreSQL 18 server at
`localhost:5432` with password `backvault-demo-secret` and a `demo` database. That is enough to
walk through `first-backup.md` end to end without touching production, and it exercises the
PostgreSQL 18 client in the image against a matching server.

The MinIO image is pulled from `quay.io/minio/minio`, not from Docker Hub. The Docker Hub
repository is no longer pullable anonymously, so a compose file that references
`minio/minio` fails with an access denied error on a clean machine.

Remove the demo services when you are done:

```bash
docker compose --profile demo down -v
```

## 5. systemd

### The install script

`deploy/install.sh` installs Backvault as a system service. Run it from a checkout to build
from source:

```bash
sudo ./deploy/install.sh
```

Or fetch a released binary instead, which needs no Go toolchain and no checkout:

```bash
curl -fsSL https://raw.githubusercontent.com/arthurr0/backvault/master/deploy/install.sh -o install.sh
sudo bash install.sh --download
```

`--download` resolves the latest release through the redirect on
`https://github.com/arthurr0/backvault/releases/latest`, downloads
`backvault_<version>_linux_<arch>.tar.gz` and `checksums.txt` for the architecture reported by
`uname -m`, refuses to continue unless the sha256 matches, and unpacks the binary into a
temporary directory that it removes on exit. Pin a version with
`sudo bash install.sh --version 0.1.0`, which implies `--download`. Outside a checkout the
script downloads by default, and when it is run from a checkout without `--download` it builds
from source as before. If no release has been published yet it says so and exits without
touching the host.

Run from outside a checkout, the script fetches `backvault.example.yaml`, the environment file
example and the systemd unit from the same tag on GitHub, so the files it installs match the
binary it installed.

It does the following, and nothing else:

1. Builds the binary if a Go toolchain is present, after building the admin panel with `npm`
   if `web/dist` is missing. With `--download` it fetches and verifies the release archive
   instead. You can also point it at a binary you already have:
   `sudo ./deploy/install.sh --binary /tmp/backvault`.
2. Creates the system group and user `backvault` if they do not exist.
3. Creates `/var/lib/backvault` and `/var/lib/backvault/work` mode 0750 owned by the service user,
   and `/etc/backvault` mode 0750 owned by root with the service group.
4. Installs the binary to `/usr/local/bin/backvault`.
5. Writes `/etc/backvault/backvault.yaml` and `/etc/backvault/backvault.env` from the examples, mode
   0640, only if they do not exist yet.
6. Installs `/etc/systemd/system/backvault.service`, reloads systemd, enables the unit and
   starts it.

The script is idempotent. Running it again upgrades the binary and reinstalls the unit,
keeps your configuration files, and never touches the data directory or the master key. Use
that for upgrades.

Useful flags: `--download`, `--version`, `--binary`, `--prefix`, `--data-dir`, `--config-dir`, `--user`, `--group`, `--listen`,
`--no-service` (install files only), `--no-start` (enable without starting), `--yes` (no
prompts) and `--dry-run` (print every step and change nothing). Start with `--dry-run` if
you want to see what it will do.

To remove it:

```bash
sudo ./deploy/install.sh --uninstall
```

That stops and disables the service, removes the unit and the binary, and keeps
`/var/lib/backvault` and `/etc/backvault`. Adding `--purge` also deletes the data directory, the
configuration and the service user, after a confirmation prompt. `--purge` destroys the
master key, which makes every encrypted artifact unreadable. There is no undo.

### The unit

The unit at `deploy/systemd/backvault.service` runs Backvault as a dedicated user with a tight
sandbox. Every directive and what it costs you:

| Directive | Effect | Consequence |
| --- | --- | --- |
| `User=backvault`, `Group=backvault`, `DynamicUser=no` | runs as a fixed unprivileged account | the data directory keeps stable ownership across restarts and upgrades |
| `NoNewPrivileges=yes` | the process and its children can never gain privileges | `sudo` and setuid binaries do not work in pre and post commands |
| `ProtectSystem=strict` | the whole filesystem is read only | writes are only possible where `ReadWritePaths` allows them |
| `ReadWritePaths=/var/lib/backvault` | the data directory is writable | a restore to a path outside it fails with permission denied |
| `ReadOnlyPaths=/etc/backvault` | configuration is readable, not writable | Backvault cannot rewrite its own config, which is intended |
| `ProtectHome=yes` | `/home`, `/root` and `/run/user` are invisible | a `files` source cannot read anything under `/home` |
| `PrivateTmp=yes` | a private `/tmp` and `/var/tmp` | a command hook cannot exchange files through `/tmp` with other services |
| `PrivateDevices=yes` | no physical device nodes | no raw device or tape backups |
| `ProtectProc=invisible`, `ProcSubset=pid` | other processes are hidden in `/proc` | a command hook cannot inspect other services |
| `ProtectClock`, `ProtectHostname`, `ProtectKernelLogs`, `ProtectKernelModules`, `ProtectKernelTunables`, `ProtectControlGroups` | the process cannot change host state | none for normal use |
| `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` | only IPv4, IPv6 and unix sockets | the Docker socket works, anything exotic does not |
| `RestrictNamespaces=yes` | no new namespaces | tools that need user namespaces do not run |
| `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`, `MemoryDenyWriteExecute` | standard hardening | none for Go binaries and the shipped tools |
| `CapabilityBoundingSet=`, `AmbientCapabilities=` | no capabilities at all | Backvault cannot bind a port below 1024, use a reverse proxy |
| `SystemCallFilter=@system-service`, `SystemCallArchitectures=native` | a restricted syscall set | unusual helper binaries may be denied with `EPERM` |
| `UMask=0077` | new files are owner only | artifacts written to a local destination are not world readable |
| `Restart=on-failure`, `RestartSec=5s` | restarts after a crash, not after a clean exit | a configuration error does not turn into a restart loop |

Never edit the installed unit directly, because the next run of the install script rewrites
it. Use a drop in:

```bash
sudo systemctl edit backvault
```

To restore into `/srv/restore` and read files under `/home`:

```ini
[Service]
ReadWritePaths=/srv/restore
ProtectHome=read-only
```

To use the `docker` source driver, add the service user to the `docker` group and let the
unit see the socket:

```bash
sudo usermod -aG docker backvault
sudo systemctl edit backvault
```

```ini
[Service]
SupplementaryGroups=docker
ReadWritePaths=/run/docker.sock
```

`AF_UNIX` is already allowed by `RestrictAddressFamilies`, so no change is needed there.
Read the warning in section 3 first: this gives the service account root on the host.

Apply changes and check the result:

```bash
sudo systemctl daemon-reload
sudo systemctl restart backvault
systemctl status backvault
journalctl -u backvault -f
systemd-analyze security backvault.service
```

## 6. Reverse proxy

Backvault speaks plain HTTP and expects a proxy to terminate TLS. Three things must be right:
server sent events must not be buffered, the ingest endpoint must accept large request
bodies, and the forwarded headers must match `BACKVAULT_TRUSTED_PROXIES`.

### Caddy

```caddyfile
backvault.example.com {
	encode zstd gzip

	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
		header_up X-Forwarded-Proto {scheme}
		header_up X-Forwarded-For {remote_host}
		transport http {
			read_timeout 1h
			write_timeout 1h
		}
	}
}
```

`flush_interval -1` disables response buffering, which is what keeps the live run log and
the dashboard event stream flowing. Caddy streams request bodies by default, so large ingest
uploads work without further configuration.

### nginx

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name backvault.example.com;

    ssl_certificate     /etc/letsencrypt/live/backvault.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/backvault.example.com/privkey.pem;

    client_max_body_size 0;
    proxy_request_buffering off;

    location / {
        proxy_pass http://127.0.0.1:8080;

        proxy_http_version 1.1;
        proxy_set_header Connection "";

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-Host  $host;

        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}

server {
    listen 80;
    server_name backvault.example.com;
    return 301 https://$host$request_uri;
}
```

What each setting is for:

- `proxy_buffering off` and `proxy_http_version 1.1` keep server sent events working. With
  the defaults, nginx buffers the response and the live log viewer shows nothing until the
  run finishes, then delivers everything at once.
- `proxy_read_timeout 3600s` stops nginx from cutting an idle event stream. Backvault sends a
  heartbeat every 20 seconds, so a shorter timeout also works, but a long running download
  or ingest needs the headroom anyway.
- `client_max_body_size 0` removes the body size limit. The default of 1 MB rejects every
  real backup pushed to the ingest endpoint with a 413. Set a concrete limit instead if you
  want a ceiling, for example `client_max_body_size 50g`.
- `proxy_request_buffering off` streams the upload straight through instead of writing the
  whole body to a temporary file on the proxy first.

### Matching the configuration

Set the base URL to the URL users type, and list the proxy addresses:

```bash
BACKVAULT_BASE_URL=https://backvault.example.com
BACKVAULT_TRUSTED_PROXIES=127.0.0.1,::1
```

`BACKVAULT_TRUSTED_PROXIES` decides whether Backvault believes `X-Forwarded-For`. If the proxy is
not listed, every request looks like it came from the proxy address, login rate limiting
becomes global instead of per client, and the audit log records the proxy address for every
action. If it is listed but something other than your proxy can reach the port directly,
a client can forge its own address, so keep the listen address bound to localhost when a
proxy is in front of it.

### The session cookie

The session cookie `backvault_session` is `HttpOnly` and `SameSite=Lax` always, and gets the
`Secure` flag only when the base URL starts with `https`. Serving the panel over plain HTTP
therefore leaves the cookie without `Secure`, which is what makes it work at all on
`http://localhost`, and is also why a production install must use https. Setting
`BACKVAULT_BASE_URL` to an https URL while actually serving http gives you a `Secure` cookie
the browser refuses to send back, which shows up as a login that appears to succeed and then
bounces straight back to the login page.

Because the cookie is `SameSite=Lax`, state changing requests from the browser also carry
`X-Requested-With: backvault` as a CSRF check. The panel sends it automatically. A proxy that
strips unknown request headers breaks every write from the panel while leaving reads working.

## 7. HTTPS

With Caddy there is nothing to do. The Caddyfile above requests and renews a certificate for
`backvault.example.com` automatically, as long as the name resolves to the host and ports 80
and 443 are reachable.

With nginx, get a certificate with certbot and let it manage renewal:

```bash
sudo certbot --nginx -d backvault.example.com
sudo systemctl reload nginx
```

If the base URL and the real URL disagree, these things break, in this order of how often
they catch people out:

1. Login loops, because of the `Secure` cookie mismatch described above.
2. Links in notifications point at the wrong host, so the run link in an email goes nowhere.
3. The ready to copy push commands in the panel show the wrong ingest URL, and the scripts
   copied from there fail with a connection error on another host.

Set `base_url` once, in the configuration file or the environment, and keep it equal to what
users type into the browser.

## 8. Upgrading and rolling back

Back up before every upgrade. Stop the service first so the SQLite database is not written
while you copy it:

```bash
sudo systemctl stop backvault
sudo tar czf /root/backvault-backup-$(date -u +%Y%m%d-%H%M%S).tar.gz \
  -C /var/lib backvault \
  -C /etc backvault
sudo systemctl start backvault
```

What matters in that archive: `master.key`, the SQLite database `backvault.db` with its `-wal`
and `-shm` files, and `/etc/backvault`. Without the master key, every encrypted secret in the
database and every artifact encrypted with a job passphrase is unreadable. `security.md`
describes a consistent copy that does not require stopping the service.

Upgrade with the same install script, or by replacing the binary:

```bash
sudo ./deploy/install.sh --yes
```

```bash
sudo systemctl stop backvault
sudo install -m 0755 backvault /usr/local/bin/backvault
sudo systemctl start backvault
backvault version
```

With containers, pull the new tag and recreate:

```bash
docker compose pull
docker compose up -d
```

Database migrations run at startup and are forward only. Rolling back to an older binary
after a migration has run is not supported, because the older binary does not know the new
schema. To roll back, restore the data directory from the archive you took before the
upgrade and then start the older binary:

```bash
sudo systemctl stop backvault
sudo rm -rf /var/lib/backvault
sudo tar xzf /root/backvault-backup-20260917-020000.tar.gz -C /var/lib backvault
sudo install -m 0755 /root/backvault-previous /usr/local/bin/backvault
sudo systemctl start backvault
```

Runs that were in progress when the service stopped are marked failed at the next start.
Scheduled runs that were missed while the service was down are executed once immediately if
the missed slot is less than 24 hours old and the job is enabled.
