#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=backvault-push.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
backvault-push.sh - upload an artifact to a Backvault push job through the ingest API.

Usage:
  backvault-push.sh --job <slug> --file <path> [options]
  <producer> | backvault-push.sh --job <slug> --name <filename> [options]

Options:
  --job SLUG              target job slug (env BACKVAULT_JOB)
  --file PATH             file to upload, omit to read stdin
  --name FILENAME         value of X-Backvault-Filename, default basename of --file
  --url URL               Backvault base URL, no trailing slash (env BACKVAULT_URL)
  --token TOKEN           API token with the ingest scope (env BACKVAULT_TOKEN)
  --token-file FILE       read the API token from a file (env BACKVAULT_TOKEN_FILE)
  --packed                the file is already compressed and encrypted, sets ?packed=1
  --sha256 HEX            use this checksum instead of computing one
  --no-sha256             do not send X-Backvault-Sha256
  --retries N             attempts after the first one, default 5 (env BACKVAULT_RETRIES)
  --retry-delay SECONDS   initial backoff, doubled per attempt, capped at 600 (default 5)
  --timeout SECONDS       curl max time per attempt, default 3600
  --connect-timeout SEC   curl connect timeout, default 15
  --insecure              skip TLS certificate verification
  --cacert FILE           CA bundle for TLS verification
  --dry-run               print what would be sent and exit
  --debug                 verbose logging on stderr
  -h, --help              this help

Output:
  The run id from the ingest response is printed on stdout, logs go to stderr.
  A 201 carries {"run":{...},"artifacts":[...]}. A 422 means the run was created and then
  failed; its id is at error.fields.runId and is printed on stdout as well.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 7 upload failed after retries,
  8 rejected by the server with a 4xx status.
USAGE
}

JOB=${BACKVAULT_JOB:-}
FILE=""
NAME=""
URL=${BACKVAULT_URL:-}
TOKEN=${BACKVAULT_TOKEN:-}
TOKEN_FILE=${BACKVAULT_TOKEN_FILE:-}
PACKED=0
SHA=""
SEND_SHA=1
RETRIES=${BACKVAULT_RETRIES:-5}
RETRY_DELAY=${BACKVAULT_RETRY_DELAY:-5}
MAX_TIME=${BACKVAULT_TIMEOUT:-3600}
CONNECT_TIMEOUT=${BACKVAULT_CONNECT_TIMEOUT:-15}
INSECURE=${BACKVAULT_INSECURE:-0}
CACERT=${BACKVAULT_CACERT:-}
DRY_RUN=0

while (($#)); do
  case $1 in
    --job) bv_need_value "$@"; JOB=$2; shift 2 ;;
    --file) bv_need_value "$@"; FILE=$2; shift 2 ;;
    --name) bv_need_value "$@"; NAME=$2; shift 2 ;;
    --url) bv_need_value "$@"; URL=$2; shift 2 ;;
    --token) bv_need_value "$@"; TOKEN=$2; shift 2 ;;
    --token-file) bv_need_value "$@"; TOKEN_FILE=$2; shift 2 ;;
    --packed) PACKED=1; shift ;;
    --sha256) bv_need_value "$@"; SHA=$2; shift 2 ;;
    --no-sha256) SEND_SHA=0; shift ;;
    --retries) bv_need_value "$@"; RETRIES=$2; shift 2 ;;
    --retry-delay) bv_need_value "$@"; RETRY_DELAY=$2; shift 2 ;;
    --timeout) bv_need_value "$@"; MAX_TIME=$2; shift 2 ;;
    --connect-timeout) bv_need_value "$@"; CONNECT_TIMEOUT=$2; shift 2 ;;
    --insecure) INSECURE=1; shift ;;
    --cacert) bv_need_value "$@"; CACERT=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --debug) BACKVAULT_DEBUG=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps
bv_require curl

[[ -n $JOB ]] || bv_die "$BV_EXIT_CONFIG" "no job slug: pass --job or set BACKVAULT_JOB"
[[ -n $URL ]] || bv_die "$BV_EXIT_CONFIG" "no server URL: pass --url or set BACKVAULT_URL"
URL=${URL%/}

if [[ -z $TOKEN && -n $TOKEN_FILE ]]; then
  TOKEN=$(bv_read_secret_file "$TOKEN_FILE")
fi
[[ -n $TOKEN ]] || bv_die "$BV_EXIT_CONFIG" "no API token: pass --token, --token-file or set BACKVAULT_TOKEN"

[[ $RETRIES =~ ^[0-9]+$ ]] || bv_die "$BV_EXIT_USAGE" "--retries must be a whole number"
[[ $RETRY_DELAY =~ ^[0-9]+$ ]] || bv_die "$BV_EXIT_USAGE" "--retry-delay must be a whole number"

SPOOLED=0
if [[ -z $FILE ]]; then
  if [[ -t 0 ]]; then
    usage >&2
    bv_die "$BV_EXIT_USAGE" "no --file and stdin is a terminal"
  fi
  FILE=$(bv_mktemp_file)
  SPOOLED=1
  bv_info "spooling stdin to $FILE"
  cat >"$FILE"
fi

[[ -f $FILE ]] || bv_die "$BV_EXIT_CONFIG" "not a file: $FILE"
[[ -r $FILE ]] || bv_die "$BV_EXIT_CONFIG" "cannot read: $FILE"

