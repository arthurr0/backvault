#!/usr/bin/env bash

BV_EXIT_ERROR=1
BV_EXIT_USAGE=2
BV_EXIT_DEPENDENCY=3
BV_EXIT_CONFIG=4
BV_EXIT_DUMP=5
BV_EXIT_PACK=6
BV_EXIT_DELIVER=7
BV_EXIT_REMOTE=8
BV_EXIT_VERIFY=9

BV_LIB_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
BV_SCRIPTS_DIR=$(dirname -- "$BV_LIB_DIR")
BV_PROG=${BV_PROG:-$(basename -- "${BASH_SOURCE[1]:-backvault}")}

BV_TMP_FILES=()
BV_TMP_DIRS=()
BV_TMPDIR=${BACKVAULT_TMPDIR:-${TMPDIR:-/tmp}}

BV_TO=${BACKVAULT_TO:-backvault}
BV_DEST_DIR=${BACKVAULT_DEST_DIR:-}
BV_COMPRESS_WANT=${BACKVAULT_COMPRESS:-auto}
BV_COMPRESS_LEVEL=${BACKVAULT_COMPRESS_LEVEL:-}
BV_ENCRYPT_WANT=${BACKVAULT_ENCRYPT:-none}
BV_PASSPHRASE_FILE=${BACKVAULT_PASSPHRASE_FILE:-}
BV_PREFIX=${BACKVAULT_PREFIX:-}
BV_NAME=""
BV_DRY_RUN=0
BV_COMPRESS=none
BV_COMPRESS_EXT=""
BV_COMPRESS_CMD=(cat)
BV_ENCRYPT=none
BV_ENCRYPT_EXT=""
BV_ARG_CONSUMED=0
BV_ARTIFACT=""
BV_ARTIFACT_NAME=""

bv_timestamp() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
bv_stamp() { date -u '+%Y%m%d-%H%M%S'; }

bv_log() {
  local level=$1
  shift
  printf '%s [%s] %s: %s\n' "$(bv_timestamp)" "$level" "$BV_PROG" "$*" >&2
}

bv_info() { bv_log INFO "$@"; }
bv_warn() { bv_log WARN "$@"; }
bv_error() { bv_log ERROR "$@"; }

bv_debug() {
  if [[ ${BACKVAULT_DEBUG:-0} == 1 ]]; then
    bv_log DEBUG "$@"
  fi
}

bv_die() {
  local code=$1
  shift
  bv_error "$@"
  exit "$code"
}

bv_cleanup() {
  local status=$?
  local path
  for path in ${BV_TMP_FILES[@]+"${BV_TMP_FILES[@]}"}; do
    [[ -n $path && -e $path ]] && rm -f -- "$path"
  done
  for path in ${BV_TMP_DIRS[@]+"${BV_TMP_DIRS[@]}"}; do
    [[ -n $path && -d $path ]] && rm -rf -- "$path"
  done
  return "$status"
}

bv_install_traps() {
  trap 'bv_cleanup' EXIT
  trap 'bv_cleanup; exit 130' INT
  trap 'bv_cleanup; exit 143' TERM
}

bv_track_file() { BV_TMP_FILES+=("$1"); }
bv_track_dir() { BV_TMP_DIRS+=("$1"); }

bv_mktemp_file() {
  local path
  path=$(mktemp -- "$BV_TMPDIR/backvault.XXXXXXXXXX") || bv_die "$BV_EXIT_CONFIG" "cannot create a temporary file in $BV_TMPDIR"
  bv_track_file "$path"
  printf '%s\n' "$path"
}

bv_mktemp_dir() {
  local path
  path=$(mktemp -d -- "$BV_TMPDIR/backvault.XXXXXXXXXX") || bv_die "$BV_EXIT_CONFIG" "cannot create a temporary directory in $BV_TMPDIR"
  bv_track_dir "$path"
  printf '%s\n' "$path"
}

bv_have() { command -v -- "$1" >/dev/null 2>&1; }

bv_require() {
  bv_have "$1" || bv_die "$BV_EXIT_DEPENDENCY" "required command not found: $1${2:+ ($2)}"
}

