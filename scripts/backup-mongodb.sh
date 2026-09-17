#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backup-mongodb.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backup-mongodb.sh - dump a MongoDB deployment with mongodump --archive and deliver the
archive.

Usage:
  backup-mongodb.sh --uri mongodb://db.internal:27017 [options]
  backup-mongodb.sh --host db.internal --database app [options]

Source options:
  --uri URI               connection string, takes precedence over host and port
  --host HOST             server host, default localhost
  --port PORT             server port, default 27017
  --user USER             user to authenticate as
  --password-file FILE    file holding the password, passed through a tools config file
  --auth-db NAME          authentication database, default admin
  --database NAME         database to dump, default every database
  --collection NAME       collection to dump, requires --database
  --exclude-collection NAME   collection to skip, repeatable, requires --database
  --read-preference PREF  for example secondaryPreferred
  --oplog                 include the oplog for a point in time snapshot of a replica set
  --binary-path DIR       directory holding mongodump
  --extra-arg ARG         extra argument passed to mongodump, repeatable

USAGE
  bv_common_usage
  cat <<'USAGE'

Notes:
  The password is written to a temporary MongoDB tools config file with mode 600 and
  passed with --config, so it never appears in the process list. A password embedded in
  --uri is visible to every user on the host, prefer --password-file.

  mongodump --archive writes a single stream, which is what Backvault stores and what
  mongorestore --archive reads back.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 5 the dump failed,
  6 packing failed, 7 delivery failed.
USAGE
}

URI=""
HOST=""
PORT=""
USER_NAME=""
PASSWORD_FILE=""
AUTH_DB=""
DATABASE=""
COLLECTION=""
EXCLUDE_COLLECTIONS=()
READ_PREFERENCE=""
OPLOG=0
BINARY_PATH=""
EXTRA_ARGS=()

while (($#)); do
  bv_common_arg "$@"
  if ((BV_ARG_CONSUMED)); then
    shift "$BV_ARG_CONSUMED"
    continue
  fi
  case $1 in
    --uri) bv_need_value "$@"; URI=$2; shift 2 ;;
    --host) bv_need_value "$@"; HOST=$2; shift 2 ;;
    --port) bv_need_value "$@"; PORT=$2; shift 2 ;;
    --user) bv_need_value "$@"; USER_NAME=$2; shift 2 ;;
    --password-file) bv_need_value "$@"; PASSWORD_FILE=$2; shift 2 ;;
    --auth-db) bv_need_value "$@"; AUTH_DB=$2; shift 2 ;;
    --database) bv_need_value "$@"; DATABASE=$2; shift 2 ;;
    --collection) bv_need_value "$@"; COLLECTION=$2; shift 2 ;;
    --exclude-collection) bv_need_value "$@"; EXCLUDE_COLLECTIONS+=("$2"); shift 2 ;;
    --read-preference) bv_need_value "$@"; READ_PREFERENCE=$2; shift 2 ;;
    --oplog) OPLOG=1; shift ;;
    --binary-path) bv_need_value "$@"; BINARY_PATH=$2; shift 2 ;;
    --extra-arg) bv_need_value "$@"; EXTRA_ARGS+=("$2"); shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

if [[ -n $COLLECTION && -z $DATABASE ]]; then
  bv_die "$BV_EXIT_USAGE" "--collection requires --database"
fi
if ((${#EXCLUDE_COLLECTIONS[@]} > 0)) && [[ -z $DATABASE ]]; then
  bv_die "$BV_EXIT_USAGE" "--exclude-collection requires --database"
fi

BINARY=mongodump
if [[ -n $BINARY_PATH ]]; then
  BINARY="${BINARY_PATH%/}/mongodump"
  [[ -x $BINARY ]] || bv_die "$BV_EXIT_DEPENDENCY" "not executable: $BINARY"
else
  bv_require mongodump "install the MongoDB Database Tools"
fi

if [[ -z $BV_PREFIX ]]; then
  if [[ -n $COLLECTION ]]; then
    BV_PREFIX="mongodb-$DATABASE-$COLLECTION"
  elif [[ -n $DATABASE ]]; then
    BV_PREFIX="mongodb-$DATABASE"
  else
    BV_PREFIX=mongodb-all
  fi
fi

bv_prepare_pack
bv_validate_target

CONFIG_FILE=""
if [[ -n $PASSWORD_FILE ]]; then
  CONFIG_FILE=$(bv_mktemp_file)
  chmod 600 -- "$CONFIG_FILE"
  printf 'password: %s\n' "$(bv_read_secret_file "$PASSWORD_FILE")" >"$CONFIG_FILE"
fi

DUMP_ARGS=(--archive)
[[ -n $CONFIG_FILE ]] && DUMP_ARGS+=("--config=$CONFIG_FILE")
if [[ -n $URI ]]; then
  DUMP_ARGS+=("--uri=$URI")
else
  [[ -n $HOST ]] && DUMP_ARGS+=("--host=$HOST")
  [[ -n $PORT ]] && DUMP_ARGS+=("--port=$PORT")
fi
[[ -n $USER_NAME ]] && DUMP_ARGS+=("--username=$USER_NAME")
[[ -n $AUTH_DB ]] && DUMP_ARGS+=("--authenticationDatabase=$AUTH_DB")
[[ -n $DATABASE ]] && DUMP_ARGS+=("--db=$DATABASE")
[[ -n $COLLECTION ]] && DUMP_ARGS+=("--collection=$COLLECTION")
for name in ${EXCLUDE_COLLECTIONS[@]+"${EXCLUDE_COLLECTIONS[@]}"}; do
  DUMP_ARGS+=("--excludeCollection=$name")
done
[[ -n $READ_PREFERENCE ]] && DUMP_ARGS+=("--readPreference=$READ_PREFERENCE")
((OPLOG)) && DUMP_ARGS+=(--oplog)
DUMP_ARGS+=(${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"})

NAME=$(bv_artifact_name "$BV_PREFIX" archive)

if ((BV_DRY_RUN)); then
  bv_plan "$NAME" "mongodump of ${DATABASE:-every database} on ${URI:-${HOST:-localhost}}"
  bv_info "plan: mongodump ${DUMP_ARGS[*]//$CONFIG_FILE/<config file>}"
  exit 0
fi

WORKDIR=$(bv_mktemp_dir)
SPOOL="$WORKDIR/spool"

bv_info "dumping ${DATABASE:-every database} from ${URI:-${HOST:-localhost}} with mongodump"
bv_dump_to_file "$SPOOL" "$BINARY" "${DUMP_ARGS[@]}" || bv_die "$BV_EXIT_DUMP" "the dump failed"

if [[ ! -s $SPOOL ]]; then
  bv_die "$BV_EXIT_DUMP" "the dump produced an empty file"
fi

bv_finish "$SPOOL" "$NAME" "$WORKDIR"
