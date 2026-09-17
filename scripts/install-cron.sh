#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=install-cron.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
install-cron.sh - add a Backvault backup script to the crontab of the current user.

Run it without options to be asked for the script, the schedule and the environment file.
Every entry it writes carries a BACKVAULT_CRON_TAG marker, which is how --list and --remove
find their own entries again.

Usage:
  install-cron.sh [options]
  install-cron.sh --list
  install-cron.sh --remove <tag>

Options:
  --script NAME           backup script, a name like backup-postgres.sh or a full path
  --schedule CRON         five field cron expression, for example "0 2 * * *"
  --preset NAME           daily, hourly, weekly, monthly or fifteen-minutes
  --env-file FILE         file sourced before the script runs, for credentials
  --args "ARGS"           arguments for the backup script
  --tag NAME              marker for this entry, default derived from the script name
  --log-file FILE         append output here, default /dev/null goes to the cron mail
  --list                  show the entries this script manages and exit
  --remove TAG            remove the entry with this tag and exit
  --yes                   do not ask for confirmation
  --dry-run               print the crontab line without installing it
  -h, --help              this help

Exit codes:
  0 ok, 2 usage, 3 crontab not available, 4 configuration.
USAGE
}

SCRIPT=""
SCHEDULE=""
PRESET=""
ENV_FILE=""
ARGS=""
TAG=""
LOG_FILE=""
ACTION=install
REMOVE_TAG=""
ASSUME_YES=0
DRY_RUN=0

while (($#)); do
  case $1 in
    --script) bv_need_value "$@"; SCRIPT=$2; shift 2 ;;
    --schedule) bv_need_value "$@"; SCHEDULE=$2; shift 2 ;;
    --preset) bv_need_value "$@"; PRESET=$2; shift 2 ;;
    --env-file) bv_need_value "$@"; ENV_FILE=$2; shift 2 ;;
    --args) bv_need_value "$@"; ARGS=$2; shift 2 ;;
    --tag) bv_need_value "$@"; TAG=$2; shift 2 ;;
    --log-file) bv_need_value "$@"; LOG_FILE=$2; shift 2 ;;
    --list) ACTION=list; shift ;;
    --remove) bv_need_value "$@"; ACTION=remove; REMOVE_TAG=$2; shift 2 ;;
    --yes) ASSUME_YES=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps
bv_have crontab || bv_die "$BV_EXIT_DEPENDENCY" "no crontab command found, install cron or use a systemd timer"

MARKER=BACKVAULT_CRON_TAG

read_crontab() {
  crontab -l 2>/dev/null || true
}

if [[ $ACTION == list ]]; then
  if read_crontab | grep -- "$MARKER=" >/dev/null; then
    read_crontab | grep -- "$MARKER="
  else
    bv_info "no Backvault entries in the crontab of $(id -un)"
  fi
  exit 0
fi

if [[ $ACTION == remove ]]; then
  current=$(bv_mktemp_file)
  read_crontab >"$current"
  if ! grep -- "$MARKER=$REMOVE_TAG " "$current" >/dev/null; then
    bv_die "$BV_EXIT_CONFIG" "no entry tagged $REMOVE_TAG"
  fi
  updated=$(bv_mktemp_file)
  grep -v -- "$MARKER=$REMOVE_TAG " "$current" >"$updated" || true
  if ((DRY_RUN)); then
    bv_info "dry run: would remove the entry tagged $REMOVE_TAG"
    exit 0
  fi
  crontab -- "$updated"
  bv_info "removed the entry tagged $REMOVE_TAG"
  exit 0
fi

ask() {
  local prompt=$1 default=${2:-} answer
  if ((ASSUME_YES)); then
    printf '%s' "$default"
    return 0
  fi
  if [[ ! -t 0 ]]; then
    printf '%s' "$default"
    return 0
  fi
  read -r -p "$prompt${default:+ [$default]}: " answer </dev/tty
  printf '%s' "${answer:-$default}"
}

available_scripts() {
  local path
  for path in "$BV_SCRIPT_DIR"/backup-*.sh; do
    [[ -f $path ]] || continue
    basename -- "$path"
  done
}

