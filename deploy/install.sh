#!/usr/bin/env bash
set -Eeuo pipefail

PROG=install.sh
REPO_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)

usage() {
  cat <<'USAGE'
install.sh - install Backvault as a systemd service on a Linux host.

Run it from a checkout to build from source, pass --download to fetch a released binary from
GitHub, or point it at a binary you already have. Outside a checkout it downloads by default.
Running it again upgrades the binary and leaves the configuration, the data directory and the
master key untouched.

Usage:
  sudo ./deploy/install.sh
  sudo ./deploy/install.sh --download
  sudo ./deploy/install.sh --download --version 0.1.0
  sudo ./deploy/install.sh --binary /tmp/backvault
  sudo ./deploy/install.sh --uninstall [--purge]

Options:
  --download          download the release archive for this host and verify its checksum
  --version VERSION   download this version instead of the latest, implies --download
  --binary PATH       install this binary instead of building from source
  --prefix DIR        directory for the binary, default /usr/local/bin
  --data-dir DIR      state directory, default /var/lib/backvault
  --config-dir DIR    configuration directory, default /etc/backvault
  --user NAME         service user, default backvault
  --group NAME        service group, default the service user
  --listen ADDR       listen address written into a fresh config, default :8080
  --no-service        install files but do not touch systemd
  --no-start          install and enable the unit without starting it
  --uninstall         stop the service and remove the unit and the binary
  --purge             with --uninstall, also remove the data directory, the config and the user
  --yes               do not ask for confirmation
  --dry-run           print what would happen and change nothing
  -h, --help          this help

Exit codes:
  0 ok, 2 usage, 3 missing dependency, 4 configuration, 1 an install step failed.
USAGE
}

log() {
  printf '%s [%s] %s: %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$1" "$PROG" "${*:2}" >&2
}
info() { log INFO "$@"; }
warn() { log WARN "$@"; }
err() { log ERROR "$@"; }
die() {
  local code=$1
  shift
  err "$@"
  exit "$code"
}

GITHUB_REPO=arthurr0/backvault

BINARY=""
DOWNLOAD=0
RELEASE_VERSION=""
DOWNLOAD_DIR=""
PREFIX=/usr/local/bin
DATA_DIR=/var/lib/backvault
CONFIG_DIR=/etc/backvault
SERVICE_USER=backvault
SERVICE_GROUP=""
LISTEN=":8080"
WITH_SERVICE=1
START_SERVICE=1
ACTION=install
PURGE=0
ASSUME_YES=0
DRY_RUN=0

while (($#)); do
  case $1 in
    --binary) BINARY=${2:?--binary needs a value}; shift 2 ;;
    --download) DOWNLOAD=1; shift ;;
    --version) RELEASE_VERSION=${2:?--version needs a value}; DOWNLOAD=1; shift 2 ;;
    --prefix) PREFIX=${2:?--prefix needs a value}; shift 2 ;;
    --data-dir) DATA_DIR=${2:?--data-dir needs a value}; shift 2 ;;
    --config-dir) CONFIG_DIR=${2:?--config-dir needs a value}; shift 2 ;;
    --user) SERVICE_USER=${2:?--user needs a value}; shift 2 ;;
    --group) SERVICE_GROUP=${2:?--group needs a value}; shift 2 ;;
    --listen) LISTEN=${2:?--listen needs a value}; shift 2 ;;
    --no-service) WITH_SERVICE=0; shift ;;
    --no-start) START_SERVICE=0; shift ;;
    --uninstall) ACTION=uninstall; shift ;;
    --purge) PURGE=1; shift ;;
    --yes) ASSUME_YES=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h | --help) usage; exit 0 ;;
    -*) usage >&2; die 2 "unknown option: $1" ;;
    *) BINARY=$1; shift ;;
  esac
done

[[ -n $SERVICE_GROUP ]] || SERVICE_GROUP=$SERVICE_USER
UNIT_PATH=/etc/systemd/system/backvault.service

run() {
  if ((DRY_RUN)); then
    printf 'would run:' >&2
    printf ' %q' "$@" >&2
    printf '\n' >&2
    return 0
  fi
  "$@"
}

