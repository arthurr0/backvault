#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backup-mysql.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backup-mysql.sh - dump a MySQL or MariaDB database with mysqldump or mariadb-dump and
deliver the dump.

Usage:
  backup-mysql.sh --database app [--host db.internal] [options]
  backup-mysql.sh --all-databases [options]

Source options:
  --host HOST             server host, default localhost
  --port PORT             server port, default 3306
  --socket PATH           unix socket instead of host and port
  --user USER             user to connect as
  --password-file FILE    file holding the password, passed through a defaults file
  --database NAME         database to dump
  --all-databases         dump every database
  --ignore-table SPEC     db.table to skip, repeatable
  --no-single-transaction dump without a consistent snapshot, for MyISAM tables
  --no-routines           skip stored procedures and functions
  --no-triggers           skip triggers
  --no-events             skip scheduled events
  --no-tablespaces        add --no-tablespaces, needed without the PROCESS privilege
  --ssl-mode MODE         value for --ssl-mode, for example REQUIRED
  --binary-path DIR       directory holding mysqldump or mariadb-dump
  --extra-arg ARG         extra argument passed to the dump tool, repeatable

USAGE
  bv_common_usage
  cat <<'USAGE'

Notes:
  The password is written to a temporary defaults file with mode 600 and passed with
  --defaults-extra-file, so it never appears in the process list. The file is removed
  when the script exits.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 5 the dump failed,
  6 packing failed, 7 delivery failed.
USAGE
}

HOST=""
PORT=""
SOCKET=""
USER_NAME=""
PASSWORD_FILE=""
DATABASE=""
ALL_DATABASES=0
IGNORE_TABLES=()
SINGLE_TRANSACTION=1
ROUTINES=1
TRIGGERS=1
EVENTS=1
NO_TABLESPACES=0
SSL_MODE=""
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
    --socket) bv_need_value "$@"; SOCKET=$2; shift 2 ;;
    --user) bv_need_value "$@"; USER_NAME=$2; shift 2 ;;
    --password-file) bv_need_value "$@"; PASSWORD_FILE=$2; shift 2 ;;
    --database) bv_need_value "$@"; DATABASE=$2; shift 2 ;;
    --all-databases) ALL_DATABASES=1; shift ;;
    --ignore-table) bv_need_value "$@"; IGNORE_TABLES+=("$2"); shift 2 ;;
    --no-single-transaction) SINGLE_TRANSACTION=0; shift ;;
    --no-routines) ROUTINES=0; shift ;;
    --no-triggers) TRIGGERS=0; shift ;;
    --no-events) EVENTS=0; shift ;;
    --no-tablespaces) NO_TABLESPACES=1; shift ;;
    --ssl-mode) bv_need_value "$@"; SSL_MODE=$2; shift 2 ;;
    --binary-path) bv_need_value "$@"; BINARY_PATH=$2; shift 2 ;;
    --extra-arg) bv_need_value "$@"; EXTRA_ARGS+=("$2"); shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

if ((ALL_DATABASES == 0)) && [[ -z $DATABASE ]]; then
  usage >&2
  bv_die "$BV_EXIT_USAGE" "--database is required unless --all-databases is given"
fi

BINARY=""
if [[ -n $BINARY_PATH ]]; then
  for candidate in mysqldump mariadb-dump; do
    if [[ -x "${BINARY_PATH%/}/$candidate" ]]; then
      BINARY="${BINARY_PATH%/}/$candidate"
      break
    fi
  done
  [[ -n $BINARY ]] || bv_die "$BV_EXIT_DEPENDENCY" "no mysqldump or mariadb-dump in $BINARY_PATH"
else
  if bv_have mysqldump; then
    BINARY=mysqldump
  elif bv_have mariadb-dump; then
    BINARY=mariadb-dump
  else
    bv_die "$BV_EXIT_DEPENDENCY" "neither mysqldump nor mariadb-dump found, install the MySQL or MariaDB client"
  fi
fi

[[ -n $BV_PREFIX ]] || BV_PREFIX=$(if ((ALL_DATABASES)); then printf 'mysql-all'; else printf 'mysql-%s' "$DATABASE"; fi)

bv_prepare_pack
bv_validate_target

DEFAULTS_FILE=""
if [[ -n $PASSWORD_FILE || -n $USER_NAME || -n $HOST || -n $PORT || -n $SOCKET ]]; then
  DEFAULTS_FILE=$(bv_mktemp_file)
  chmod 600 -- "$DEFAULTS_FILE"
  {
    printf '[client]\n'
    [[ -n $USER_NAME ]] && printf 'user=%s\n' "$USER_NAME"
    [[ -n $HOST ]] && printf 'host=%s\n' "$HOST"
    [[ -n $PORT ]] && printf 'port=%s\n' "$PORT"
    [[ -n $SOCKET ]] && printf 'socket=%s\n' "$SOCKET"
    if [[ -n $PASSWORD_FILE ]]; then
      printf 'password=%s\n' "$(bv_read_secret_file "$PASSWORD_FILE")"
    fi
  } >"$DEFAULTS_FILE"
fi

DUMP_ARGS=()
[[ -n $DEFAULTS_FILE ]] && DUMP_ARGS+=("--defaults-extra-file=$DEFAULTS_FILE")
((SINGLE_TRANSACTION)) && DUMP_ARGS+=(--single-transaction)
((ROUTINES)) && DUMP_ARGS+=(--routines)
((TRIGGERS)) && DUMP_ARGS+=(--triggers)
((EVENTS)) && DUMP_ARGS+=(--events)
((NO_TABLESPACES)) && DUMP_ARGS+=(--no-tablespaces)
[[ -n $SSL_MODE ]] && DUMP_ARGS+=("--ssl-mode=$SSL_MODE")
for spec in ${IGNORE_TABLES[@]+"${IGNORE_TABLES[@]}"}; do
  DUMP_ARGS+=("--ignore-table=$spec")
done
DUMP_ARGS+=(${EXTRA_ARGS[@]+"${EXTRA_ARGS[@]}"})
if ((ALL_DATABASES)); then
  DUMP_ARGS+=(--all-databases)
else
  DUMP_ARGS+=(--databases "$DATABASE")
fi

NAME=$(bv_artifact_name "$BV_PREFIX" sql)

if ((BV_DRY_RUN)); then
  bv_plan "$NAME" "$(basename -- "$BINARY") of ${DATABASE:-all databases} on ${HOST:-local socket}"
  bv_info "plan: $(basename -- "$BINARY") ${DUMP_ARGS[*]//$DEFAULTS_FILE/<defaults file>}"
  exit 0
fi

WORKDIR=$(bv_mktemp_dir)
SPOOL="$WORKDIR/spool"

bv_info "dumping ${DATABASE:-all databases} from ${HOST:-local socket}${PORT:+:$PORT} with $(basename -- "$BINARY")"
bv_dump_to_file "$SPOOL" "$BINARY" "${DUMP_ARGS[@]}" || bv_die "$BV_EXIT_DUMP" "the dump failed"

if [[ ! -s $SPOOL ]]; then
  bv_die "$BV_EXIT_DUMP" "the dump produced an empty file"
fi

bv_finish "$SPOOL" "$NAME" "$WORKDIR"
