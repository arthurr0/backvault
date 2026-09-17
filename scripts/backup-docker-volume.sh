#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backup-docker-volume.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backup-docker-volume.sh - archive a Docker volume by running tar in a throwaway helper
container, and deliver the archive.

Usage:
  backup-docker-volume.sh --volume pgdata [options]

Source options:
  --volume NAME           volume to archive, required
  --path PATH             path inside the volume to archive, default .
  --exclude GLOB          tar exclude pattern, repeatable
  --helper-image IMAGE    image providing tar, default alpine:3.20
  --docker-binary NAME    docker or podman, default docker (env DOCKER_BINARY)
  --stop-container NAME   stop this container during the archive and start it again after,
                          repeatable, for databases that must not be copied while running
  --run-arg ARG           extra argument for the helper container run, repeatable

USAGE
  bv_common_usage
  cat <<'USAGE'

Notes:
  The volume is mounted read only, so the helper container cannot damage it. Archiving a
  running database volume gives you the files as they were at that moment, which for most
  database engines is a crash-consistent copy, not a clean backup. Prefer a real dump, or
  use --stop-container.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 5 the archive failed,
  6 packing failed, 7 delivery failed.
USAGE
}

VOLUME=""
INNER_PATH="."
EXCLUDES=()
HELPER_IMAGE=${HELPER_IMAGE:-alpine:3.20}
DOCKER_BINARY=${DOCKER_BINARY:-docker}
STOP_CONTAINERS=()
RUN_ARGS=()
STOPPED=()

while (($#)); do
  bv_common_arg "$@"
  if ((BV_ARG_CONSUMED)); then
    shift "$BV_ARG_CONSUMED"
    continue
  fi
  case $1 in
    --volume) bv_need_value "$@"; VOLUME=$2; shift 2 ;;
    --path) bv_need_value "$@"; INNER_PATH=$2; shift 2 ;;
    --exclude) bv_need_value "$@"; EXCLUDES+=("$2"); shift 2 ;;
    --helper-image) bv_need_value "$@"; HELPER_IMAGE=$2; shift 2 ;;
    --docker-binary) bv_need_value "$@"; DOCKER_BINARY=$2; shift 2 ;;
    --stop-container) bv_need_value "$@"; STOP_CONTAINERS+=("$2"); shift 2 ;;
    --run-arg) bv_need_value "$@"; RUN_ARGS+=("$2"); shift 2 ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

start_stopped_containers() {
  local name
  for name in ${STOPPED[@]+"${STOPPED[@]}"}; do
    bv_info "starting $name again"
    "$DOCKER_BINARY" start "$name" >/dev/null || bv_error "could not start $name again"
  done
  STOPPED=()
}

cleanup_all() {
  local status=$?
  start_stopped_containers
  bv_cleanup
  return $status
}

trap 'cleanup_all' EXIT
trap 'cleanup_all; exit 130' INT
trap 'cleanup_all; exit 143' TERM

bv_require "$DOCKER_BINARY"
[[ -n $VOLUME ]] || { usage >&2; bv_die "$BV_EXIT_USAGE" "--volume is required"; }

if ! "$DOCKER_BINARY" volume inspect "$VOLUME" >/dev/null 2>&1; then
  bv_die "$BV_EXIT_CONFIG" "no such volume: $VOLUME"
fi

[[ -n $BV_PREFIX ]] || BV_PREFIX="volume-$VOLUME"

bv_prepare_pack
bv_validate_target

TAR_COMMAND="tar --create --file - --directory /data"
for pattern in ${EXCLUDES[@]+"${EXCLUDES[@]}"}; do
  printf -v TAR_COMMAND '%s --exclude %q' "$TAR_COMMAND" "$pattern"
done
printf -v TAR_COMMAND '%s -- %q' "$TAR_COMMAND" "$INNER_PATH"

RUN_COMMAND=(run --rm --interactive=false --volume "$VOLUME:/data:ro")
RUN_COMMAND+=(${RUN_ARGS[@]+"${RUN_ARGS[@]}"})
RUN_COMMAND+=("$HELPER_IMAGE" sh -c "$TAR_COMMAND")

NAME=$(bv_artifact_name "$BV_PREFIX" tar)

if ((BV_DRY_RUN)); then
  bv_plan "$NAME" "tar of volume $VOLUME at $INNER_PATH"
  bv_info "plan: $DOCKER_BINARY ${RUN_COMMAND[*]}"
  if ((${#STOP_CONTAINERS[@]} > 0)); then
    bv_info "plan: stop and restart ${STOP_CONTAINERS[*]}"
  fi
  exit 0
fi

for name in ${STOP_CONTAINERS[@]+"${STOP_CONTAINERS[@]}"}; do
  bv_info "stopping $name"
  "$DOCKER_BINARY" stop "$name" >/dev/null || bv_die "$BV_EXIT_CONFIG" "could not stop $name"
  STOPPED+=("$name")
done

WORKDIR=$(bv_mktemp_dir)
SPOOL="$WORKDIR/spool"

bv_info "archiving volume $VOLUME with $HELPER_IMAGE"
bv_dump_to_file "$SPOOL" "$DOCKER_BINARY" "${RUN_COMMAND[@]}" || bv_die "$BV_EXIT_DUMP" "the helper container failed"

start_stopped_containers

bv_finish "$SPOOL" "$NAME" "$WORKDIR"
