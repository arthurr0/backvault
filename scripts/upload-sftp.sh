#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=upload-sftp.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
upload-sftp.sh - upload one file to an SFTP host, atomically.

The file is written as <name>.partial and renamed once the transfer finished, so a
half-transferred backup is never mistaken for a complete one.

Usage:
  upload-sftp.sh --file <path> [--name <remote-name>] [options]

Options:
  --file PATH             file to upload, required
  --name NAME             remote filename, default basename of --file
  --host HOST             SFTP host (env SFTP_HOST)
  --port PORT             SFTP port, default 22, use 23 for a Hetzner Storage Box (env SFTP_PORT)
  --user USER             SFTP user (env SFTP_USER)
  --key FILE              private key file (env SFTP_KEY)
  --password-file FILE    password file, needs sshpass (env SFTP_PASSWORD_FILE)
  --base-path PATH        remote directory, created when missing (env SFTP_BASE_PATH)
  --mode sftp|ssh         sftp batch mode or streaming through ssh cat, default sftp (env SFTP_MODE)
  --known-hosts FILE      known_hosts file to verify against (env SFTP_KNOWN_HOSTS)
  --host-key-checking MODE  yes, accept-new or no, default yes (env SFTP_HOST_KEY_CHECKING)
  --ssh-option OPT        extra -o option for ssh and sftp, repeatable
  --retries N             attempts after the first one, default 3 (env SFTP_RETRIES)
  --retry-delay SECONDS   initial backoff, doubled per attempt, capped at 600 (default 5)
  --dry-run               print the target and exit
  --debug                 verbose logging on stderr
  -h, --help              this help

Passwords are read from a file and handed to sshpass through a file descriptor, never
placed on a command line. Key authentication is preferred and is the only option when
sshpass is not installed.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 7 upload failed.
USAGE
}

FILE=""
NAME=""
HOST=${SFTP_HOST:-}
PORT=${SFTP_PORT:-22}
USER_NAME=${SFTP_USER:-}
KEY=${SFTP_KEY:-}
PASSWORD_FILE=${SFTP_PASSWORD_FILE:-}
BASE_PATH=${SFTP_BASE_PATH:-}
MODE=${SFTP_MODE:-sftp}
KNOWN_HOSTS=${SFTP_KNOWN_HOSTS:-}
HOST_KEY_CHECKING=${SFTP_HOST_KEY_CHECKING:-yes}
RETRIES=${SFTP_RETRIES:-3}
RETRY_DELAY=${SFTP_RETRY_DELAY:-5}
DRY_RUN=0
EXTRA_OPTIONS=()

while (($#)); do
  case $1 in
    --file) bv_need_value "$@"; FILE=$2; shift 2 ;;
    --name) bv_need_value "$@"; NAME=$2; shift 2 ;;
    --host) bv_need_value "$@"; HOST=$2; shift 2 ;;
    --port) bv_need_value "$@"; PORT=$2; shift 2 ;;
    --user) bv_need_value "$@"; USER_NAME=$2; shift 2 ;;
    --key) bv_need_value "$@"; KEY=$2; shift 2 ;;
    --password-file) bv_need_value "$@"; PASSWORD_FILE=$2; shift 2 ;;
    --base-path) bv_need_value "$@"; BASE_PATH=$2; shift 2 ;;
    --mode) bv_need_value "$@"; MODE=$2; shift 2 ;;
    --known-hosts) bv_need_value "$@"; KNOWN_HOSTS=$2; shift 2 ;;
    --host-key-checking) bv_need_value "$@"; HOST_KEY_CHECKING=$2; shift 2 ;;
    --ssh-option) bv_need_value "$@"; EXTRA_OPTIONS+=(-o "$2"); shift 2 ;;
    --retries) bv_need_value "$@"; RETRIES=$2; shift 2 ;;
    --retry-delay) bv_need_value "$@"; RETRY_DELAY=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --debug) BACKVAULT_DEBUG=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