require_root() {
  if ((DRY_RUN)); then
    return 0
  fi
  [[ $(id -u) == 0 ]] || die 4 "run this as root, for example with sudo"
}

confirm() {
  ((ASSUME_YES)) && return 0
  ((DRY_RUN)) && return 0
  [[ -t 0 ]] || return 0
  local answer
  read -r -p "$1 [y/N]: " answer </dev/tty
  case $answer in
    y | Y | yes) return 0 ;;
    *) return 1 ;;
  esac
}

have() { command -v -- "$1" >/dev/null 2>&1; }

systemd_available() {
  have systemctl && [[ -d /run/systemd/system ]]
}

uninstall() {
  require_root
  if ((WITH_SERVICE)) && systemd_available; then
    if systemctl list-unit-files backvault.service >/dev/null 2>&1; then
      run systemctl disable --now backvault.service || warn "could not disable backvault.service"
    fi
    if [[ -f $UNIT_PATH ]]; then
      run rm -f -- "$UNIT_PATH"
      run systemctl daemon-reload
    fi
  fi
  if [[ -f "$PREFIX/backvault" ]]; then
    run rm -f -- "$PREFIX/backvault"
    info "removed $PREFIX/backvault"
  fi
  if ((PURGE)); then
    if confirm "Delete $DATA_DIR including the master key and every backup record?"; then
      run rm -rf -- "$DATA_DIR"
      run rm -rf -- "$CONFIG_DIR"
      if id -u "$SERVICE_USER" >/dev/null 2>&1; then
        run userdel "$SERVICE_USER" || warn "could not remove the user $SERVICE_USER"
      fi
      info "purged $DATA_DIR and $CONFIG_DIR"
    else
      info "kept $DATA_DIR and $CONFIG_DIR"
    fi
  else
    info "kept $DATA_DIR and $CONFIG_DIR, pass --purge to remove them"
  fi
  info "uninstalled"
}

cleanup() {
  if [[ -n $DOWNLOAD_DIR && -d $DOWNLOAD_DIR ]]; then
    rm -rf -- "$DOWNLOAD_DIR"
  fi
}
trap cleanup EXIT

ASSET_REF=main

repo_asset() {
  local rel=$1 out
  if [[ -f "$REPO_DIR/$rel" ]]; then
    printf '%s\n' "$REPO_DIR/$rel"
    return 0
  fi
  if ((DRY_RUN)); then
    printf '%s\n' "https://raw.githubusercontent.com/$GITHUB_REPO/$ASSET_REF/$rel"
    return 0
  fi
  [[ -n $DOWNLOAD_DIR ]] || DOWNLOAD_DIR=$(mktemp -d)
  out="$DOWNLOAD_DIR/$(basename -- "$rel")"
  if [[ ! -f $out ]]; then
    fetch "https://raw.githubusercontent.com/$GITHUB_REPO/$ASSET_REF/$rel" "$out" ||
      die 1 "could not fetch $rel for $ASSET_REF, run the installer from a checkout instead"
  fi
  printf '%s\n' "$out"
}

target_arch() {
  local machine
  machine=$(uname -m)
  case $machine in
    x86_64 | amd64) printf 'amd64\n' ;;
    aarch64 | arm64) printf 'arm64\n' ;;
    *) die 3 "no release is built for $machine, build from source instead" ;;
  esac
}

fetch() {
  local url=$1 out=$2
  if have curl; then
    curl -fsSL --retry 3 --retry-delay 2 -o "$out" -- "$url"
  elif have wget; then
    wget -q -O "$out" -- "$url"
  else
    die 3 "no curl or wget found, download the release by hand and pass --binary <path>"
  fi
}

latest_version() {
  local url location
  url="https://github.com/$GITHUB_REPO/releases/latest"
  if have curl; then
    location=$(curl -fsSLI -o /dev/null -w '%{url_effective}' -- "$url" 2>/dev/null || true)
  elif have wget; then
    location=$(wget -q -S --max-redirect 10 -O /dev/null -- "$url" 2>&1 | awk '/[Ll]ocation: /{print $2}' | tail -n 1 || true)
  else
    die 3 "no curl or wget found, pass --version or download the release by hand"
  fi
  if [[ $location != *"/releases/tag/"* ]]; then
    die 1 "no published release found at https://github.com/$GITHUB_REPO/releases, pass --version once one exists or build from source"
  fi
  printf '%s\n' "${location##*/releases/tag/}"
}

