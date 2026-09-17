# MongoDB

Dumps a MongoDB deployment with `mongodump --archive`, which produces a single stream that
`mongorestore` reads back.

Kind `mongodb`. Extension `archive`. Requires the MongoDB Database Tools (`mongodump`, and
`mongorestore` for restores) on the Backvault host. Capabilities: test, restore.

## Configuration fields

Connection:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `uri` | secret | yes | | Connection string, for example `mongodb://backup:secret@db.internal:27017/?authSource=admin`. Stored encrypted, masked in every API response. |

Options:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `database` | string | no | | Database to dump. Empty means every database the user can read. |
| `collection` | string | no | | Single collection to dump. Requires `database`. |

Advanced:

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `extra_args` | list | no | | Extra arguments appended to `mongodump`, one per line, for example `--readPreference=secondaryPreferred`. |
| `binary_path` | path | no | | Directory holding `mongodump` and `mongorestore`, when they are not on `PATH`. |

The whole connection string is treated as a secret because it usually carries the password.
Backvault registers the URI, and the password inside it, as redacted strings, so neither
appears in the run log.

## Example

```yaml
sources:
  - name: Orders cluster
    kind: mongodb
    config:
      uri: "********"
      database: shop
      extra_args:
        - --readPreference=secondaryPreferred
```

## Restore

`download` gives you the archive stream once it is unpacked. Replay it yourself:

```bash
mongorestore --uri "mongodb://root:secret@db.internal:27017" --archive=shop-20260917-020000.archive
```

To restore next to the live data rather than over it, rename the namespaces:

```bash
mongorestore --uri "mongodb://root:secret@db.internal:27017" \
  --archive=shop-20260917-020000.archive \
  --nsFrom 'shop.*' --nsTo 'shop_restored.*'
```

`path` writes the unpacked archive to a file on the Backvault host.

`source` pipes the archive into `mongorestore --archive` against the job's source, or
against `targetSourceId` when it names another `mongodb` source. `mongorestore` adds
documents to existing collections rather than replacing them, so a restore onto a live
database leaves whatever is already there. Pass `--drop` through the restore parameters
when you want the collections replaced.

## Gotchas

**A password in the URI is visible to every user on the host.** Backvault calls
`mongodump --uri=...`, which puts the whole string in the process list for as long as the
dump runs. The URI is scrubbed from the run log, but not from `ps`. On a shared host, give
the backup user its own credentials, or use a certificate through `extra_args`.

**The tools are a separate package.** Recent `mongo` server images and distribution
packages do not always include `mongodump`. Install the MongoDB Database Tools, and check
**Settings, Tools** to see whether Backvault found them.

**`--archive` with no filename writes to stdout.** That single stream is exactly what
Backvault stores, which is why the artifact is one file rather than a directory tree.

**A plain dump is not point in time.** Without `--oplog` the archive mixes documents read
at slightly different moments. On a replica set, add `--oplog` through `extra_args` and
restore with `--oplogReplay` for a consistent snapshot.

**Dump from a secondary where you can.** `--readPreference=secondaryPreferred` keeps the
read load off the primary. The secondary has to be current, otherwise the backup silently
lags.

**Users and roles live in `admin`.** A dump of one application database does not carry the
accounts that use it. Back up `admin` as well if you need a full rebuild.

## See also

- [../scripts.md](../scripts.md) for `backup-mongodb.sh`, which uses the same config file
  approach on the database host.
- [../restore.md](../restore.md) for the restore modes.
