#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backup-files.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backup-files.sh - archive files and directories with tar and deliver the archive.

Usage:
  backup-files.sh --path /var/www [--path /etc/nginx] [options]

Source options:
  --path PATH             file or directory to archive, repeatable, at least one required
  --exclude GLOB          tar exclude pattern, repeatable
  --exclude-from FILE     read exclude patterns from a file
  --base-dir DIR          change to this directory first and store relative paths
  --one-file-system       do not cross mount points
  --follow-symlinks       store the files symlinks point at instead of the links
  --tar-binary PATH       tar to use, default the first tar on PATH

USAGE
  bv_common_usage
  cat <<'USAGE'

Examples:
  backup-files.sh --path /var/www --exclude '*/cache/*' --job web-files
  backup-files.sh --path /etc --to local --dest-dir /srv/backups --compress gzip

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 5 tar failed, 6 packing failed,
  7 delivery failed.
USAGE
}

PATHS=()
EXCLUDES=()
EXCLUDE_FROM=""
BASE_DIR=""
ONE_FILE_SYSTEM=0
FOLLOW_SYMLINKS=0
TAR_BINARY=${TAR_BINARY:-tar}

while (($#)); do
  bv_common_arg "$@"
  if ((BV_ARG_CONSUMED)); then
    shift "$BV_ARG_CONSUMED"
    continue
  fi
  case $1 in
    --path) bv_need_value "$@"; PATHS+=("$2"); shift 2 ;;
    --exclude) bv_need_value "$@"; EXCLUDES+=("$2"); shift 2 ;;
    --exclude-from) bv_need_value "$@"; EXCLUDE_FROM=$2; shift 2 ;;
    --base-dir) bv_need_value "$@"; BASE_DIR=$2; shift 2 ;;
    --one-file-system) ONE_FILE_SYSTEM=1; shift ;;
    --follow-symlinks) FOLLOW_SYMLINKS=1; shift ;;
    --tar-binary) bv_need_value "$@"; TAR_BINARY=$2; shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps
bv_require "$TAR_BINARY"

((${#PATHS[@]} > 0)) || { usage >&2; bv_die "$BV_EXIT_USAGE" "at least one --path is required"; }
if [[ -n $BASE_DIR ]]; then
  [[ -d $BASE_DIR ]] || bv_die "$BV_EXIT_CONFIG" "not a directory: $BASE_DIR"
fi
if [[ -n $EXCLUDE_FROM ]]; then
  [[ -r $EXCLUDE_FROM ]] || bv_die "$BV_EXIT_CONFIG" "cannot read exclude file: $EXCLUDE_FROM"
fi

for entry in "${PATHS[@]}"; do
  if [[ -n $BASE_DIR ]]; then
    [[ -e "$BASE_DIR/$entry" ]] || bv_warn "no such path under $BASE_DIR: $entry"
  else
    [[ -e $entry ]] || bv_warn "no such path: $entry"
  fi
done

if [[ -z $BV_PREFIX ]]; then
  first=${PATHS[0]%/}
  BV_PREFIX=$(basename -- "${first:-files}")
  [[ -n $BV_PREFIX && $BV_PREFIX != / ]] || BV_PREFIX=files
fi

bv_prepare_pack
bv_validate_target

TAR_ARGS=(--create --file -)
[[ -n $BASE_DIR ]] && TAR_ARGS+=(--directory "$BASE_DIR")
((ONE_FILE_SYSTEM)) && TAR_ARGS+=(--one-file-system)
((FOLLOW_SYMLINKS)) && TAR_ARGS+=(--dereference)
[[ -n $EXCLUDE_FROM ]] && TAR_ARGS+=(--exclude-from "$EXCLUDE_FROM")
for pattern in ${EXCLUDES[@]+"${EXCLUDES[@]}"}; do
  TAR_ARGS+=(--exclude "$pattern")
done
TAR_ARGS+=(--)
TAR_ARGS+=("${PATHS[@]}")

NAME=$(bv_artifact_name "$BV_PREFIX" tar)

if ((BV_DRY_RUN)); then
  bv_plan "$NAME" "tar of ${PATHS[*]}"
  bv_info "plan: $TAR_BINARY ${TAR_ARGS[*]}"
  exit 0
fi

WORKDIR=$(bv_mktemp_dir)
SPOOL="$WORKDIR/spool"

bv_info "archiving ${#PATHS[@]} path(s) with $TAR_BINARY"
bv_dump_to_file "$SPOOL" "$TAR_BINARY" "${TAR_ARGS[@]}" || bv_die "$BV_EXIT_DUMP" "tar failed"

bv_finish "$SPOOL" "$NAME" "$WORKDIR"
