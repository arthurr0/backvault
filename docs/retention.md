# Retention

Retention decides which artifacts survive and which are deleted from the destination, using the
same keep rules as restic: a number of recent backups plus one per hour, day, week, month and year.

## The rules

A retention policy has seven numbers. All of them default to 0, which means "this rule is not
used".

| Field | Meaning |
|---|---|
| `keepLast` | Keep the N newest artifacts, whatever their age |
| `keepHourly` | Keep the newest artifact in each of the N most recent hours that have one |
| `keepDaily` | Keep the newest artifact in each of the N most recent days that have one |
| `keepWeekly` | Keep the newest artifact in each of the N most recent weeks that have one |
| `keepMonthly` | Keep the newest artifact in each of the N most recent months that have one |
| `keepYearly` | Keep the newest artifact in each of the N most recent years that have one |
| `maxAgeDays` | Keep artifacts from the last N days and delete the rest, but only when no `keep` rule is set |

Four properties matter more than the individual numbers:

1. **Any rule is enough.** An artifact kept by one rule is kept, even if every other rule would
   discard it. The rules are a union, not an intersection.
2. **Buckets are distinct periods that contain a backup, not calendar slots.** `keepDaily 7` means
   the seven most recent days on which a backup exists, not the last seven calendar days. A job
   that was paused for a month still keeps seven daily backups from before the pause.
3. **`maxAgeDays` is a fallback, not an extra rule.** It is evaluated only when every `keep` field
   is 0. Set one single `keep` field and `maxAgeDays` is ignored completely, so it is an age based
   policy on its own rather than a ceiling on top of one.
4. **The newest artifact is never deleted.** A policy that would delete everything, or a
   misconfigured `maxAgeDays 1` on a weekly job, still leaves you the most recent backup on that
   destination.

Bucket boundaries are evaluated in UTC, which is also what artifact filenames carry.
Weeks start on Monday, months and years are calendar periods.

## What retention runs against

Retention runs once per destination, at the end of every backup run and every ingest run, over the
artifacts of that job that have status `present` on that destination. Artifacts on other
destinations, other jobs, or already `pruned` or `deleted`, are not considered. This means two
destinations can hold different numbers of artifacts, for example a local disk with `keepLast 3`
and an S3 bucket with a full daily and monthly policy, if you give them separate jobs.

An artifact removed by retention is deleted on the destination and its status becomes `pruned`. It
stays in the database and stays visible in the artifact list, so the history of what existed is not
lost when the file is.

## Worked example 1: a daily job

A PostgreSQL job runs every night at 02:00 UTC. It has been running since 1 March 2026, one
artifact per day, and today is 17 September 2026. The policy is:

```yaml
retention:
  keepLast: 0
  keepDaily: 7
  keepMonthly: 6
```

`keepDaily 7` protects the newest artifact of each of the seven most recent days that have one:

```text
2026-09-17  2026-09-16  2026-09-15  2026-09-14  2026-09-13  2026-09-12  2026-09-11
```

`keepMonthly 6` protects the newest artifact of each of the six most recent months that have one.
September is represented by 2026-09-17, which is already kept by the daily rule, so it costs
nothing extra:

```text
2026-09-17 (September)  2026-08-31 (August)  2026-07-31 (July)
2026-06-30 (June)       2026-05-31 (May)     2026-04-30 (April)
```

Twelve artifacts survive. Everything else, including all of March and every other day of April
through August, is deleted from the destination and marked `pruned`. March has no surviving
artifact because the six monthly buckets stop at April.

If you want a year of history, add `keepMonthly: 12`, or add `keepYearly: 3` to hold one artifact
per year on top.

## Worked example 2: an hourly job

A job dumps a small MySQL database every hour on the hour. It is 2026-09-17 09:15 UTC and the most
recent run was at 09:00. The policy is:

```yaml
retention:
  keepHourly: 24
  keepDaily: 7
```

`keepHourly 24` protects the 24 most recent hourly buckets, which for an hourly job is the last 24
artifacts:

```text
2026-09-17 09:00 down to 2026-09-16 10:00
```

`keepDaily 7` protects the newest artifact of each of the seven most recent days. For today that is
09:00, already kept. For every earlier day it is the 23:00 run:

```text
2026-09-17 09:00 (already kept)
2026-09-16 23:00   2026-09-15 23:00   2026-09-14 23:00
2026-09-13 23:00   2026-09-12 23:00   2026-09-11 23:00
```

Thirty artifacts survive out of the roughly 168 produced in a week. Note that 2026-09-16 23:00 is
kept twice over, by the hourly rule and by the daily rule, which is fine: the rules are a union.