bv_need_value() {
  [[ $# -ge 2 && -n ${2:-} ]] || bv_die "$BV_EXIT_USAGE" "option $1 requires a value"
}

bv_sha256() {
  local path=$1
  if bv_have sha256sum; then
    sha256sum -- "$path" | awk '{print $1}'
  elif bv_have shasum; then
    shasum -a 256 -- "$path" | awk '{print $1}'
  elif bv_have openssl; then
    openssl dgst -sha256 -r -- "$path" | awk '{print $1}'
  else
    bv_die "$BV_EXIT_DEPENDENCY" "no sha256 tool found: install coreutils, perl shasum or openssl"
  fi
}

bv_sha256_stdin() {
  if bv_have sha256sum; then
    sha256sum | awk '{print $1}'
  elif bv_have shasum; then
    shasum -a 256 | awk '{print $1}'
  else
    openssl dgst -sha256 | awk '{print $NF}'
  fi
}

bv_filesize() {
  local path=$1 size
  if size=$(stat -c %s -- "$path" 2>/dev/null); then
    printf '%s\n' "$size"
  elif size=$(stat -f %z -- "$path" 2>/dev/null); then
    printf '%s\n' "$size"
  else
    wc -c <"$path" | tr -d ' '
  fi
}

bv_human_bytes() {
  awk -v bytes="$1" 'BEGIN {
    split("B KiB MiB GiB TiB PiB", unit, " ")
    i = 1
    while (bytes >= 1024 && i < 6) { bytes /= 1024; i++ }
    if (i == 1) printf "%d %s\n", bytes, unit[i]; else printf "%.1f %s\n", bytes, unit[i]
  }'
}

bv_hex() { od -An -v -tx1 | tr -d ' \n'; }

bv_unhex() {
  local hex out='' i
  hex=$(cat)
  for ((i = 0; i < ${#hex}; i += 2)); do out+="\\x${hex:i:2}"; done
  printf '%b' "$out"
}

bv_hmac_sha256_hex() {
  local keyhex=$1 padded='' ipad='' opad='' inner byte i
  padded=$keyhex
  if ((${#padded} > 128)); then
    padded=$(printf '%s' "$padded" | bv_unhex | openssl dgst -sha256 -binary | bv_hex)
  fi
  while ((${#padded} < 128)); do padded+='00'; done
  for ((i = 0; i < 128; i += 2)); do
    byte=$((16#${padded:i:2}))
    printf -v ipad '%s\\x%02x' "$ipad" "$((byte ^ 0x36))"
    printf -v opad '%s\\x%02x' "$opad" "$((byte ^ 0x5c))"
  done
  inner=$({ printf '%b' "$ipad"; cat; } | openssl dgst -sha256 -binary | bv_hex)
  { printf '%b' "$opad"; printf '%s' "$inner" | bv_unhex; } | openssl dgst -sha256 -binary | bv_hex
}

bv_sha256_hex_string() { printf '%s' "$1" | openssl dgst -sha256 -binary | bv_hex; }

bv_uri_encode() {
  local input=$1 keep_slash=${2:-0} out='' i char
  for ((i = 0; i < ${#input}; i++)); do
    char=${input:i:1}
    case $char in
      [a-zA-Z0-9._~-]) out+=$char ;;
      /) if ((keep_slash)); then out+=$char; else printf -v out '%s%%2F' "$out"; fi ;;
      *) printf -v out '%s%%%02X' "$out" "'$char" ;;
    esac
  done
  printf '%s' "$out"
}

bv_trim_slashes() {
  local value=$1
  value=${value#/}
  value=${value%/}
  printf '%s' "$value"
}

bv_join_path() {
  local base=$1 name=$2
  base=$(bv_trim_slashes "$base")
  if [[ -z $base ]]; then
    printf '%s' "$name"
  else
    printf '%s/%s' "$base" "$name"
  fi
}

bv_read_secret_file() {
  local path=$1
  [[ -r $path ]] || bv_die "$BV_EXIT_CONFIG" "cannot read secret file: $path"
  [[ -s $path ]] || bv_die "$BV_EXIT_CONFIG" "secret file is empty: $path"
  bv_check_permissions "$path"
  head -n 1 -- "$path" | tr -d '\r\n'
}

bv_check_permissions() {
  local path=$1 mode
  mode=$(stat -c %a -- "$path" 2>/dev/null || stat -f %Lp -- "$path" 2>/dev/null || echo "")
  if [[ -n $mode && ${mode: -1} != 0 ]]; then
    bv_warn "$path is readable by other users, chmod 600 it"
  fi
}

bv_resolve_compression() {
  local want=${1:-auto}
  case $want in
    auto)
      if bv_have zstd; then want=zstd; elif bv_have gzip; then want=gzip; else want=none; fi
      ;;
    zst) want=zstd ;;
    gz) want=gzip ;;
    zstd | gzip | none) ;;
    *) bv_die "$BV_EXIT_USAGE" "unknown compression: $want (want zstd, gzip, none or auto)" ;;
  esac
  case $want in
    zstd)
      bv_require zstd "install zstd or use --compress gzip"
      BV_COMPRESS=zstd
      BV_COMPRESS_EXT=.zst
      BV_COMPRESS_CMD=(zstd -q -T0 "-${BV_COMPRESS_LEVEL:-6}" -c)
      ;;
    gzip)
      bv_require gzip
      BV_COMPRESS=gzip
      BV_COMPRESS_EXT=.gz
      BV_COMPRESS_CMD=(gzip "-${BV_COMPRESS_LEVEL:-6}" -c)
      ;;
    none)
      BV_COMPRESS=none
      BV_COMPRESS_EXT=""
      BV_COMPRESS_CMD=(cat)
      ;;
  esac
}

bv_resolve_encryption() {
  local want=${1:-none}
  case $want in
    auto)
      if bv_have age; then want=age; elif bv_have openssl; then want=openssl; else want=none; fi
      ;;
    age | openssl | none) ;;
    *) bv_die "$BV_EXIT_USAGE" "unknown encryption: $want (want age, openssl, none or auto)" ;;
  esac
  if [[ $want != none ]]; then
    [[ -n $BV_PASSPHRASE_FILE ]] || bv_die "$BV_EXIT_CONFIG" "--passphrase-file is required when --encrypt is not none"
    [[ -r $BV_PASSPHRASE_FILE ]] || bv_die "$BV_EXIT_CONFIG" "cannot read passphrase file: $BV_PASSPHRASE_FILE"
    [[ -s $BV_PASSPHRASE_FILE ]] || bv_die "$BV_EXIT_CONFIG" "passphrase file is empty: $BV_PASSPHRASE_FILE"
    bv_check_permissions "$BV_PASSPHRASE_FILE"
  fi
  case $want in
    age)
      bv_require age "install age or use --encrypt openssl"
      BV_ENCRYPT=age
      BV_ENCRYPT_EXT=.age
      ;;
    openssl)
      bv_require openssl
      BV_ENCRYPT=openssl
      BV_ENCRYPT_EXT=.enc
      ;;
    none)
      BV_ENCRYPT=none
      BV_ENCRYPT_EXT=""
      ;;
  esac
}

