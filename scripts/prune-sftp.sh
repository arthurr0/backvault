#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=prune-sftp.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
prune-sftp.sh - keep the newest N files in a remote directory and delete the rest.

Files are ordered by the YYYYMMDD-HHMMSS timestamp in their name, not by modification
time. Files whose name carries no such timestamp, and .partial files, are never deleted.

Usage:
  prune-sftp.sh --keep <n> [--base-path <dir>] [options]

Options:
  --keep N                how many files to keep, required, must be at least 1
  --base-path PATH        remote directory to prune (env SFTP_BASE_PATH)
  --pattern GLOB          extra filter on the filename, default *
  --host HOST             SFTP host (env SFTP_HOST)
  --port PORT             SFTP port, default 22, use 23 for a Hetzner Storage Box (env SFTP_PORT)
  --user USER             SFTP user (env SFTP_USER)
  --key FILE              private key file (env SFTP_KEY)
  --password-file FILE    password file, needs sshpass (env SFTP_PASSWORD_FILE)
  --known-hosts FILE      known_hosts file to verify against (env SFTP_KNOWN_HOSTS)
  --host-key-checking MODE  yes, accept-new or no, default yes (env SFTP_HOST_KEY_CHECKING)
  --ssh-option OPT        extra -o option for sftp, repeatable
  --dry-run               list what would be deleted and exit
  --debug                 verbose logging on stderr
  -h, --help              this help

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 7 a delete failed.
USAGE
}

KEEP=""
BASE_PATH=${SFTP_BASE_PATH:-}
PATTERN='*'
HOST=${SFTP_HOST:-}
PORT=${SFTP_PORT:-22}
USER_NAME=${SFTP_USER:-}
KEY=${SFTP_KEY:-}
PASSWORD_FILE=${SFTP_PASSWORD_FILE:-}
KNOWN_HOSTS=${SFTP_KNOWN_HOSTS:-}
HOST_KEY_CHECKING=${SFTP_HOST_KEY_CHECKING:-yes}
DRY_RUN=0
EXTRA_OPTIONS=()

while (($#)); do
  case $1 in
    --keep) bv_need_value "$@"; KEEP=$2; shift 2 ;;
    --base-path) bv_need_value "$@"; BASE_PATH=$2; shift 2 ;;
    --pattern) bv_need_value "$@"; PATTERN=$2; shift 2 ;;
    --host) bv_need_value "$@"; HOST=$2; shift 2 ;;
    --port) bv_need_value "$@"; PORT=$2; shift 2 ;;
    --user) bv_need_value "$@"; USER_NAME=$2; shift 2 ;;
    --key) bv_need_value "$@"; KEY=$2; shift 2 ;;
    --password-file) bv_need_value "$@"; PASSWORD_FILE=$2; shift 2 ;;
    --known-hosts) bv_need_value "$@"; KNOWN_HOSTS=$2; shift 2 ;;
    --host-key-checking) bv_need_value "$@"; HOST_KEY_CHECKING=$2; shift 2 ;;
    --ssh-option) bv_need_value "$@"; EXTRA_OPTIONS+=(-o "$2"); shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --debug) BACKVAULT_DEBUG=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps
bv_require sftp

[[ -n $KEEP ]] || { usage >&2; bv_die "$BV_EXIT_USAGE" "--keep is required"; }
[[ $KEEP =~ ^[0-9]+$ ]] || bv_die "$BV_EXIT_USAGE" "--keep must be a whole number"
((KEEP >= 1)) || bv_die "$BV_EXIT_USAGE" "--keep must be at least 1"
[[ -n $HOST ]] || bv_die "$BV_EXIT_CONFIG" "no host: pass --host or set SFTP_HOST"
[[ -n $USER_NAME ]] || bv_die "$BV_EXIT_CONFIG" "no user: pass --user or set SFTP_USER"

case $HOST_KEY_CHECKING in
  yes | accept-new | no) ;;
  *) bv_die "$BV_EXIT_USAGE" "unknown --host-key-checking: $HOST_KEY_CHECKING (want yes, accept-new or no)" ;;
esac

SSH_OPTIONS=(-o "BatchMode=yes" -o "StrictHostKeyChecking=$HOST_KEY_CHECKING")
if [[ -n $KNOWN_HOSTS ]]; then
  SSH_OPTIONS+=(-o "UserKnownHostsFile=$KNOWN_HOSTS")
elif [[ $HOST_KEY_CHECKING == no ]]; then
  SSH_OPTIONS+=(-o "UserKnownHostsFile=/dev/null" -o "GlobalKnownHostsFile=/dev/null")
  bv_warn "host key checking is off, the connection is not protected against a man in the middle"
