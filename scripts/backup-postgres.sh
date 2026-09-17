#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backup-postgres.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backup-postgres.sh - dump a PostgreSQL database with pg_dump, or a whole cluster with
pg_dumpall, and deliver the dump.

Usage:
  backup-postgres.sh --database app [--host db.internal] [options]
  backup-postgres.sh --all-databases [options]

Source options:
  --host HOST             server host, default localhost (env PGHOST)
  --port PORT             server port, default 5432 (env PGPORT)
  --user USER             role to connect as (env PGUSER)
  --database NAME         database to dump (env PGDATABASE)
  --all-databases         dump the whole cluster with pg_dumpall, always plain SQL
  --format custom|plain   pg_dump output format, default custom
  --schema-only           dump the schema without any rows
  --data-only             dump rows without the schema
  --schema NAME           restrict to this schema, repeatable
  --exclude-table GLOB    exclude tables matching the pattern, repeatable
  --sslmode MODE          libpq sslmode, for example require (env PGSSLMODE)
  --password-file FILE    file holding the password, read into PGPASSWORD for the child
  --pgpassfile FILE       use a libpq password file instead (env PGPASSFILE)
  --binary-path DIR       directory holding pg_dump and pg_dumpall
  --extra-arg ARG         extra argument passed to pg_dump, repeatable

USAGE
  bv_common_usage
  cat <<'USAGE'

Notes:
  The custom format produces a .dump file that pg_restore reads and that supports
  selective restore. The plain format produces .sql that psql replays. pg_dumpall
  always produces plain SQL, including roles and tablespaces.

  pg_dump must be at least as new as the server it talks to. Dumping a PostgreSQL 16
  server with a pg_dump 15 client fails with a server version mismatch.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 5 the dump failed,
  6 packing failed, 7 delivery failed.
USAGE
}

HOST=${PGHOST:-}
PORT=${PGPORT:-}
USER_NAME=${PGUSER:-}
DATABASE=${PGDATABASE:-}
ALL_DATABASES=0
FORMAT=custom
SCHEMA_ONLY=0
DATA_ONLY=0
SCHEMAS=()
EXCLUDE_TABLES=()
SSLMODE=${PGSSLMODE:-}
PASSWORD_FILE=""
PGPASSFILE_OPT=${PGPASSFILE:-}
BINARY_PATH=""
EXTRA_ARGS=()

while (($#)); do
  bv_common_arg "$@"
  if ((BV_ARG_CONSUMED)); then
    shift "$BV_ARG_CONSUMED"
    continue
  fi
  case $1 in
    --host) bv_need_value "$@"; HOST=$2; shift 2 ;;
    --port) bv_need_value "$@"; PORT=$2; shift 2 ;;
    --user) bv_need_value "$@"; USER_NAME=$2; shift 2 ;;
    --database) bv_need_value "$@"; DATABASE=$2; shift 2 ;;
    --all-databases) ALL_DATABASES=1; shift ;;
    --format) bv_need_value "$@"; FORMAT=$2; shift 2 ;;
    --schema-only) SCHEMA_ONLY=1; shift ;;
    --data-only) DATA_ONLY=1; shift ;;
    --schema) bv_need_value "$@"; SCHEMAS+=("$2"); shift 2 ;;
    --exclude-table) bv_need_value "$@"; EXCLUDE_TABLES+=("$2"); shift 2 ;;
    --sslmode) bv_need_value "$@"; SSLMODE=$2; shift 2 ;;
    --password-file) bv_need_value "$@"; PASSWORD_FILE=$2; shift 2 ;;
    --pgpassfile) bv_need_value "$@"; PGPASSFILE_OPT=$2; shift 2 ;;
    --binary-path) bv_need_value "$@"; BINARY_PATH=$2; shift 2 ;;
    --extra-arg) bv_need_value "$@"; EXTRA_ARGS+=("$2"); shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

case $FORMAT in
  custom | plain) ;;
  *) bv_die "$BV_EXIT_USAGE" "unknown --format: $FORMAT (want custom or plain)" ;;
esac
if ((SCHEMA_ONLY && DATA_ONLY)); then
  bv_die "$BV_EXIT_USAGE" "--schema-only and --data-only are mutually exclusive"