if [[ -z $NAME ]]; then
  if ((SPOOLED)); then
    NAME="$JOB-$(bv_stamp).bin"
    bv_warn "no --name given, using $NAME; set --name so Backvault can derive the extension"
  else
    NAME=$(basename -- "$FILE")
  fi
fi

SIZE=$(bv_filesize "$FILE")
if ((SEND_SHA)) && [[ -z $SHA ]]; then
  SHA=$(bv_sha256 "$FILE")
fi

ENDPOINT="$URL/api/v1/ingest/$(bv_uri_encode "$JOB")"
if ((PACKED)); then
  ENDPOINT="$ENDPOINT?packed=1"
fi

CURL_ARGS=(
  --silent --show-error
  --request POST
  --upload-file "$FILE"
  --header "X-Backvault-Filename: $NAME"
  --header "Expect:"
  --connect-timeout "$CONNECT_TIMEOUT"
  --max-time "$MAX_TIME"
  --write-out '%{http_code}'
)
if ((SEND_SHA)) && [[ -n $SHA ]]; then
  CURL_ARGS+=(--header "X-Backvault-Sha256: $SHA")
fi
if [[ $INSECURE == 1 ]]; then
  CURL_ARGS+=(--insecure)
fi
if [[ -n $CACERT ]]; then
  CURL_ARGS+=(--cacert "$CACERT")
fi

bv_info "pushing $NAME ($(bv_human_bytes "$SIZE")) to job $JOB at $URL"
bv_debug "endpoint $ENDPOINT, sha256 ${SHA:-none}, packed $PACKED"

if ((DRY_RUN)); then
  bv_info "dry run: POST $ENDPOINT"
  bv_info "dry run: X-Backvault-Filename: $NAME"
  bv_info "dry run: X-Backvault-Sha256: ${SHA:-none}"
  bv_info "dry run: Content-Length: $SIZE"
  bv_info "dry run: Authorization: Bearer <token hidden>"
  exit 0
fi

BODY_FILE=$(bv_mktemp_file)
TOKEN_HEADER_FILE=$(bv_mktemp_file)
chmod 600 -- "$TOKEN_HEADER_FILE"
printf 'Authorization: Bearer %s\n' "$TOKEN" >"$TOKEN_HEADER_FILE"
CURL_ARGS+=(--header "@$TOKEN_HEADER_FILE")

extract_run_id() {
  local body_file=$1 value=""
  if bv_have jq; then
    value=$(jq -r 'if type == "object" then (.run.id // .id // empty) else empty end' <"$body_file" 2>/dev/null || true)
  fi
  if [[ -z $value ]]; then
    value=$(sed -n 's/.*"run"[[:space:]]*:[[:space:]]*{[^}]*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <"$body_file" | head -n 1)
  fi
  printf '%s' "$value"
}

failed_run_id() {
  local body_file=$1 value=""
  if bv_have jq; then
    value=$(jq -r 'if type == "object" then (.error.fields.runId // empty) else empty end' <"$body_file" 2>/dev/null || true)
  fi
  if [[ -z $value ]]; then
    value=$(sed -n 's/.*"runId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <"$body_file" | head -n 1)
  fi
  printf '%s' "$value"
}

error_message() {
  local body_file=$1 value=""
  if bv_have jq; then
    value=$(jq -r 'if type == "object" then (.error.message // empty) else empty end' <"$body_file" 2>/dev/null || true)
  fi
  if [[ -z $value ]]; then
    value=$(sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <"$body_file" | head -n 1)
  fi
  printf '%s' "$value"
}

attempt=0
delay=$RETRY_DELAY
failed_run=""
while :; do
  attempt=$((attempt + 1))
  : >"$BODY_FILE"
  set +e
  http_code=$(curl "${CURL_ARGS[@]}" --output "$BODY_FILE" "$ENDPOINT")
  curl_status=$?
  set -e

  if ((curl_status == 0)) && [[ $http_code =~ ^2 ]]; then
    run_id=$(extract_run_id "$BODY_FILE" || true)
    if [[ -n $run_id ]]; then
      bv_info "accepted, run $run_id"
      printf '%s\n' "$run_id"
    else
      bv_warn "accepted with HTTP $http_code but no run id in the response"
    fi
    exit 0
  fi

  message=$(error_message "$BODY_FILE" || true)
  if ((curl_status != 0)); then
    bv_warn "attempt $attempt failed: curl exit $curl_status"
  else
    bv_warn "attempt $attempt failed: HTTP $http_code${message:+ - $message}"
  fi

  if ((curl_status == 0)) && [[ $http_code =~ ^4 ]] && [[ $http_code != 408 && $http_code != 423 && $http_code != 429 ]]; then
    failed_run=$(failed_run_id "$BODY_FILE" || true)
    if [[ -n $failed_run ]]; then
      bv_error "the run was created and then failed, run $failed_run"
      printf '%s\n' "$failed_run"
    fi
    bv_die "$BV_EXIT_REMOTE" "server rejected the upload with HTTP $http_code${message:+: $message}"
  fi

  if ((attempt > RETRIES)); then
    bv_die "$BV_EXIT_DELIVER" "giving up after $attempt attempts"
  fi

  bv_info "retrying in ${delay}s"
  sleep "$delay"
  delay=$((delay * 2))
  if ((delay > 600)); then delay=600; fi
done
