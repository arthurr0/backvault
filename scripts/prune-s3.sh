#!/usr/bin/env bash
set -Eeuo pipefail

BV_PROG=prune-s3.sh
BV_SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
. "$BV_SCRIPT_DIR/lib/common.sh"

usage() {
  cat <<'USAGE'
prune-s3.sh - keep the newest N objects under an S3 prefix and delete the rest.

Objects are ordered by the YYYYMMDD-HHMMSS timestamp in their name, not by upload time.
Objects whose name carries no such timestamp are never deleted.

Usage:
  prune-s3.sh --keep <n> [--prefix <path>] [options]

Options:
  --keep N                how many objects to keep, required, must be at least 1
  --prefix PATH           key prefix to prune (env S3_PREFIX)
  --pattern GLOB          extra filter on the object name, default *
  --bucket NAME           bucket (env S3_BUCKET)
  --endpoint URL          endpoint, empty means AWS (env S3_ENDPOINT)
  --region NAME           region, default us-east-1 (env S3_REGION)
  --path-style            force path-style addressing (env S3_PATH_STYLE=1)
  --virtual-host          force virtual-host addressing
  --method auto|aws|mc|curl   client to use, default auto (env S3_METHOD)
  --insecure              skip TLS certificate verification
  --dry-run               list what would be deleted and exit
  --debug                 verbose logging on stderr
  -h, --help              this help

Credentials come from S3_ACCESS_KEY and S3_SECRET_KEY, or from AWS_ACCESS_KEY_ID and
AWS_SECRET_ACCESS_KEY.

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 7 a delete failed, 8 listing rejected.
USAGE
}

KEEP=""
PREFIX=${S3_PREFIX:-}
PATTERN='*'
BUCKET=${S3_BUCKET:-}
ENDPOINT=${S3_ENDPOINT:-}
REGION=${S3_REGION:-us-east-1}
PATH_STYLE=${S3_PATH_STYLE:-auto}
METHOD=${S3_METHOD:-auto}
DRY_RUN=0

BV_S3_INSECURE=${S3_INSECURE:-0}
BV_S3_MAX_TIME=${S3_TIMEOUT:-300}
BV_S3_ACCESS_KEY=${S3_ACCESS_KEY:-${AWS_ACCESS_KEY_ID:-}}
BV_S3_SECRET_KEY=${S3_SECRET_KEY:-${AWS_SECRET_ACCESS_KEY:-}}

while (($#)); do
  case $1 in
    --keep) bv_need_value "$@"; KEEP=$2; shift 2 ;;
    --prefix) bv_need_value "$@"; PREFIX=$2; shift 2 ;;
    --pattern) bv_need_value "$@"; PATTERN=$2; shift 2 ;;
    --bucket) bv_need_value "$@"; BUCKET=$2; shift 2 ;;
    --endpoint) bv_need_value "$@"; ENDPOINT=$2; shift 2 ;;
    --region) bv_need_value "$@"; REGION=$2; shift 2 ;;
    --path-style) PATH_STYLE=1; shift ;;
    --virtual-host) PATH_STYLE=0; shift ;;
    --method) bv_need_value "$@"; METHOD=$2; shift 2 ;;
    --insecure) BV_S3_INSECURE=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --debug) BACKVAULT_DEBUG=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) usage >&2; bv_die "$BV_EXIT_USAGE" "unknown option: $1" ;;
  esac
done

bv_install_traps

[[ -n $KEEP ]] || { usage >&2; bv_die "$BV_EXIT_USAGE" "--keep is required"; }
[[ $KEEP =~ ^[0-9]+$ ]] || bv_die "$BV_EXIT_USAGE" "--keep must be a whole number"
((KEEP >= 1)) || bv_die "$BV_EXIT_USAGE" "--keep must be at least 1"
[[ -n $BUCKET ]] || bv_die "$BV_EXIT_CONFIG" "no bucket: pass --bucket or set S3_BUCKET"

ENDPOINT=${ENDPOINT%/}
if [[ $PATH_STYLE == auto ]]; then
  if [[ -z $ENDPOINT || $ENDPOINT == *amazonaws.com* ]]; then PATH_STYLE=0; else PATH_STYLE=1; fi
fi

BV_S3_ENDPOINT=$ENDPOINT
BV_S3_REGION=$REGION
BV_S3_BUCKET=$BUCKET
BV_S3_PATH_STYLE=$PATH_STYLE

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

NORMALIZED_PREFIX=$(bv_trim_slashes "$PREFIX")

list_keys_aws() {
  local args=(s3 ls "s3://$BUCKET/${NORMALIZED_PREFIX:+$NORMALIZED_PREFIX/}" --recursive)
  [[ -n $ENDPOINT ]] && args+=(--endpoint-url "$ENDPOINT")
  [[ -n $REGION ]] && args+=(--region "$REGION")
  AWS_ACCESS_KEY_ID=$BV_S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$BV_S3_SECRET_KEY aws "${args[@]}" |
    awk '{ $1=""; $2=""; $3=""; sub(/^ +/, ""); if (length($0)) print }'
}

