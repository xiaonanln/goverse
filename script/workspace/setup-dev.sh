#!/usr/bin/env bash
# Setup development environment for Goverse (idempotent)
# Mirrors the major steps in docker/Dockerfile.dev but for a host workspace.
#
# Usage:
#   ./script/setup-dev-environment.sh [--yes] [--postgres] [--etcd-version X.Y.Z]
# Options:
#   --yes            : non-interactive (assume yes for apt)
#   --postgres       : initialize local PostgreSQL cluster and create DB/user (optional)
#   --etcd-version   : etcd version to install (default: 3.5.10)

set -euo pipefail

# Defaults
ASSUME_YES=false
WITH_POSTGRES=false
ETCD_VERSION="3.5.10"

# parse args
while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes|-y) ASSUME_YES=true; shift;;
    --postgres) WITH_POSTGRES=true; shift;;
    --etcd-version) ETCD_VERSION="$2"; shift 2;;
    -h|--help)
      cat <<EOF
Usage: $0 [--yes] [--postgres] [--etcd-version X.Y.Z]

--yes        : non-interactive apt installs
--postgres   : initialize a local PostgreSQL cluster and create 'goverse' DB and user
--etcd-version: etcd version to install (default: ${ETCD_VERSION})
EOF
      exit 0
      ;;
    *) echo "Unknown arg: $1"; exit 1;;
  esac
done

# helper: run command with sudo if not root
if [[ "$EUID" -ne 0 ]]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  else
    echo "This script needs root privileges for system package installation and some setup steps."
    echo "Please re-run as root or install sudo first."
    exit 1
  fi
else
  SUDO=""
fi

APT_ARGS="-y"
if ! $ASSUME_YES; then
  APT_ARGS=""
fi

echo "==> Starting development environment setup"
echo "etcd version: ${ETCD_VERSION}"
echo "postgres init: ${WITH_POSTGRES}"

# detect apt-get
if ! command -v apt-get >/dev/null 2>&1; then
  echo "ERROR: This script currently supports Debian/Ubuntu systems with apt-get."
  echo "Please install the required packages manually on your OS."
  exit 1
fi

echo "==> Updating apt and installing system packages"
$SUDO apt-get update
$SUDO apt-get install $APT_ARGS dos2unix protobuf-compiler git python3 python3-pip wget curl ca-certificates postgresql postgresql-client || {
  echo "apt-get install failed; you can re-run the script or install packages manually."
  exit 1
}

# Install etcd (idempotent)
if command -v etcd >/dev/null 2>&1; then
  echo "Found existing etcd: $(etcd --version 2>/dev/null | head -n1 || true)"
else
  echo "==> Installing etcd v${ETCD_VERSION}"
  TMPDIR="$(mktemp -d)"
  pushd "$TMPDIR" >/dev/null
  ETCD_ARCHIVE="etcd-v${ETCD_VERSION}-linux-amd64.tar.gz"
  ETCD_URL="https://github.com/etcd-io/etcd/releases/download/v${ETCD_VERSION}/${ETCD_ARCHIVE}"
  wget -q "$ETCD_URL"
  tar xzf "$ETCD_ARCHIVE"
  $SUDO mv "etcd-v${ETCD_VERSION}-linux-amd64/etcd"* /usr/local/bin/
  popd >/dev/null
  rm -rf "$TMPDIR"
  echo "etcd installed to /usr/local/bin"
fi

# Install Python gRPC tools
echo "==> Installing Python grpc tools (grpcio, grpcio-tools)"
if ! python3 -m pip install --user grpcio grpcio-tools 2>/dev/null; then
  # fallback to system install if user install fails
  python3 -m pip install grpcio grpcio-tools
fi

# Install protoc-gen-go and protoc-gen-go-grpc
if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go not found in PATH. Please install Go and ensure 'go' is available before running this script."
  exit 1
fi