bv_age_supports_passphrase_file() {
  age --help 2>&1 | grep -q -- '--passphrase-file'
}

bv_age_encrypt() {
  local input=$1 output=$2
  if bv_age_supports_passphrase_file; then
    age --encrypt --passphrase-file "$BV_PASSPHRASE_FILE" --output "$output" -- "$input"
    return
  fi
  if bv_have script; then
    local quoted_in quoted_out passphrase
    printf -v quoted_in '%q' "$input"
    printf -v quoted_out '%q' "$output"
    passphrase=$(bv_read_secret_file "$BV_PASSPHRASE_FILE")
    printf '%s\n%s\n' "$passphrase" "$passphrase" |
      script -q -e -c "age --encrypt --passphrase --output $quoted_out -- $quoted_in" /dev/null >/dev/null
    local status=$?
    passphrase=""
    unset passphrase
    return $status
  fi
  bv_die "$BV_EXIT_DEPENDENCY" "this age build needs a terminal for passphrases: install util-linux script or use --encrypt openssl"
}

bv_encrypt_file() {
  local input=$1 output=$2
  case $BV_ENCRYPT in
    none)
      mv -- "$input" "$output"
      ;;
    openssl)
      openssl enc -aes-256-cbc -pbkdf2 -salt -in "$input" -out "$output" -pass file:"$BV_PASSPHRASE_FILE" ||
        bv_die "$BV_EXIT_PACK" "openssl encryption failed"
      rm -f -- "$input"
      ;;
    age)
      bv_age_encrypt "$input" "$output" || bv_die "$BV_EXIT_PACK" "age encryption failed"
      rm -f -- "$input"
      ;;
  esac
}

bv_dump_to_file() {
  local output=$1
  shift
  bv_debug "dump command: $*"
  if [[ $BV_COMPRESS == none ]]; then
    "$@" >"$output"
  else
    "$@" | "${BV_COMPRESS_CMD[@]}" >"$output"
  fi
}