fi
if [[ -n $KEY ]]; then
  [[ -r $KEY ]] || bv_die "$BV_EXIT_CONFIG" "cannot read private key: $KEY"
  bv_check_permissions "$KEY"
  SSH_OPTIONS+=(-o "IdentitiesOnly=yes" -i "$KEY")
fi
SSH_OPTIONS+=(${EXTRA_OPTIONS[@]+"${EXTRA_OPTIONS[@]}"})

WRAPPER=()
if [[ -n $PASSWORD_FILE ]]; then
  [[ -r $PASSWORD_FILE ]] || bv_die "$BV_EXIT_CONFIG" "cannot read password file: $PASSWORD_FILE"
  bv_check_permissions "$PASSWORD_FILE"
  bv_have sshpass || bv_die "$BV_EXIT_DEPENDENCY" "password authentication needs sshpass, install it or use --key"
  WRAPPER=(sshpass -f "$PASSWORD_FILE")
  SSH_OPTIONS=("${SSH_OPTIONS[@]/BatchMode=yes/BatchMode=no}")
  SSH_OPTIONS+=(-o "PubkeyAuthentication=no" -o "PreferredAuthentications=password,keyboard-interactive")
fi

REMOTE_DIR=${BASE_PATH%/}
remote_quote() { printf "'%s'" "${1//\'/\'\\\'\'}"; }

run_batch() {
  local batch=$1
  ${WRAPPER[@]+"${WRAPPER[@]}"} sftp "${SSH_OPTIONS[@]}" -P "$PORT" -b "$batch" "$USER_NAME@$HOST"
}

LIST_BATCH=$(bv_mktemp_file)
printf 'ls -1 %s\n' "$(remote_quote "${REMOTE_DIR:-.}")" >"$LIST_BATCH"

bv_info "listing $USER_NAME@$HOST:${REMOTE_DIR:-.}"
LISTING=$(bv_mktemp_file)
if ! run_batch "$LIST_BATCH" >"$LISTING" 2>>/dev/stderr; then
  bv_die "$BV_EXIT_DELIVER" "cannot list ${REMOTE_DIR:-.} on $HOST"
fi

CANDIDATES=$(bv_mktemp_file)
TOTAL=0
SKIPPED=0
while IFS= read -r line; do
  line=${line%$'\r'}
  [[ -n $line ]] || continue
  case $line in
    sftp\>* | "Connected to"* | "Changing"*) continue ;;
  esac
  name=${line##*/}
  [[ -n $name ]] || continue
  TOTAL=$((TOTAL + 1))
  case $name in
    *.partial) continue ;;
  esac
  case $name in
    $PATTERN) ;;
    *) continue ;;
  esac
  stamp=$(printf '%s' "$name" | grep -o '[0-9]\{8\}-[0-9]\{6\}' | head -n 1 || true)
  if [[ -z $stamp ]]; then
    SKIPPED=$((SKIPPED + 1))
    bv_debug "no timestamp in $name, keeping"
    continue
  fi
  printf '%s\t%s\n' "$stamp" "$name" >>"$CANDIDATES"
done <"$LISTING"

MATCHED=$(wc -l <"$CANDIDATES" | tr -d ' ')
bv_info "$TOTAL entries listed, $MATCHED match the pattern, $SKIPPED without a timestamp are kept"

if ((MATCHED <= KEEP)); then
  bv_info "nothing to delete, keeping the newest $KEEP"
  exit 0
fi

DELETE_LIST=$(bv_mktemp_file)
sort -r <"$CANDIDATES" | tail -n +$((KEEP + 1)) | cut -f2- >"$DELETE_LIST"

DELETE_BATCH=$(bv_mktemp_file)
count=0
while IFS= read -r name; do
  [[ -n $name ]] || continue
  count=$((count + 1))
  if ((DRY_RUN)); then
    bv_info "dry run: would delete ${REMOTE_DIR:+$REMOTE_DIR/}$name"
  else
    printf 'rm %s\n' "$(remote_quote "${REMOTE_DIR:+$REMOTE_DIR/}$name")" >>"$DELETE_BATCH"
  fi
done <"$DELETE_LIST"

if ((DRY_RUN)); then
  bv_info "dry run: $count files would be deleted, $KEEP kept"
  exit 0
fi

if ! run_batch "$DELETE_BATCH" >&2; then
  bv_die "$BV_EXIT_DELIVER" "at least one delete failed"
fi
bv_info "deleted $count files, kept $KEEP"
