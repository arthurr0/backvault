# Docker volume or container

Archives a Docker volume through a throwaway helper container, or stores the output of a
command run inside a container that is already running.

Kind `docker`. Extension `tar` for volumes, configurable in exec mode. Requires the
`docker` client on the Backvault host and access to the Docker socket. Capabilities: test,
restore for volumes, remote.

## Configuration fields

Mode:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `mode` | select | no | `volume` | `volume` archives a named volume, `exec` runs a command inside a container that is already running. |
| `volume` | string | yes when `mode` is `volume` | | Name of the Docker volume, as shown by `docker volume ls`. Only shown in volume mode. |
| `container` | string | yes when `mode` is `exec` | | Name or id of the running container. Only shown in exec mode. |
| `command` | text | yes when `mode` is `exec` | | Command run through the container shell. It must write the backup to standard output and exit with code 0. Only shown in exec mode. |
| `extension` | string | no | `dump` | Extension recorded for the artifact, for example `dump`, `sql` or `tar`. Only shown in exec mode; volume mode always produces `tar`. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `image` | string | no | `alpine:3.20` | Helper image used to tar the volume. It must provide `tar` and must already be pullable from this host. Only shown in volume mode. |
| `exec_user` | string | no | | Passed as `docker exec --user`. Only shown in exec mode. |
| `shell` | string | no | `/bin/sh` | Interpreter inside the container, called as `<shell> -c <command>`. Only shown in exec mode. |
| `docker_host` | string | no | | Sets `DOCKER_HOST` for the `docker` calls, for example `unix:///var/run/docker.sock`. Leave it empty to use the host default. |
| `binary_path` | path | no | | Full path to the `docker` binary, or the directory holding it, when it is not on `PATH`. |

There is no exclude option. Volume mode archives the whole volume; narrow the backup by
splitting the data across volumes, or use exec mode with a command that selects what you
want.

In volume mode Backvault runs the equivalent of:

```bash
docker run --rm --network none --volume myvolume:/data:ro alpine:3.20 tar -C /data -cf - .
```

The volume is mounted read only and the helper gets no network, so it cannot damage the
volume or talk to anything.

Backvault looks for `docker` first and falls back to `podman`, so a rootless Podman host works
with the same configuration as long as the socket is reachable.

## Example

```yaml
sources:
  - name: Grafana volume
    kind: docker
    config:
      mode: volume
      volume: grafana-storage
      image: alpine:3.20
```

Exec mode, dumping a database from inside its own container:

```yaml
sources:
  - name: Shop database in Docker
    kind: docker
    config:
      mode: exec
      container: shop-db
      command: pg_dump --format=custom --username postgres shop
      extension: dump
      exec_user: postgres
```

## Run on a host

Pick a host in the **Run on** select and the same commands run on that machine, against its
Docker daemon: `docker run --rm -v <volume>:/data:ro <image> tar -C /data -cf - .` in volume
mode, `docker exec ...` in exec mode, and the helper container with `-i` for a restore. The
host needs `docker` or `podman` and an account allowed to talk to the socket, either through
the `docker` group or through the host's sudo option.

`binary_path` points at the binary on the host, so it is the place to select `podman` there,
and `docker_host` sets `DOCKER_HOST` for the remote command. **Test connection** asks the
host's daemon for its version and then checks that the volume or the running container exists
on that machine.

## Restore

`download` and `path` work for both modes and give you the bytes the helper or the
container produced.

`source` restores a volume: Backvault runs a helper container with the volume mounted
read-write and extracts the archive into it. With overwrite on, the helper empties the
volume before extracting; without it, the archive is unpacked over whatever is there. The
restore parameter `volume` sends the archive to a different volume than the one configured.
Stop the containers that use the volume first, otherwise a running process overwrites what
was restored. Exec mode has no `source` restore, because Backvault cannot know what command
would undo the dump. Restore it by hand:

```bash
docker exec -i shop-db pg_restore --clean --if-exists --username postgres --dbname shop < shop-20260917-020000.dump
```

## Gotchas

**Backvault needs the Docker socket.** In the container image that means mounting
`/var/run/docker.sock`, which gives Backvault effective root on the host.
Mount the socket with `-v /var/run/docker.sock:/var/run/docker.sock` and add the host's `docker`
group to the container with `--group-add "$(stat -c %g /var/run/docker.sock)"` (compose:
`group_add`), because the image runs as uid 1000 and the socket is normally `root:docker` 660.
See the install page for the full command. Decide whether that
trade is acceptable before you enable it, and read the note in
[../security.md](../security.md). Under systemd, add the `docker` group to the unit with
`SupplementaryGroups=docker`.

**A volume copied while a database runs is crash consistent at best.** The files are what
the database would find after a power cut. Most engines recover from that, but a dump from
the matching driver is the better backup. Use volume mode for things without a dump tool,
such as uploaded media or a Grafana data directory.

**Stop the container for a clean copy.** When a real dump is not available, stop the
container, run the job, start it again. The standalone
[`backup-docker-volume.sh`](../scripts.md) has `--stop-container` for exactly this, and
starts the container again even when the archive fails.

**The helper image is not pulled for you.** The helper runs with `--network none`, so the
image has to be present on the host already. Keep `image` on a small image you control and
pull it as part of provisioning.

**Exec mode needs the tool inside the container.** `docker exec ... pg_dump` uses the
client that image ships, which is usually the right version for that server. That is the
main reason to prefer exec mode over a `postgres` source pointed at a mapped port.

**Exec mode needs the container running.** The connection test fails with
`container ... is not running` when it is stopped, and so does the backup. A container that
only runs during business hours needs its job scheduled inside that window.

**Bind mounts are not volumes.** A directory mounted from the host is backed up with the
[`files`](files.md) source, not with this one.

## See also

- [../scripts.md](../scripts.md) for `backup-docker-volume.sh`.
- [files.md](files.md) for host paths and bind mounts.
- [../security.md](../security.md) for what mounting the Docker socket implies.