bv_common_arg() {
  BV_ARG_CONSUMED=0
  case ${1:-} in
    --to)
      bv_need_value "$@"
      BV_TO=$2
      BV_ARG_CONSUMED=2
      ;;
    --dest-dir)
      bv_need_value "$@"
      BV_DEST_DIR=$2
      BV_ARG_CONSUMED=2
      ;;
    --compress)
      bv_need_value "$@"
      BV_COMPRESS_WANT=$2
      BV_ARG_CONSUMED=2
      ;;
    --compress-level)
      bv_need_value "$@"
      BV_COMPRESS_LEVEL=$2
      BV_ARG_CONSUMED=2
      ;;
    --encrypt)
      bv_need_value "$@"
      BV_ENCRYPT_WANT=$2
      BV_ARG_CONSUMED=2
      ;;
    --passphrase-file)
      bv_need_value "$@"
      BV_PASSPHRASE_FILE=$2
      BV_ARG_CONSUMED=2
      ;;
    --prefix)
      bv_need_value "$@"
      BV_PREFIX=$2
      BV_ARG_CONSUMED=2
      ;;
    --name)
      bv_need_value "$@"
      BV_NAME=$2
      BV_ARG_CONSUMED=2
      ;;
    --temp-dir)
      bv_need_value "$@"
      BV_TMPDIR=$2
      BV_ARG_CONSUMED=2
      ;;
    --job)
      bv_need_value "$@"
      BACKVAULT_JOB=$2
      export BACKVAULT_JOB
      BV_ARG_CONSUMED=2
      ;;
    --backvault-url)
      bv_need_value "$@"
      BACKVAULT_URL=$2
      export BACKVAULT_URL
      BV_ARG_CONSUMED=2
      ;;
    --token-file)
      bv_need_value "$@"
      BACKVAULT_TOKEN_FILE=$2
      export BACKVAULT_TOKEN_FILE
      BV_ARG_CONSUMED=2
      ;;
    --dry-run)
      BV_DRY_RUN=1
      BV_ARG_CONSUMED=1
      ;;
    --debug)
      BACKVAULT_DEBUG=1
      export BACKVAULT_DEBUG
      BV_ARG_CONSUMED=1
      ;;
  esac
}

bv_common_usage() {
  cat <<'USAGE'
Common options:
  --to backvault|s3|sftp|local   delivery target (default backvault, env BACKVAULT_TO)
  --dest-dir DIR              target directory for --to local
  --compress zstd|gzip|none   compression, default auto (zstd when available)
  --compress-level N          compression level passed to zstd or gzip
  --encrypt age|openssl|none  encryption, default none
  --passphrase-file FILE      passphrase source for --encrypt
  --prefix NAME               filename prefix, default derived from the source
  --name FILENAME             full artifact filename, overrides --prefix and timestamp
  --temp-dir DIR              directory for spool files (default $TMPDIR or /tmp)
  --job SLUG                  Backvault job slug for --to backvault (env BACKVAULT_JOB)
  --backvault-url URL            Backvault base URL for --to backvault (env BACKVAULT_URL)
  --token-file FILE           file holding the Backvault API token (env BACKVAULT_TOKEN_FILE)
  --dry-run                   print the plan and exit without touching the source
  --debug                     verbose logging on stderr
  -h, --help                  this help
USAGE
}

bv_prepare_pack() {
  bv_resolve_compression "$BV_COMPRESS_WANT"
  bv_resolve_encryption "$BV_ENCRYPT_WANT"
}

bv_artifact_name() {
  local base=$1 extension=$2
  if [[ -n $BV_NAME ]]; then
    printf '%s' "$BV_NAME"
    return
  fi
  printf '%s-%s.%s%s%s' "$base" "$(bv_stamp)" "$extension" "$BV_COMPRESS_EXT" "$BV_ENCRYPT_EXT"
}