fi
if ((ALL_DATABASES == 0)) && [[ -z $DATABASE ]]; then
  usage >&2
  bv_die "$BV_EXIT_USAGE" "--database is required unless --all-databases is given"
fi

if ((ALL_DATABASES)); then
  BINARY=pg_dumpall
  EXTENSION=sql
else
  BINARY=pg_dump
  if [[ $FORMAT == custom ]]; then EXTENSION=dump; else EXTENSION=sql; fi
fi
if [[ -n $BINARY_PATH ]]; then
  BINARY="${BINARY_PATH%/}/$BINARY"
  [[ -x $BINARY ]] || bv_die "$BV_EXIT_DEPENDENCY" "not executable: $BINARY"
else
  bv_require "$BINARY" "install the PostgreSQL client tools"
fi

[[ -n $BV_PREFIX ]] || BV_PREFIX=$(if ((ALL_DATABASES)); then printf 'postgres-all'; else printf 'postgres-%s' "$DATABASE"; fi)

bv_prepare_pack
bv_validate_target

DUMP_ARGS=()
[[ -n $HOST ]] && DUMP_ARGS+=(--host "$HOST")
[[ -n $PORT ]] && DUMP_ARGS+=(--port "$PORT")
[[ -n $USER_NAME ]] && DUMP_ARGS+=(--username "$USER_NAME")
DUMP_ARGS+=(--no-password)
if ((ALL_DATABASES)); then
  ((SCHEMA_ONLY)) && DUMP_ARGS+=(--schema-only)
  ((DATA_ONLY)) && DUMP_ARGS+=(--data-only)
else
  if [[ $FORMAT == custom ]]; then
    DUMP_ARGS+=(--format=custom --compress=0)
  else
    DUMP_ARGS+=(--format=plain)
  fi
  ((SCHEMA_ONLY)) && DUMP_ARGS+=(--schema-only)
  ((DATA_ONLY)) && DUMP_ARGS+=(--data-only)
  for schema in ${SCHEMAS[@]+"${SCHEMAS[@]}"}; do
    DUMP_ARGS+=(--schema "$schema")
  done
  for table in ${EXCLUDE_TABLES[@]+"${EXCLUDE_TABLES[@]}"}; do
    DUMP_ARGS+=(--exclude-table "$table")
  done
fi
DUMP_ARGS+=(${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"})
((ALL_DATABASES)) || DUMP_ARGS+=(--dbname "$DATABASE")

NAME=$(bv_artifact_name "$BV_PREFIX" "$EXTENSION")

if ((BV_DRY_RUN)); then
  bv_plan "$NAME" "$BINARY of ${DATABASE:-all databases} on ${HOST:-local socket}"
  bv_info "plan: $BINARY ${DUMP_ARGS[*]}"
  exit 0
fi

export PGCONNECT_TIMEOUT=${PGCONNECT_TIMEOUT:-15}
[[ -n $SSLMODE ]] && export PGSSLMODE=$SSLMODE
if [[ -n $PGPASSFILE_OPT ]]; then
  [[ -r $PGPASSFILE_OPT ]] || bv_die "$BV_EXIT_CONFIG" "cannot read pgpass file: $PGPASSFILE_OPT"
  bv_check_permissions "$PGPASSFILE_OPT"
  export PGPASSFILE=$PGPASSFILE_OPT
fi
if [[ -n $PASSWORD_FILE ]]; then
  PGPASSWORD=$(bv_read_secret_file "$PASSWORD_FILE")
  export PGPASSWORD
fi

WORKDIR=$(bv_mktemp_dir)
SPOOL="$WORKDIR/spool"

bv_info "dumping ${DATABASE:-all databases} from ${HOST:-local socket}${PORT:+:$PORT} with $(basename -- "$BINARY")"
bv_dump_to_file "$SPOOL" "$BINARY" "${DUMP_ARGS[@]}" || bv_die "$BV_EXIT_DUMP" "the dump failed"

if [[ ! -s $SPOOL ]]; then
  bv_die "$BV_EXIT_DUMP" "the dump produced an empty file"
fi

bv_finish "$SPOOL" "$NAME" "$WORKDIR"