echo "==> Installing Go protobuf plugins (protoc-gen-go, protoc-gen-go-grpc)"
GO_PROTOS=(
  "google.golang.org/protobuf/cmd/protoc-gen-go@latest"
  "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
)
for pkg in "${GO_PROTOS[@]}"; do
  echo "Installing $pkg"
  if ! go install "$pkg"; then
    echo "go install $pkg failed; ensure GOPATH/GOBIN is set and your Go environment is healthy."
  fi
done

# Suggest adding GOPATH/bin to PATH
GOBIN="$(go env GOPATH 2>/dev/null)/bin"
if [[ -d "$GOBIN" ]]; then
  echo "Note: Go tools installed to: $GOBIN"
  echo "If you can't run protoc-gen-go or protoc-gen-go-grpc, add this to your shell rc:" 
  echo "  export PATH=\"\$PATH:$GOBIN\""
fi

# Compile protocol buffers (if script exists)
if [[ -x "./script/compile-proto.sh" ]]; then
  echo "==> Compiling protocol buffers via ./script/compile-proto.sh"
  ./script/compile-proto.sh
else
  echo "Warning: ./script/compile-proto.sh not found or not executable. Skipping proto compile."
fi

# Optional: PostgreSQL initialization (invasive) - only if requested
if $WITH_POSTGRES; then
  echo "==> PostgreSQL initialization requested"

  PG_DATA="/var/lib/postgresql/data"
  PG_BIN_DIR="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | head -n1 || true)"
  if [[ -z "$PG_BIN_DIR" ]]; then
    echo "Unable to find /usr/lib/postgresql/*/bin. Postgres installation may differ on your system."
    exit 1
  fi

  if [[ ! -d "$PG_DATA" || -z "$(ls -A "$PG_DATA" 2>/dev/null || true)" ]]; then
    echo "Initializing PostgreSQL cluster in $PG_DATA"
    $SUDO mkdir -p "$PG_DATA"
    $SUDO chown -R postgres:postgres "$PG_DATA"
    $SUDO -u postgres "$PG_BIN_DIR/initdb" -D "$PG_DATA"
  else
    echo "PostgreSQL data directory appears to exist at $PG_DATA; skipping initdb"
  fi

  echo "Configuring Postgres to allow external connections (development) - editing pg_hba.conf and postgresql.conf"
  $SUDO bash -c "echo \"host all all 0.0.0.0/0 md5\" >> $PG_DATA/pg_hba.conf || true"
  $SUDO bash -c "echo \"listen_addresses='*'\" >> $PG_DATA/postgresql.conf || true"

  echo "Starting temporary Postgres to create database and user..."
  $SUDO -u postgres "$PG_BIN_DIR/pg_ctl" -D "$PG_DATA" -o "-c listen_addresses='localhost'" -w start
  sleep 2
  $SUDO -u postgres psql -c "ALTER USER postgres PASSWORD 'postgres';" || true
  $SUDO -u postgres psql -c "CREATE DATABASE goverse;" || true
  $SUDO -u postgres psql -c "CREATE USER goverse WITH PASSWORD 'goverse';" || true
  $SUDO -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;" || true
  $SUDO -u postgres psql -d goverse -c "GRANT ALL ON SCHEMA public TO goverse;" || true
  echo "Stopping temporary Postgres..."
  $SUDO -u postgres "$PG_BIN_DIR/pg_ctl" -D "$PG_DATA" -w stop
  echo "Postgres database 'goverse' and user 'goverse' created (password: 'goverse')."
else
  echo "Postgres setup was not requested; skipping."
fi

echo "==> Final notes / next steps"
echo " - Make sure your shell PATH includes the Go bin dir (e.g. add to ~/.profile or ~/.bashrc):"
echo "     export PATH=\"\$PATH:$(go env GOPATH 2>/dev/null)/bin\""
echo " - If you installed etcd, you can start it with: etcd &"
echo " - To run tests or build the project, ensure proto generation succeeded and then run:" 
echo "     go test ./...   (or follow instructions in README.md / TESTING.md)"
echo "Done."