bv_validate_target() {
  case $BV_TO in
    backvault)
      [[ -n ${BACKVAULT_JOB:-} ]] || bv_die "$BV_EXIT_CONFIG" "--to backvault needs --job or BACKVAULT_JOB"
      [[ -n ${BACKVAULT_URL:-} ]] || bv_die "$BV_EXIT_CONFIG" "--to backvault needs --backvault-url or BACKVAULT_URL"
      ;;
    s3)
      [[ -n ${S3_BUCKET:-} ]] || bv_die "$BV_EXIT_CONFIG" "--to s3 needs S3_BUCKET"
      ;;
    sftp)
      [[ -n ${SFTP_HOST:-} ]] || bv_die "$BV_EXIT_CONFIG" "--to sftp needs SFTP_HOST"
      ;;
    local)
      [[ -n $BV_DEST_DIR ]] || bv_die "$BV_EXIT_CONFIG" "--to local needs --dest-dir"
      ;;
    *)
      bv_die "$BV_EXIT_USAGE" "unknown target: $BV_TO (want backvault, s3, sftp or local)"
      ;;
  esac
}

bv_plan() {
  local name=$1 description=$2
  bv_info "plan: source $description"
  bv_info "plan: compression $BV_COMPRESS, encryption $BV_ENCRYPT"
  bv_info "plan: artifact $name"
  case $BV_TO in
    backvault) bv_info "plan: deliver to Backvault job ${BACKVAULT_JOB:-} at ${BACKVAULT_URL:-}" ;;
    s3) bv_info "plan: deliver to s3://${S3_BUCKET:-}/$(bv_join_path "${S3_PREFIX:-}" "$name")" ;;
    sftp) bv_info "plan: deliver to ${SFTP_USER:-}@${SFTP_HOST:-}:$(bv_join_path "${SFTP_BASE_PATH:-}" "$name")" ;;
    local) bv_info "plan: deliver to $BV_DEST_DIR/$name" ;;
  esac
}

bv_deliver() {
  local path=$1 name=$2
  case $BV_TO in
    backvault)
      "$BV_SCRIPTS_DIR/backvault-push.sh" --file "$path" --name "$name" --packed ||
        bv_die "$BV_EXIT_DELIVER" "push to Backvault failed"
      ;;
    s3)
      "$BV_SCRIPTS_DIR/upload-s3.sh" --file "$path" --name "$name" ||
        bv_die "$BV_EXIT_DELIVER" "upload to S3 failed"
      ;;
    sftp)
      "$BV_SCRIPTS_DIR/upload-sftp.sh" --file "$path" --name "$name" ||
        bv_die "$BV_EXIT_DELIVER" "upload to SFTP failed"
      ;;
    local)
      mkdir -p -- "$BV_DEST_DIR" || bv_die "$BV_EXIT_DELIVER" "cannot create $BV_DEST_DIR"
      local partial="$BV_DEST_DIR/.$name.partial"
      cp -- "$path" "$partial" || bv_die "$BV_EXIT_DELIVER" "cannot copy to $partial"
      mv -- "$partial" "$BV_DEST_DIR/$name" || bv_die "$BV_EXIT_DELIVER" "cannot rename $partial"
      bv_info "stored $BV_DEST_DIR/$name"
      ;;
  esac
}

bv_finish() {
  local spool=$1 name=$2 workdir=$3
  local final="$workdir/$name"
  bv_encrypt_file "$spool" "$final"
  bv_track_file "$final"
  local size sha
  size=$(bv_filesize "$final")
  sha=$(bv_sha256 "$final")
  bv_info "artifact $name, $(bv_human_bytes "$size") ($size bytes), sha256 $sha"
  BV_ARTIFACT=$final
  BV_ARTIFACT_NAME=$name
  bv_deliver "$final" "$name"
  bv_info "done"
}

BV_S3_ENDPOINT=""
BV_S3_REGION="us-east-1"
BV_S3_BUCKET=""
BV_S3_PATH_STYLE=0
BV_S3_ACCESS_KEY=""
BV_S3_SECRET_KEY=""
BV_S3_SSE=""
BV_S3_STORAGE_CLASS=""
BV_S3_INSECURE=0
BV_S3_MAX_TIME=3600

