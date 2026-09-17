# Encryption

Backvault encrypts the packed artifact with age, using a passphrase you choose per job, so that a
stolen bucket or a compromised storage box yields nothing readable.

## What is encrypted

Encryption is the last step of the pack stage, after compression:

```text
dump  ->  compress (gzip or zstd)  ->  encrypt (age)  ->  spool file  ->  upload
```

That order matters. Compressing first gives a much smaller artifact, because encrypted bytes do not
compress. The sha256 recorded on the run and on every artifact is the checksum of the final
encrypted file, which is exactly what the destination stores, so verification works without the
passphrase.

What is encrypted is the artifact, not the metadata. Filenames, sizes, timestamps, job names and
checksums are stored in Backvault's database in clear, and the filename is visible on the destination.
Someone with read access to your bucket learns that you back up a database called `shop-db` every
night and how large it is. They do not learn what is in it.

Encryption is per job. Set it in the job editor under Pack, or in the API:

```json
{
  "compression": "zstd",
  "encryption": "age",
  "encryptionPassphrase": "correct horse battery staple wharf"
}
```

## How the passphrase is handled

Backvault uses the age scrypt passphrase recipient. There is no key file to manage: the passphrase is
the key, and age derives the encryption key from it with scrypt.

- The passphrase is stored in the database encrypted with the Backvault master key (AES-256-GCM), like
  every other secret, as an `enc:v1:` prefixed value. See [security.md](security.md).
- Every API response replaces it with `********`. So does the YAML export, unless an admin asks for
  `?includeSecrets=1`, which is written to the audit log.
- Sending exactly `********` back in an update keeps the stored value, which is how the panel can
  save a job without ever holding the real passphrase.
- The passphrase never appears in run logs, notifications or error messages.

### Choosing one

Use a long passphrase from a password manager, five or more random words, or 32 random characters.
scrypt makes brute force expensive but not impossible, and the artifact is sitting in someone
else's storage where an attacker can try offline for as long as they like. Length is the only thing
that helps.

Use a different passphrase per job only if the jobs have genuinely different audiences. One
passphrase per environment is usually the right trade off between blast radius and the risk of
losing track of which passphrase belongs to which artifact.

### Losing it

If you lose the passphrase, the artifacts encrypted with it are gone. Backvault cannot recover them,
and neither can anyone else. Changing the passphrase on a job affects new artifacts only, old ones
stay encrypted with the old passphrase, and Backvault keeps using the job's current passphrase when
you download them. Pass `?passphrase=` on a download to override it for an artifact made under an
older passphrase, see [restore.md](restore.md).

Store the passphrase somewhere that survives the loss of the Backvault host, together with the master
key backup. A backup you cannot decrypt is not a backup.

## Extension conventions

The suffixes on an artifact filename say what was done to it, in the order it was done:

| Suffix | Meaning | Undo with |
|---|---|---|
| `.gz` | gzip compression | `gzip -d`, `gunzip`, `zcat` |
| `.zst` | zstd compression | `zstd -d`, `zstdcat` |
| `.age` | age encryption, scrypt passphrase recipient | `age --decrypt` |
| `.enc` | openssl AES-256-CBC, the standalone script fallback | `openssl enc -d` |

So `shop-db-20260917-020000.dump.zst.age` is a PostgreSQL custom format dump, compressed with zstd,
then encrypted with age. Undo in reverse: decrypt, decompress, restore.

Backvault itself only ever produces `.age`. The `.enc` suffix comes from the standalone scripts on
hosts without age, see below.

## Decrypting outside Backvault

This is the section to test before you need it, and to keep a copy of somewhere that is not the
Backvault host.

Everything below needs only the `age` CLI and standard compression tools. Install age from your
distribution (`apk add age`, `apt install age`, `brew install age`) or from
<https://github.com/FiloSottile/age>.

### Decrypt, then decompress

```bash
age --decrypt --output shop-db-20260917-020000.dump.zst \
  shop-db-20260917-020000.dump.zst.age
```

age asks for the passphrase on the terminal. Then undo the compression:

```bash
zstd -d shop-db-20260917-020000.dump.zst
```

or, for a gzip artifact:

```bash
gzip -d shop-db-20260917-020000.dump.gz
```

You now have `shop-db-20260917-020000.dump`, the raw output of `pg_dump`.

### In one pipeline

age writes to stdout when no `--output` is given, so a large artifact can go straight into the
restore tool without ever landing on disk unencrypted:

