#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=upload-s3.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
upload-s3.sh - upload one file to an S3-compatible bucket.

Usage:
  upload-s3.sh --file <path> [--name <object-name>] [options]

Options:
  --file PATH             file to upload, required
  --name NAME             object name inside the prefix, default basename of --file
  --key KEY               full object key, overrides --name and the prefix
  --bucket NAME           bucket (env S3_BUCKET)
  --prefix PATH           key prefix (env S3_PREFIX)
  --endpoint URL          endpoint, empty means AWS (env S3_ENDPOINT)
  --region NAME           region, default us-east-1 (env S3_REGION)
  --path-style            force path-style addressing (env S3_PATH_STYLE=1)
  --virtual-host          force virtual-host addressing (env S3_PATH_STYLE=0)
  --storage-class CLASS   x-amz-storage-class value (env S3_STORAGE_CLASS)
  --sse ALGO              x-amz-server-side-encryption value, for example AES256 (env S3_SSE)
  --method auto|aws|mc|curl   uploader, default auto (env S3_METHOD)
  --insecure              skip TLS certificate verification
  --retries N             attempts after the first one, default 3 (env S3_RETRIES)
  --retry-delay SECONDS   initial backoff, doubled per attempt, capped at 600 (default 5)
  --timeout SECONDS       curl max time per attempt, default 3600
  --dry-run               print the target and exit
  --debug                 verbose logging on stderr
  -h, --help              this help

Credentials come from S3_ACCESS_KEY and S3_SECRET_KEY, or from AWS_ACCESS_KEY_ID and
AWS_SECRET_ACCESS_KEY. They are never placed on a command line.

Method auto prefers the aws CLI, then mc, then the built-in curl uploader. The curl
uploader signs the request with AWS Signature Version 4 and performs a single PUT,
which limits it to 5 GiB per object.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 7 upload failed, 8 rejected by the server.
USAGE
}

FILE=""
NAME=""
KEY=""
BUCKET=${S3_BUCKET:-}
PREFIX=${S3_PREFIX:-}
ENDPOINT=${S3_ENDPOINT:-}
REGION=${S3_REGION:-us-east-1}
PATH_STYLE=${S3_PATH_STYLE:-auto}
METHOD=${S3_METHOD:-auto}
RETRIES=${S3_RETRIES:-3}
RETRY_DELAY=${S3_RETRY_DELAY:-5}
DRY_RUN=0

BV_S3_STORAGE_CLASS=${S3_STORAGE_CLASS:-}
BV_S3_SSE=${S3_SSE:-}
BV_S3_INSECURE=${S3_INSECURE:-0}
BV_S3_MAX_TIME=${S3_TIMEOUT:-3600}
BV_S3_ACCESS_KEY=${S3_ACCESS_KEY:-${AWS_ACCESS_KEY_ID:-}}
BV_S3_SECRET_KEY=${S3_SECRET_KEY:-${AWS_SECRET_ACCESS_KEY:-}}

while (($#)); do
  case $1 in
    --file) bv_need_value "$@"; FILE=$2; shift 2 ;;
    --name) bv_need_value "$@"; NAME=$2; shift 2 ;;
    --key) bv_need_value "$@"; KEY=$2; shift 2 ;;
    --bucket) bv_need_value "$@"; BUCKET=$2; shift 2 ;;
    --prefix) bv_need_value "$@"; PREFIX=$2; shift 2 ;;
    --endpoint) bv_need_value "$@"; ENDPOINT=$2; shift 2 ;;
    --region) bv_need_value "$@"; REGION=$2; shift 2 ;;
    --path-style) PATH_STYLE=1; shift ;;
    --virtual-host) PATH_STYLE=0; shift ;;
    --storage-class) bv_need_value "$@"; BV_S3_STORAGE_CLASS=$2; shift 2 ;;
    --sse) bv_need_value "$@"; BV_S3_SSE=$2; shift 2 ;;
    --method) bv_need_value "$@"; METHOD=$2; shift 2 ;;
    --insecure) BV_S3_INSECURE=1; shift ;;
    --retries) bv_need_value "$@"; RETRIES=$2; shift 2 ;;
    --retry-delay) bv_need_value "$@"; RETRY_DELAY=$2; shift 2 ;;
    --timeout) bv_need_value "$@"; BV_S3_MAX_TIME=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --debug) BACKVAULT_DEBUG=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

