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
#!/usr/bin/env bash
# Setup development environment for Goverse (idempotent)
# Mirrors the major steps in docker/Dockerfile.dev but for a host workspace.
#
# Usage:
#   ./script/workspace/setup-dev.sh [--yes] [--postgres] [--etcd-version X.Y.Z]
# Options:
#   --yes            : non-interactive (assume yes for apt)
#   --postgres       : initialize local PostgreSQL cluster and create DB/user (optional)
#   --etcd-version   : etcd version to install (default: 3.5.10)

set -euo pipefail

# Defaults
ASSUME_YES=true
WITH_POSTGRES=true
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

# Determine whether system-level installs are possible. If not, perform user-local installs
SKIP_SYSTEM_INSTALLS=false
SUDO=""
if [[ "$EUID" -eq 0 ]]; then
  SUDO=""
elif command -v sudo >/dev/null 2>&1; then
  SUDO="sudo"
else
  SKIP_SYSTEM_INSTALLS=true
  echo "Note: running without root or sudo — system package installation and Postgres init will be skipped."
  echo "You can re-run with sudo if you want system-level installs."
fi

APT_ARGS="-y"
if ! $ASSUME_YES; then
  APT_ARGS=""
fi

echo "==> Starting development environment setup"
echo "etcd version: ${ETCD_VERSION}"
echo "postgres init: ${WITH_POSTGRES}"

# If possible, install system packages; otherwise print instructions
if ! $SKIP_SYSTEM_INSTALLS; then
  if ! command -v apt-get >/dev/null 2>&1; then
    echo "WARNING: apt-get not found. Please install required packages manually."
  else
    echo "==> Updating apt and installing system packages"
    $SUDO apt-get update
    $SUDO apt-get install $APT_ARGS dos2unix protobuf-compiler git python3 python3-pip wget curl ca-certificates postgresql postgresql-client || {
      echo "apt-get install failed; you can re-run the script or install packages manually."
    }
  fi
else
  echo "==> Skipping apt installs (no sudo). Please install these packages yourself if needed:" 
  echo "   dos2unix protobuf-compiler git python3 python3-pip wget curl ca-certificates postgresql postgresql-client"
fi

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
  # Prefer system install if allowed; otherwise install to user-local bin
  if [[ -w "/usr/local/bin" && ! "$SKIP_SYSTEM_INSTALLS" = true ]]; then
    $SUDO mv "etcd-v${ETCD_VERSION}-linux-amd64/etcd"* /usr/local/bin/
    ETCD_INSTALL_DIR="/usr/local/bin"
  else
    USER_BIN="$HOME/.local/bin"
    mkdir -p "$USER_BIN"
    mv "etcd-v${ETCD_VERSION}-linux-amd64/etcd"* "$USER_BIN/"
    ETCD_INSTALL_DIR="$USER_BIN"
    echo "Installed etcd binaries to $ETCD_INSTALL_DIR (add to PATH if needed)"
  fi
  popd >/dev/null
  rm -rf "$TMPDIR"
  echo "etcd installed to $ETCD_INSTALL_DIR"
fi

# Install Python gRPC tools (user install)
echo "==> Installing Python grpc tools (grpcio, grpcio-tools) to user site"
python3 -m pip install --user grpcio grpcio-tools || python3 -m pip install grpcio grpcio-tools

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
  if $SKIP_SYSTEM_INSTALLS; then
    echo "Postgres initialization was requested but this session doesn't have root/sudo access. Skipping system Postgres init."
    echo "To initialize Postgres, re-run the script with sudo or run the following commands as root or using your OS package manager:"
    cat <<EOF
# On Debian/Ubuntu (run as root or with sudo)
apt-get install -y postgresql postgresql-client
# Initialize cluster (if needed), then set up database/user:
su - postgres -c "/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start"
su - postgres -c "psql -c \"ALTER USER postgres PASSWORD 'postgres';\""
su - postgres -c "psql -c \"CREATE DATABASE goverse;\""
su - postgres -c "psql -c \"CREATE USER goverse WITH PASSWORD 'goverse';\""
su - postgres -c "psql -c \"GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;\""
su - postgres -c "/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop"
EOF
  else
    echo "==> PostgreSQL initialization requested"

    PG_DATA="/var/lib/postgresql/data"
    PG_BIN_DIR="$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | head -n1 || true)"
    if [[ -z "$PG_BIN_DIR" ]]; then
      echo "Unable to find /usr/lib/postgresql/*/bin. Postgres installation may differ on your system."
    else
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
    fi
  fi
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