```bash
age --decrypt shop-db-20260917-020000.dump.zst.age \
  | zstd -dc \
  | pg_restore --host localhost --username postgres --dbname shop_restored --clean --if-exists
```

For a plain SQL dump, feed it to `psql` instead:

```bash
age --decrypt shop-db-20260917-020000.sql.gz.age \
  | gzip -dc \
  | psql --host localhost --username postgres --dbname shop_restored
```

For a file archive:

```bash
age --decrypt www-20260917-020000.tar.zst.age \
  | zstd -dc \
  | tar -x -C /srv/restore
```

age reads the passphrase from the terminal even when its input is a pipe, so these commands work
interactively. They do not work unattended, which is the subject of the next section.

## The standalone scripts

The scripts under `scripts/` run on hosts that do not have Backvault installed, and they have to
encrypt without a human at the keyboard. They take the passphrase from a file:

```bash
scripts/backup-postgres.sh --database shop \
  --compress zstd --encrypt age --passphrase-file /etc/backvault/passphrase \
  --to s3
```

The passphrase file should be mode 600 and owned by the user that runs the backup. The scripts warn
when it is readable by anyone else, and they never print its contents.

### Why age needs help here

The age CLI reads passphrases from the terminal device, not from standard input. There is no
environment variable for it, and no released version has a `--passphrase-file` flag: the 1.1.1
build in Debian bookworm, which is what the Backvault image ships, lists only `-p, --passphrase`.
Piping a passphrase into `age --passphrase` does not work.


`scripts/lib/common.sh` handles this in three steps:

1. If the installed `age` advertises `--passphrase-file` in its help output, use it directly.
2. Otherwise, if util-linux `script` is available, drive `age --encrypt --passphrase` through a
   pseudo terminal, feeding the passphrase twice, and use the command's real exit status.
3. Otherwise, fail with exit code 3 and tell you to install util-linux or use
   `--encrypt openssl`.

On a minimal container image, step 2 is usually the one that fails, because `script` lives in a
package that is not installed. Install util-linux, or use the openssl fallback.

### The openssl fallback

```bash
scripts/backup-files.sh --path /var/www \
  --compress gzip --encrypt openssl --passphrase-file /etc/backvault/passphrase \
  --to local --dest-dir /srv/backups
```

This produces `<name>.tar.gz.enc` using:

```bash
openssl enc -aes-256-cbc -pbkdf2 -salt -pass file:/etc/backvault/passphrase
```

Decrypt it with the matching command. This round trip is verified:

```bash
openssl enc -d -aes-256-cbc -pbkdf2 \
  -in www-20260917-020000.tar.gz.enc \
  -pass file:/etc/backvault/passphrase \
  | gzip -dc \
  | tar -t
```

Only the first line of the passphrase file is used, so a trailing newline is harmless.

### The `.enc` warning

**Backvault cannot decrypt a `.enc` artifact.** Backvault speaks age and nothing else. When a packed
upload arrives, Backvault reads the suffixes of the filename you sent and only understands `.age`,
`.zst` and `.gz`. So if you push an openssl encrypted file to a push job with `--packed`, Backvault
stores the bytes faithfully but records the artifact as neither compressed nor encrypted, and:

- The stored name is built from the job slug and the run time with the last extension it found, so
  `www.tar.gz.enc` becomes `www-20260917-020000.enc` and the `.tar.gz` part of the name is lost.
  The bytes are untouched, only the recorded name is shorter.
- Downloading it through the panel or `GET /artifacts/{id}/download` gives you the encrypted bytes,
  because Backvault has nothing recorded to undo. `?raw=1` changes nothing for such an artifact.
- Restore to path writes that still encrypted file to the target. Restore into the source fails,
  because the driver is handed ciphertext.
- You must decrypt it by hand with the command above.

If you want artifacts that Backvault can unpack, use `--encrypt age` on the host and give the job the
same passphrase, or push unencrypted over TLS without `--packed` and let Backvault do the packing.

Compression has no such limitation: Backvault understands `.gz` and `.zst` from any producer.

## Verifying you can still restore

Put this in your calendar, once a quarter:

1. Pick an artifact at random from the artifact list.
2. Download it with `?raw=1` so you get exactly what the destination holds.
3. Decrypt and decompress it on a machine that is not the Backvault host, using the passphrase from
   your password manager and not from the Backvault database.
4. Restore it into a scratch database or directory and look at the data.

Step 3 is the one that catches a lost passphrase while you still have alternatives. See
[restore.md](restore.md) for the full procedure, and [security.md](security.md) for backing up the
master key that protects the stored passphrase.