bv_s3_base_host() {
  local endpoint=${BV_S3_ENDPOINT:-https://s3.$BV_S3_REGION.amazonaws.com}
  printf '%s' "${endpoint#*://}"
}

bv_s3_scheme() {
  local endpoint=${BV_S3_ENDPOINT:-https://s3.$BV_S3_REGION.amazonaws.com}
  printf '%s' "${endpoint%%://*}"
}

bv_s3_check_credentials() {
  [[ -n $BV_S3_ACCESS_KEY && -n $BV_S3_SECRET_KEY ]] ||
    bv_die "$BV_EXIT_CONFIG" "no credentials: set S3_ACCESS_KEY and S3_SECRET_KEY"
}

bv_s3_request() {
  local method=$1 key=$2 query=$3 payload_hash=$4 body_file=${5:-} out_file=${6:-/dev/null}
  local scheme base_host host canonical_uri path_prefix="" url amz_date datestamp
  scheme=$(bv_s3_scheme)
  base_host=$(bv_s3_base_host)
  if ((BV_S3_PATH_STYLE)); then
    host=$base_host
    path_prefix="/$(bv_uri_encode "$BV_S3_BUCKET")"
  else
    host="$BV_S3_BUCKET.$base_host"
  fi
  canonical_uri="$path_prefix/$(bv_uri_encode "$key" 1)"
  url="$scheme://$host$canonical_uri"
  [[ -n $query ]] && url="$url?$query"

  amz_date=$(date -u '+%Y%m%dT%H%M%SZ')
  datestamp=${amz_date%%T*}

  local names=(host x-amz-content-sha256 x-amz-date)
  local values=("$host" "$payload_hash" "$amz_date")
  local extra_headers=()
  if [[ $method == PUT ]]; then
    if [[ -n $BV_S3_SSE ]]; then
      names+=(x-amz-server-side-encryption)
      values+=("$BV_S3_SSE")
      extra_headers+=(--header "x-amz-server-side-encryption: $BV_S3_SSE")
    fi
    if [[ -n $BV_S3_STORAGE_CLASS ]]; then
      names+=(x-amz-storage-class)
      values+=("$BV_S3_STORAGE_CLASS")
      extra_headers+=(--header "x-amz-storage-class: $BV_S3_STORAGE_CLASS")
    fi
  fi

  local canonical_headers="" signed_headers="" i
  for i in "${!names[@]}"; do
    canonical_headers+="${names[i]}:${values[i]}"$'\n'
    [[ -n $signed_headers ]] && signed_headers+=";"
    signed_headers+="${names[i]}"
  done

  local canonical_request="$method"$'\n'"$canonical_uri"$'\n'"$query"$'\n'"$canonical_headers"$'\n'"$signed_headers"$'\n'"$payload_hash"
  local scope="$datestamp/$BV_S3_REGION/s3/aws4_request"
  local string_to_sign="AWS4-HMAC-SHA256"$'\n'"$amz_date"$'\n'"$scope"$'\n'"$(bv_sha256_hex_string "$canonical_request")"

  local key_hex date_key region_key service_key signing_key signature
  key_hex=$(printf '%s' "AWS4$BV_S3_SECRET_KEY" | bv_hex)
  date_key=$(printf '%s' "$datestamp" | bv_hmac_sha256_hex "$key_hex")
  region_key=$(printf '%s' "$BV_S3_REGION" | bv_hmac_sha256_hex "$date_key")
  service_key=$(printf '%s' s3 | bv_hmac_sha256_hex "$region_key")
  signing_key=$(printf '%s' aws4_request | bv_hmac_sha256_hex "$service_key")
  signature=$(printf '%s' "$string_to_sign" | bv_hmac_sha256_hex "$signing_key")

  local header_file
  header_file=$(bv_mktemp_file)
  chmod 600 -- "$header_file"
  printf 'Authorization: AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s\n' \
    "$BV_S3_ACCESS_KEY" "$scope" "$signed_headers" "$signature" >"$header_file"

  local curl_args=(
    --silent --show-error
    --request "$method"
    --header "@$header_file"
    --header "x-amz-content-sha256: $payload_hash"
    --header "x-amz-date: $amz_date"
    --header "Expect:"
    --connect-timeout 15
    --max-time "$BV_S3_MAX_TIME"
    --write-out '%{http_code}'
    --output "$out_file"
  )
  curl_args+=(${extra_headers[@]+"${extra_headers[@]}"})
  ((BV_S3_INSECURE)) && curl_args+=(--insecure)
  [[ -n $body_file ]] && curl_args+=(--upload-file "$body_file")

  bv_debug "$method $url"
  curl "${curl_args[@]}" "$url"
  local status=$?
  rm -f -- "$header_file"
  return $status
}

bv_s3_error_message() {
  local body=$1
  [[ -s $body ]] || return 0
  sed -n 's/.*<Message>\([^<]*\)<\/Message>.*/\1/p' <"$body" | head -n 1
}