delete_key_aws() {
  local args=(s3 rm "s3://$BUCKET/$1")
  [[ -n $ENDPOINT ]] && args+=(--endpoint-url "$ENDPOINT")
  [[ -n $REGION ]] && args+=(--region "$REGION")
  AWS_ACCESS_KEY_ID=$BV_S3_ACCESS_KEY AWS_SECRET_ACCESS_KEY=$BV_S3_SECRET_KEY aws "${args[@]}" >/dev/null
}

mc_env() {
  local host=${ENDPOINT:-https://s3.$REGION.amazonaws.com}
  local scheme=${host%%://*} rest=${host#*://}
  printf 'MC_HOST_backvault=%s://%s:%s@%s' "$scheme" "$(bv_uri_encode "$BV_S3_ACCESS_KEY")" "$(bv_uri_encode "$BV_S3_SECRET_KEY")" "$rest"
}

list_keys_mc() {
  env "$(mc_env)" mc --quiet ls --recursive "backvault/$BUCKET/${NORMALIZED_PREFIX:+$NORMALIZED_PREFIX/}" |
    awk '{ $1=""; $2=""; $3=""; $4=""; sub(/^ +/, ""); if (length($0)) print }' |
    while IFS= read -r name; do
      printf '%s\n' "$(bv_join_path "$NORMALIZED_PREFIX" "$name")"
    done
}

delete_key_mc() {
  env "$(mc_env)" mc --quiet rm "backvault/$BUCKET/$1" >/dev/null
}

list_keys_curl() {
  bv_s3_check_credentials
  local token="" body query code
  body=$(bv_mktemp_file)
  while :; do
    query="list-type=2&max-keys=1000&prefix=$(bv_uri_encode "${NORMALIZED_PREFIX:+$NORMALIZED_PREFIX/}")"
    if [[ -n $token ]]; then
      query="continuation-token=$(bv_uri_encode "$token")&$query"
    fi
    code=$(bv_s3_request GET "" "$query" "$(bv_sha256_hex_string '')" "" "$body")
    if [[ ! $code =~ ^2 ]]; then
      bv_error "listing failed with HTTP ${code:-none}$(bv_s3_error_message "$body" | sed 's/^/ - /')"
      return 8
    fi
    tr '<' '\n' <"$body" | sed -n 's/^Key>\(.*\)/\1/p' | sed 's/&amp;/\&/g; s/&lt;/</g; s/&gt;/>/g; s/&quot;/"/g; s/&#34;/"/g; s/&#39;/'"'"'/g'
    if grep -q '<IsTruncated>true</IsTruncated>' "$body"; then
      token=$(tr '<' '\n' <"$body" | sed -n 's/^NextContinuationToken>\(.*\)/\1/p' | head -n 1)
      [[ -n $token ]] || break
    else
      break
    fi
  done
  rm -f -- "$body"
}

delete_key_curl() {
  local body code
  body=$(bv_mktemp_file)
  code=$(bv_s3_request DELETE "$1" "" "$(bv_sha256_hex_string '')" "" "$body")
  local message
  message=$(bv_s3_error_message "$body")
  rm -f -- "$body"
  if [[ $code =~ ^2 ]]; then return 0; fi
  bv_error "delete of $1 failed with HTTP ${code:-none}${message:+ - $message}"
  return 1
}

list_keys() {
  case $METHOD in
    aws) list_keys_aws ;;
    mc) list_keys_mc ;;
    curl) list_keys_curl ;;
  esac
}

delete_key() {
  case $METHOD in
    aws) delete_key_aws "$1" ;;
    mc) delete_key_mc "$1" ;;
    curl) delete_key_curl "$1" ;;
  esac
}

bv_info "listing s3://$BUCKET/${NORMALIZED_PREFIX:+$NORMALIZED_PREFIX/} using $METHOD"

CANDIDATES=$(bv_mktemp_file)
SKIPPED=0
TOTAL=0
while IFS= read -r key; do
  [[ -n $key ]] || continue
  TOTAL=$((TOTAL + 1))
  name=${key##*/}
  [[ -n $name ]] || continue
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
  printf '%s\t%s\n' "$stamp" "$key" >>"$CANDIDATES"
done < <(list_keys)

MATCHED=$(wc -l <"$CANDIDATES" | tr -d ' ')
bv_info "$TOTAL objects listed, $MATCHED match the pattern, $SKIPPED without a timestamp are kept"

if ((MATCHED <= KEEP)); then
  bv_info "nothing to delete, keeping the newest $KEEP"
  exit 0
fi

DELETE_LIST=$(bv_mktemp_file)
sort -r <"$CANDIDATES" | tail -n +$((KEEP + 1)) | cut -f2- >"$DELETE_LIST"

failures=0
deleted=0
while IFS= read -r key; do
  [[ -n $key ]] || continue
  if ((DRY_RUN)); then
    bv_info "dry run: would delete s3://$BUCKET/$key"
    continue
  fi
  if delete_key "$key"; then
    deleted=$((deleted + 1))
    bv_info "deleted s3://$BUCKET/$key"
  else
    failures=$((failures + 1))
  fi
done <"$DELETE_LIST"

if ((DRY_RUN)); then
  bv_info "dry run: $(wc -l <"$DELETE_LIST" | tr -d ' ') objects would be deleted, $KEEP kept"
  exit 0
fi

bv_info "deleted $deleted objects, kept $KEEP"
if ((failures > 0)); then
  bv_die "$BV_EXIT_DELIVER" "$failures deletions failed"
fi