verify_checksum() {
  local dir=$1 asset=$2 expected actual
  expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1 }' "$dir/checksums.txt" | head -n 1)
  [[ -n $expected ]] || die 1 "checksums.txt does not list $asset"
  if have sha256sum; then
    actual=$(sha256sum -- "$dir/$asset" | awk '{print $1}')
  elif have shasum; then
    actual=$(shasum -a 256 -- "$dir/$asset" | awk '{print $1}')
  else
    die 3 "no sha256sum or shasum found, cannot verify the download"
  fi
  [[ $actual == "$expected" ]] || die 1 "checksum mismatch for $asset, expected $expected and got $actual"
  info "checksum verified"
}

download_binary() {
  local version arch asset base
  version=$RELEASE_VERSION
  if [[ -z $version ]]; then
    info "resolving the latest release of $GITHUB_REPO"
    version=$(latest_version)
  fi
  version=${version#v}
  ASSET_REF="v${version}"
  arch=$(target_arch)
  asset="backvault_${version}_linux_${arch}.tar.gz"
  base="https://github.com/$GITHUB_REPO/releases/download/v${version}"

  if ((DRY_RUN)); then
    printf 'would download %s\n' "$base/$asset" >&2
    printf 'would download %s\n' "$base/checksums.txt" >&2
    printf 'would verify %s against checksums.txt\n' "$asset" >&2
    BINARY="$PWD/backvault"
    return 0
  fi

  DOWNLOAD_DIR=$(mktemp -d)
  info "downloading backvault $version for linux/$arch"
  fetch "$base/$asset" "$DOWNLOAD_DIR/$asset" ||
    die 1 "could not download $base/$asset, check that the release exists and lists that asset"
  fetch "$base/checksums.txt" "$DOWNLOAD_DIR/checksums.txt" ||
    die 1 "could not download $base/checksums.txt"
  verify_checksum "$DOWNLOAD_DIR" "$asset"
  tar xzf "$DOWNLOAD_DIR/$asset" -C "$DOWNLOAD_DIR" ||
    die 1 "could not unpack $asset"
  [[ -f "$DOWNLOAD_DIR/backvault" ]] || die 1 "$asset did not contain a backvault binary"
  chmod 0755 "$DOWNLOAD_DIR/backvault"
  BINARY="$DOWNLOAD_DIR/backvault"
}

build_binary() {
  have go || die 3 "no go toolchain found, pass --download to fetch a released binary or --binary <path>"
  [[ -f "$REPO_DIR/go.mod" ]] || die 4 "no go.mod in $REPO_DIR, run this from a checkout or pass --download"
  if [[ ! -f "$REPO_DIR/web/dist/index.html" ]]; then
    if have npm; then
      info "building the admin panel"
      run sh -c "cd $(printf '%q' "$REPO_DIR/web") && npm ci && npm run build"
    else
      die 3 "web/dist is missing and npm is not installed, build the panel first or pass --binary"
    fi
  fi
  local version commit date
  version=$(cd "$REPO_DIR" && git describe --tags --always --dirty 2>/dev/null || echo dev)
  commit=$(cd "$REPO_DIR" && git rev-parse --short HEAD 2>/dev/null || echo none)
  date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  info "building backvault $version"
  run sh -c "cd $(printf '%q' "$REPO_DIR") && CGO_ENABLED=0 go build -trimpath -ldflags $(printf '%q' "-s -w -X github.com/arthurr0/backvault/internal/version.Version=$version -X github.com/arthurr0/backvault/internal/version.Commit=$commit -X github.com/arthurr0/backvault/internal/version.BuildDate=$date") -o $(printf '%q' "$REPO_DIR/bin/backvault") ./cmd/backvault"
  BINARY="$REPO_DIR/bin/backvault"
}

install_all() {
  require_root

  if [[ -n $BINARY ]]; then
    [[ -f $BINARY ]] || die 4 "no such binary: $BINARY"
  elif ((DOWNLOAD)) || [[ ! -f "$REPO_DIR/go.mod" ]]; then
    download_binary
  else
    build_binary
  fi

  if ! getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
    info "creating the group $SERVICE_GROUP"
    run groupadd --system "$SERVICE_GROUP"
  fi
  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    info "creating the user $SERVICE_USER"
    run useradd --system --gid "$SERVICE_GROUP" --home-dir "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
  fi

  run install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" -- "$DATA_DIR"
  run install -d -m 0750 -o "$SERVICE_USER" -g "$SERVICE_GROUP" -- "$DATA_DIR/work"
  run install -d -m 0750 -o root -g "$SERVICE_GROUP" -- "$CONFIG_DIR"

  info "installing the binary to $PREFIX/backvault"
  run install -d -m 0755 -- "$PREFIX"
  run install -m 0755 -- "$BINARY" "$PREFIX/backvault"

  if [[ ! -f "$CONFIG_DIR/backvault.yaml" ]]; then
    info "writing $CONFIG_DIR/backvault.yaml"
    if ((DRY_RUN)); then
      printf 'would write %s\n' "$CONFIG_DIR/backvault.yaml" >&2
    else
      sed -e "s#^listen:.*#listen: \"$LISTEN\"#" \
          -e "s#^data_dir:.*#data_dir: $DATA_DIR#" \
          -e "s#^work_dir:.*#work_dir: $DATA_DIR/work#" \
          -e "s#^master_key_file:.*#master_key_file: $DATA_DIR/master.key#" \
          "$(repo_asset deploy/backvault.example.yaml)" >"$CONFIG_DIR/backvault.yaml"
      chown root:"$SERVICE_GROUP" "$CONFIG_DIR/backvault.yaml"
      chmod 0640 "$CONFIG_DIR/backvault.yaml"
    fi
  else
    info "keeping the existing $CONFIG_DIR/backvault.yaml"
  fi

  if [[ ! -f "$CONFIG_DIR/backvault.env" ]]; then
    info "writing $CONFIG_DIR/backvault.env"
    run install -m 0640 -o root -g "$SERVICE_GROUP" -- "$(repo_asset deploy/systemd/backvault.env.example)" "$CONFIG_DIR/backvault.env"
  else
    info "keeping the existing $CONFIG_DIR/backvault.env"
  fi

  if ((WITH_SERVICE == 0)); then
    info "skipping systemd as requested"
    info "start it yourself with: $PREFIX/backvault serve --config $CONFIG_DIR/backvault.yaml"
    return 0
  fi

  systemd_available || {
    warn "systemd is not running here, the unit was not installed"
    return 0
  }

  info "installing $UNIT_PATH"
  if ((DRY_RUN)); then
    printf 'would write %s\n' "$UNIT_PATH" >&2
  else
    sed -e "s#^User=.*#User=$SERVICE_USER#" \
        -e "s#^Group=.*#Group=$SERVICE_GROUP#" \
        -e "s#^WorkingDirectory=.*#WorkingDirectory=$DATA_DIR#" \
        -e "s#^EnvironmentFile=.*#EnvironmentFile=-$CONFIG_DIR/backvault.env#" \
        -e "s#^ExecStart=.*#ExecStart=$PREFIX/backvault serve --config $CONFIG_DIR/backvault.yaml#" \
        -e "s#^ReadWritePaths=.*#ReadWritePaths=$DATA_DIR#" \
        -e "s#^ReadOnlyPaths=.*#ReadOnlyPaths=$CONFIG_DIR#" \
        "$(repo_asset deploy/systemd/backvault.service)" >"$UNIT_PATH"
    chmod 0644 "$UNIT_PATH"
  fi

  run systemctl daemon-reload
  run systemctl enable backvault.service
  if ((START_SERVICE)); then
    run systemctl restart backvault.service
    info "backvault is running, check it with: systemctl status backvault"
  else
    info "enabled but not started, start it with: systemctl start backvault"
  fi
  info "open the panel and create the first admin account"
}

case $ACTION in
  install) install_all ;;
  uninstall) uninstall ;;
esac
