# FAQ

Questions operators ask before adopting Backvault, answered honestly.

## Does it deduplicate?

No. Every run produces one complete artifact. There is no block level deduplication, no
content addressed store and no incremental chain. A daily 10 GB database dump kept for 30
days costs 30 times the compressed size.

This is a deliberate trade. Each artifact is a self contained file you can decompress,
decrypt and restore with standard tools, without Backvault and without a repository format. The
cost is storage. If storage matters more than independence, run restic or borg as a
`command` source and let Backvault schedule, monitor and account for it.

## Can it do incremental backups?

Not natively, for the same reason. Two ways to get the effect:

- Use a source that produces an incremental artifact itself, through a `command` or `ssh`
  source. Backvault stores and tracks whatever the command writes to standard output.
- For files, use a tool built for it (restic, borg, rsync with hard links) and let Backvault run
  it and watch it.

What Backvault does give you for a full backup strategy is the retention algorithm, so keeping
7 daily, 4 weekly and 12 monthly full backups is a configuration rather than a script. See
`retention.md`.

## How big can a backup be?

There is no hard limit in Backvault. The practical limits are:

- Free space in the working directory. The engine spools the complete packed artifact to
  disk before upload, so the largest single backup must fit, along with any concurrent run.
- The destination. S3 uploads use multipart with 16 MiB parts, so the 5 GiB single object
  limit does not apply. The standalone `upload-s3.sh` script does apply it, because its pure
  curl uploader performs a single PUT.
- Time. The job timeout defaults to six hours.

Backups in the tens of gigabytes are routine. Hundreds of gigabytes work if the disk and the
timeout allow it.

## Where is the data stored?

Two different things:

- Backvault's own state, in one SQLite database in the data directory, in WAL mode, together
  with the master key. Nothing else, no external database, no cache, no queue.
- The backups, on the destinations you configure. Backvault does not keep a copy unless one of
  the destinations is a local directory.

See `security.md` for what to copy when you back up Backvault itself.

## Can I run it without Docker?

Yes. A single static binary and a systemd unit is the primary deployment. See `install.md`
sections 2 and 5. The container image is a convenience, mostly because it already contains
the PostgreSQL 18 client, `mariadb-dump`, the MongoDB Database Tools and the rest.

## Can several instances share a database?

No. SQLite in WAL mode allows several readers and one writer on one machine, but Backvault also
runs a scheduler, holds job locks in process and keeps an in process event bus. Two
instances against the same file would run the same job twice and corrupt each other's
assumptions.

Run one instance per database. For several sites, run several instances, one per site. For
high availability, accept that a backup manager being down for an hour means the runs are
executed late rather than lost: missed schedules are run once at startup if the slot is less
than 24 hours old.

## Can it back up a database on another host without installing anything there?

Yes, three ways, in order of least footprint:

1. **Direct connection.** Point a `postgres`, `mysql`, `mongodb` or `redis` source at the
   remote host and port. Backvault runs the client tool locally and connects over the network.
   Nothing is installed on the database host. Use TLS, and a read only backup role.
2. **The `ssh` source.** Backvault connects with a key and runs a command on the remote host,
   for example `pg_dump` or `tar`, and reads its standard output. The remote host needs the
   tool and an account, nothing else.
3. **The standalone scripts.** Copy `scripts/` to the host, add a cron entry and push to a
   push job. This is the right answer when the database host cannot be reached from Backvault,
   only the other way round, which is the usual firewall direction. See
   `push-and-ingest.md`.

## What happens if the server is down when a schedule fires?

The run does not happen at that moment. At the next start, Backvault looks for scheduled slots
it missed, logs them, and runs the job once immediately if the job is enabled and the missed
slot is less than 24 hours old. Older misses are logged and skipped rather than causing a
thundering herd after a long outage.

Independently of that, the overdue watcher marks a job overdue when its next run time passed
without a run, and emits a `job.overdue` event once per incident, which your notification
channels can deliver. That is the mechanism that tells you the backup did not happen.

## Can I use it only as a receiver for scripts?

Yes. Create jobs with the `push` source. They are never scheduled. They wait for artifacts
posted to `POST /api/v1/ingest/{jobSlug}` with a token carrying the `ingest` scope, and they
apply the job's retention, notifications and overdue detection to whatever arrives.

Set `expectedIntervalMinutes` on the job and Backvault will tell you when a host stops pushing,
which is the failure a plain cron job can never report. The token doing the pushing needs the
`ingest` scope, and its job slug restriction is enforced on exactly this endpoint. The panel
shows ready to copy curl and `backvault push` commands for each push job. See
`push-and-ingest.md` and `scripts.md`.

## How do I move an installation to a new host?

Backvault's state is a directory. Move the directory.

```bash
sudo systemctl stop backvault
sudo tar czf /tmp/backvault-move.tar.gz -C /var/lib backvault -C /etc backvault
```

On the new host, install the same version, restore the archive into place, keep the
ownership and modes, then start it:

```bash
sudo ./deploy/install.sh --binary /tmp/backvault --no-start
sudo tar xzf /tmp/backvault-move.tar.gz -C /var/lib backvault
sudo chown -R backvault:backvault /var/lib/backvault
sudo systemctl start backvault
```

The master key has to come along, otherwise every stored credential and every encrypted
artifact is unreadable. Afterwards, update `base_url` if the hostname changed, and check that
the destinations are still reachable from the new network position with Test connection.

If you would rather rebuild from scratch, export the configuration first
(`backvault export --include-secrets`) and import it on the new host. That moves sources,
destinations, jobs and channels but not the run history or the artifact index.

## Is it multi tenant?

No. There are two roles, administrator and viewer, and they apply to the whole instance.
Every administrator sees every job, source, destination and artifact. API tokens can be
restricted to a list of job slugs, which is enough to give an automation narrow access, but
it is not a tenancy boundary for people.

For separate teams that must not see each other's backups, run separate instances.

## How do I automate configuration?

Use export and import. Both are available in the panel, over the API and from the CLI:

```bash
backvault export > backvault-config.yaml
backvault export --include-secrets > backvault-config-full.yaml
backvault import backvault-config.yaml --dry-run
backvault import backvault-config.yaml
```

The export contains sources, destinations, jobs and notification channels as YAML, with
secrets masked as `********` unless you ask for them. Import upserts by slug or name, so
running it repeatedly converges rather than duplicating, and `--dry-run` returns the plan
without changing anything. That makes the file reasonable to keep in version control, as long
as you keep the masked version there and inject the secrets separately.

An export with secrets decrypts your whole estate into one file and is recorded in the audit
log. Treat it accordingly.

## What happens when a destination is full or unreachable?

Per destination, with retries: each upload is retried `job.retries` times, with a backoff
that starts at `job.retryDelaySeconds`, doubles per attempt and is capped at ten minutes.
Both default to 0, which means one attempt per destination and a fallback delay of five
seconds once you do set a retry count, so raise `retries` on the jobs whose destination is
known to be flaky.

After the retries:

- If some destinations succeeded and others failed, the run ends as `warning`. Artifacts
  exist for the destinations that accepted the upload, so a copy is still safe, and the
  notification says which destination failed.
- If every destination failed, the run ends as `failed` and no artifact is recorded.

Either way the working directory is cleaned up, the failure is in the run log with the
provider's error message, and the configured channels receive an event. A destination that
is merely full behaves the same as one that is unreachable: the upload fails, the run is
marked, and you get told.

Retention only ever deletes artifacts that are recorded as present on that destination, and
never the newest successful one, so a failing destination does not cause the previous good
backup to be pruned away.