if [[ -z $SCRIPT ]]; then
  if [[ -t 0 ]]; then
    printf 'Backup scripts in %s:\n' "$BV_SCRIPT_DIR" >&2
    available_scripts | sed 's/^/  /' >&2
  fi
  SCRIPT=$(ask "Which script" "backup-files.sh")
fi
[[ -n $SCRIPT ]] || bv_die "$BV_EXIT_USAGE" "no script chosen"

if [[ $SCRIPT != /* ]]; then
  SCRIPT="$BV_SCRIPT_DIR/$SCRIPT"
fi
[[ -x $SCRIPT ]] || bv_die "$BV_EXIT_CONFIG" "not an executable script: $SCRIPT"

case $PRESET in
  "") ;;
  daily) SCHEDULE=${SCHEDULE:-"0 2 * * *"} ;;
  hourly) SCHEDULE=${SCHEDULE:-"0 * * * *"} ;;
  weekly) SCHEDULE=${SCHEDULE:-"0 3 * * 0"} ;;
  monthly) SCHEDULE=${SCHEDULE:-"0 4 1 * *"} ;;
  fifteen-minutes) SCHEDULE=${SCHEDULE:-"*/15 * * * *"} ;;
  *) bv_die "$BV_EXIT_USAGE" "unknown preset: $PRESET" ;;
esac

if [[ -z $SCHEDULE ]]; then
  SCHEDULE=$(ask "Schedule, five cron fields" "0 2 * * *")
fi
field_count=$(printf '%s\n' "$SCHEDULE" | awk '{print NF}')
[[ $field_count == 5 ]] || bv_die "$BV_EXIT_USAGE" "the schedule needs five fields, got: $SCHEDULE"

if [[ -z $ENV_FILE ]]; then
  ENV_FILE=$(ask "Environment file with credentials, empty for none" "")
fi
if [[ -n $ENV_FILE ]]; then
  [[ -r $ENV_FILE ]] || bv_die "$BV_EXIT_CONFIG" "cannot read environment file: $ENV_FILE"
  bv_check_permissions "$ENV_FILE"
fi

if [[ -z $ARGS ]]; then
  ARGS=$(ask "Arguments for $(basename -- "$SCRIPT")" "")
fi

if [[ -z $TAG ]]; then
  default_tag=$(basename -- "$SCRIPT")
  default_tag=${default_tag#backup-}
  default_tag=${default_tag%.sh}
  TAG=$(ask "Tag for this entry" "$default_tag")
fi
[[ $TAG =~ ^[A-Za-z0-9_.-]+$ ]] || bv_die "$BV_EXIT_USAGE" "the tag may only contain letters, digits, dot, dash and underscore"

[[ -n $LOG_FILE ]] || LOG_FILE=/dev/null

quote() { printf '%q' "$1"; }

COMMAND=""
if [[ -n $ENV_FILE ]]; then
  COMMAND=". $(quote "$ENV_FILE") && "
fi
COMMAND+="$(quote "$SCRIPT")"
[[ -n $ARGS ]] && COMMAND+=" $ARGS"
COMMAND+=" >> $(quote "$LOG_FILE") 2>&1"

LINE="$SCHEDULE $MARKER=$TAG $COMMAND"
LINE=${LINE//%/\\%}

bv_info "crontab entry:"
printf '%s\n' "$LINE" >&2

if ((DRY_RUN)); then
  bv_info "dry run: nothing was installed"
  exit 0
fi

current=$(bv_mktemp_file)
read_crontab >"$current"
if grep -- "$MARKER=$TAG " "$current" >/dev/null; then
  bv_info "an entry tagged $TAG exists and will be replaced"
fi

if ((ASSUME_YES == 0)) && [[ -t 0 ]]; then
  answer=$(ask "Install this entry? yes or no" "yes")
  case $answer in
    y | yes) ;;
    *)
      bv_info "nothing was installed"
      exit 0
      ;;
  esac
fi

updated=$(bv_mktemp_file)
grep -v -- "$MARKER=$TAG " "$current" >"$updated" || true
printf '%s\n' "$LINE" >>"$updated"
crontab -- "$updated"
bv_info "installed, tagged $TAG"
bv_info "check it with: crontab -l"