[[ -n $FILE ]] || { usage >&2; bv_die "$BV_EXIT_USAGE" "--file is required"; }
[[ -f $FILE ]] || bv_die "$BV_EXIT_CONFIG" "not a file: $FILE"
[[ -n $HOST ]] || bv_die "$BV_EXIT_CONFIG" "no host: pass --host or set SFTP_HOST"
[[ -n $USER_NAME ]] || bv_die "$BV_EXIT_CONFIG" "no user: pass --user or set SFTP_USER"
[[ -n $NAME ]] || NAME=$(basename -- "$FILE")
case $MODE in
  sftp) bv_require sftp ;;
  ssh) bv_require ssh ;;
  *) bv_die "$BV_EXIT_USAGE" "unknown mode: $MODE (want sftp or ssh)" ;;
esac

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
if [[ -z $REMOTE_DIR ]]; then
  REMOTE_PATH=$NAME
else
  REMOTE_PATH="$REMOTE_DIR/$NAME"
fi
PARTIAL_PATH="$REMOTE_PATH.partial"
SIZE=$(bv_filesize "$FILE")

bv_info "uploading $NAME ($(bv_human_bytes "$SIZE")) to $USER_NAME@$HOST:$PORT${REMOTE_DIR:+ under $REMOTE_DIR} using $MODE"

if ((DRY_RUN)); then
  bv_info "dry run: remote path ${REMOTE_PATH:-$NAME}"
  bv_info "dry run: staged as ${PARTIAL_PATH:-$NAME.partial}"
  exit 0
fi

remote_quote() { printf "'%s'" "${1//\'/\'\\\'\'}"; }

mkdir_commands() {
  local dir=$1 accumulated="" part
  [[ -n $dir ]] || return 0
  if [[ $dir == /* ]]; then accumulated="/"; fi
  local IFS=/
  for part in $dir; do
    [[ -n $part ]] || continue
    if [[ $accumulated == "/" ]]; then
      accumulated="/$part"
    elif [[ -z $accumulated ]]; then
      accumulated="$part"
    else
      accumulated="$accumulated/$part"
    fi
    printf -- '-mkdir %s\n' "$(remote_quote "$accumulated")"
  done
}

upload_sftp() {
  local batch
  batch=$(bv_mktemp_file)
  {
    mkdir_commands "$REMOTE_DIR"
    printf -- '-rm %s\n' "$(remote_quote "$PARTIAL_PATH")"
    printf 'put %s %s\n' "$(remote_quote "$FILE")" "$(remote_quote "$PARTIAL_PATH")"
    printf -- '-rm %s\n' "$(remote_quote "$REMOTE_PATH")"
    printf 'rename %s %s\n' "$(remote_quote "$PARTIAL_PATH")" "$(remote_quote "$REMOTE_PATH")"
  } >"$batch"
  bv_debug "sftp batch: $(tr '\n' ';' <"$batch")"
  ${WRAPPER[@]+"${WRAPPER[@]}"} sftp "${SSH_OPTIONS[@]}" -P "$PORT" -b "$batch" "$USER_NAME@$HOST" >&2
  local status=$?
  rm -f -- "$batch"
  return $status
}

upload_ssh() {
  local remote_script
  remote_script="mkdir -p -- $(remote_quote "${REMOTE_DIR:-.}") && cat > $(remote_quote "$PARTIAL_PATH") && mv -- $(remote_quote "$PARTIAL_PATH") $(remote_quote "$REMOTE_PATH")"
  ${WRAPPER[@]+"${WRAPPER[@]}"} ssh "${SSH_OPTIONS[@]}" -p "$PORT" "$USER_NAME@$HOST" "$remote_script" <"$FILE"
}

attempt=0
delay=$RETRY_DELAY
while :; do
  attempt=$((attempt + 1))
  set +e
  case $MODE in
    sftp) upload_sftp ;;
    ssh) upload_ssh ;;
  esac
  status=$?
  set -e
  if ((status == 0)); then
    bv_info "uploaded ${REMOTE_PATH:-$NAME}"
    exit 0
  fi
  bv_warn "attempt $attempt failed with status $status"
  if ((attempt > RETRIES)); then
    bv_die "$BV_EXIT_DELIVER" "giving up after $attempt attempts"
  fi
  bv_info "retrying in ${delay}s"
  sleep "$delay"
  delay=$((delay * 2))
  if ((delay > 600)); then delay=600; fi
done