[[ -n $FILE ]] || { usage >&2; bv_die "$BV_EXIT_USAGE" "--file is required"; }
[[ -f $FILE ]] || bv_die "$BV_EXIT_CONFIG" "not a file: $FILE"
[[ -n $BUCKET ]] || bv_die "$BV_EXIT_CONFIG" "no bucket: pass --bucket or set S3_BUCKET"
[[ -n $NAME ]] || NAME=$(basename -- "$FILE")
[[ -n $KEY ]] || KEY=$(bv_join_path "$PREFIX" "$NAME")

ENDPOINT=${ENDPOINT%/}
if [[ $PATH_STYLE == auto ]]; then
  if [[ -z $ENDPOINT || $ENDPOINT == *amazonaws.com* ]]; then PATH_STYLE=0; else PATH_STYLE=1; fi
fi

BV_S3_ENDPOINT=$ENDPOINT
BV_S3_REGION=$REGION
BV_S3_BUCKET=$BUCKET
BV_S3_PATH_STYLE=$PATH_STYLE

SIZE=$(bv_filesize "$FILE")

case $METHOD in
  auto)
    if bv_have aws; then METHOD=aws
    elif bv_have mc; then METHOD=mc
    else METHOD=curl; fi
    ;;
  aws) bv_require aws ;;
  mc) bv_require mc ;;
  curl) bv_require curl; bv_require openssl ;;
  *) bv_die "$BV_EXIT_USAGE" "unknown method: $METHOD (want auto, aws, mc or curl)" ;;
esac

bv_info "uploading $NAME ($(bv_human_bytes "$SIZE")) to s3://$BUCKET/$KEY using $METHOD"

if ((DRY_RUN)); then
  bv_info "dry run: endpoint ${ENDPOINT:-https://s3.$REGION.amazonaws.com}, region $REGION, path-style $PATH_STYLE"
  bv_info "dry run: object key $KEY"
  exit 0
fi

upload_aws() {
  local args=(s3 cp "$FILE" "s3://$BUCKET/$KEY" --no-progress)
  [[ -n $ENDPOINT ]] && args+=(--endpoint-url "$ENDPOINT")
  [[ -n $REGION ]] && args+=(--region "$REGION")
  [[ -n $BV_S3_STORAGE_CLASS ]] && args+=(--storage-class "$BV_S3_STORAGE_CLASS")
  [[ -n $BV_S3_SSE ]] && args+=(--sse "$BV_S3_SSE")
  AWS_ACCESS_KEY_ID=$BV_S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$BV_S3_SECRET_KEY aws "${args[@]}"
}

upload_mc() {
  local alias=backvault
  local host=${ENDPOINT:-https://s3.$REGION.amazonaws.com}
  local scheme=${host%%://*} rest=${host#*://}
  local hostvar="MC_HOST_$alias"
  local value="$scheme://$(bv_uri_encode "$BV_S3_ACCESS_KEY"):$(bv_uri_encode "$BV_S3_SECRET_KEY")@$rest"
  env "$hostvar=$value" mc --quiet cp "$FILE" "$alias/$BUCKET/$KEY" >/dev/null
}

upload_curl() {
  bv_s3_check_credentials
  if ((SIZE > 5368709120)); then
    bv_die "$BV_EXIT_CONFIG" "the curl uploader does a single PUT and cannot upload more than 5 GiB, install the aws CLI or mc"
  fi
  local payload_hash body http_code message
  payload_hash=$(bv_sha256 "$FILE")
  body=$(bv_mktemp_file)
  http_code=$(bv_s3_request PUT "$KEY" "" "$payload_hash" "$FILE" "$body")
  if [[ $http_code =~ ^2 ]]; then
    rm -f -- "$body"
    return 0
  fi
  message=$(bv_s3_error_message "$body")
  bv_warn "S3 responded with HTTP ${http_code:-none}${message:+ - $message}"
  rm -f -- "$body"
  if [[ $http_code =~ ^4 ]] && [[ $http_code != 408 && $http_code != 429 ]]; then
    return 8
  fi
  return 1
}

attempt=0
delay=$RETRY_DELAY
while :; do
  attempt=$((attempt + 1))
  set +e
  case $METHOD in
    aws) upload_aws ;;
    mc) upload_mc ;;
    curl) upload_curl ;;
  esac
  status=$?
  set -e
  if ((status == 0)); then
    bv_info "uploaded s3://$BUCKET/$KEY"
    exit 0
  fi
  if ((status == 8)); then
    bv_die "$BV_EXIT_REMOTE" "S3 rejected the upload"
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