## Worked example 3: maxAgeDays next to keepLast

A weekly job runs every Sunday. Ten artifacts exist, the oldest is 63 days old. The policy is:

```yaml
retention:
  keepLast: 10
  maxAgeDays: 30
```

Nothing is deleted, and `maxAgeDays` is not the reason. Because `keepLast` is set, the age rule is
never evaluated at all: `keepLast 10` protects all ten artifacts and the policy keeps 63 days of
history. Lowering `maxAgeDays` to 7 would change nothing.

This is the most common surprise in retention configuration. To let age decide, leave every `keep`
field at 0:

```yaml
retention:
  maxAgeDays: 30
```

Now every artifact from the last 30 days is kept and everything older is deleted. If the job stops
running for two months, the newest artifact is older than 30 days and is still kept, because the
newest artifact is never deleted.

If you want both a count and an age limit, you have to pick one. A weekly job with `keepLast: 4`
already spans roughly 28 days, which is the practical way to express a 30 day window with a count.

## Job policy and the default policy

Each job carries its own retention. If every field of a job's retention is 0, the job uses
`settings.defaultRetention` instead. This is how a fleet of jobs shares one policy: leave the job
retention empty and set the default once under Settings.

If the job retention is empty and the default retention is also empty, nothing is ever deleted.
That is a valid choice for a small archive, but storage grows without bound, so watch the storage
figures on the dashboard.

Setting a single field on a job overrides the default entirely. A job with `keepLast: 3` and
nothing else does not inherit the default's `keepDaily`, it keeps three artifacts. Copy the fields
you still want. The shipped default is `keepLast 7`, `keepDaily 7`, `keepWeekly 4`,
`keepMonthly 6`.

## Running retention by hand

Retention normally runs at the end of a backup. You can also run it on its own, which queues a
prune run:

```bash
curl -X POST -H "Authorization: Bearer $BACKVAULT_TOKEN" \
  https://backvault.example.com/api/v1/jobs/shop-db/prune
```

The endpoint takes a job id or a slug, needs the `admin` scope, and answers `202` with the queued
run. It takes the job lock, so it returns `423` while a backup of that job is running.

In the panel, open the job and use Prune. A prune run walks the same code as the retention stage,
records what it deleted in the run log, and emits the `prune.done` event when it ends in
`success`. Change a policy to
something stricter and run a prune to apply it immediately rather than waiting for the next backup.

To see what a policy would do before you commit to it, look at the artifact list for the job, sort
by date, and apply the rules by hand against the dates. There is no dry run mode on the API side.

## Pruning without Backvault

Hosts that upload straight to storage, without a Backvault job in the path, do not get retention.
Two scripts cover that case with a simpler policy, keep the newest N:

```bash
scripts/prune-s3.sh --prefix shop-db --keep 14
scripts/prune-sftp.sh --base-path backups/shop-db --keep 14
```

Both of them:

- Order files by the `YYYYMMDD-HHMMSS` timestamp in the filename, not by modification time. Upload
  order and clock skew on the storage side do not affect what is kept.
- Never touch a file whose name contains no such timestamp. A `README`, a checksum file or an
  unrelated object in the same prefix is left alone, and the count of skipped files is logged.
- Never touch `.partial` files, which are in flight uploads (`prune-sftp.sh`).
- Refuse `--keep 0`, so a typo cannot empty a bucket.
- Support `--dry-run`, which lists exactly what would be deleted and changes nothing.
- Support `--pattern`, so several backup sets can share one prefix and be pruned independently.

Run them from cron after the upload, in the same entry:

```bash
0 2 * * * . /etc/backvault/agent.env && /opt/backvault/scripts/backup-postgres.sh --database shop --to s3 && /opt/backvault/scripts/prune-s3.sh --prefix shop-db --keep 14 >> /var/log/backvault/shop-db.log 2>&1
```

Full flag reference in [scripts.md](scripts.md).

## Choosing a policy

Some starting points, adjust to the restore window you actually need:

| Situation | Policy |
|---|---|
| Small database, daily backup, one year of history | `keepDaily: 14`, `keepMonthly: 12` |
| Busy database, hourly backup | `keepHourly: 48`, `keepDaily: 14`, `keepMonthly: 6` |
| Static files, weekly backup | `keepLast: 8`, `keepMonthly: 6` |
| Compliance archive | `keepMonthly: 12`, `keepYearly: 7` |
| Scratch, local disk only | `keepLast: 3` |

Two numbers decide your storage bill: how many artifacts you keep, and how large each one is. The
artifact list shows both. Compression settings are on the job, see [concepts.md](concepts.md), and
storage totals per destination are on the dashboard.
