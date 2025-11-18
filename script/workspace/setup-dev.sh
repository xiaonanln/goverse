#!/usr/bin/env bash
# Setup development environment for Goverse (idempotent)
# Optimized for GitHub Codespaces but works on any Linux system
#
# Usage:
#   ./script/workspace/setup-dev.sh [--yes] [--postgres] [--etcd-version X.Y.Z]
# Options:
#   --yes            : non-interactive (assume yes for apt)
#   --postgres       : initialize local PostgreSQL cluster and create DB/user (optional)
#   --etcd-version   : etcd version to install (default: 3.5.10)

set -euo pipefail

# If not running as root, re-execute this script as root using sudo su
if [[ "$EUID" -ne 0 ]]; then
  echo "==> Elevating to root privileges..."
  exec sudo su -c "cd '$PWD' && bash $0 $*"
fi

# Defaults (GitHub Codespaces-friendly)
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

# GitHub Codespaces detection
IN_CODESPACE=false
if [[ -n "${CODESPACES:-}" ]] || [[ -n "${GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN:-}" ]]; then
  IN_CODESPACE=true
  echo "==> Detected GitHub Codespaces environment"
fi

APT_ARGS="-y"
if ! $ASSUME_YES; then
  APT_ARGS=""
fi

echo "==> Starting development environment setup (running as root)"
echo "Environment: $(if $IN_CODESPACE; then echo "GitHub Codespaces"; else echo "Local"; fi)"
echo "etcd version: ${ETCD_VERSION}"
echo "postgres init: ${WITH_POSTGRES}"

# Install system packages
if ! command -v apt-get >/dev/null 2>&1; then
  echo "WARNING: apt-get not found. Please install required packages manually."
else
  echo "==> Updating apt and installing system packages"
  apt-get update
  apt-get install $APT_ARGS dos2unix protobuf-compiler git python3 python3-pip wget curl ca-certificates postgresql postgresql-client
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
  # Install to /usr/local/bin
  mv "etcd-v${ETCD_VERSION}-linux-amd64/etcd"* /usr/local/bin/
  ETCD_INSTALL_DIR="/usr/local/bin"
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

# Optional: PostgreSQL initialization
if $WITH_POSTGRES; then
  echo "==> PostgreSQL initialization"
  
  # Start PostgreSQL service
  service postgresql start || pg_ctlcluster $(pg_lsclusters -h | awk '{print $1, $2}' | head -1) start
  
  # Wait for PostgreSQL to be ready
  sleep 2
  
  # Create database and user
  echo "Creating goverse database and user..."
  su - postgres -c "psql -c \"ALTER USER postgres PASSWORD 'postgres';\"" 2>/dev/null || true
  su - postgres -c "psql -c \"CREATE DATABASE goverse;\"" 2>/dev/null || echo "Database goverse already exists"
  su - postgres -c "psql -c \"CREATE USER goverse WITH PASSWORD 'goverse';\"" 2>/dev/null || echo "User goverse already exists"
  su - postgres -c "psql -c \"GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;\"" || true
  su - postgres -c "psql -d goverse -c \"GRANT ALL ON SCHEMA public TO goverse;\"" 2>/dev/null || true
  
  echo "✓ Postgres database 'goverse' and user 'goverse' ready (password: 'goverse')"
else
  echo "Postgres setup was not requested; skipping."
fi

echo ""
echo "==> ✓ Development environment setup complete!"
echo ""
if $IN_CODESPACE; then
  echo "GitHub Codespaces notes:"
  echo "  • etcd installed and ready to use"
  echo "  • PostgreSQL running and configured"
  echo "  • Go protobuf tools installed"
  echo ""
  echo "Quick start:"
  echo "  ./script/compile-proto.sh    # Compile protobuf files"
  echo "  go build ./...                # Build the project"
  echo "  go test -v ./...              # Run tests"
else
  echo "Next steps:"
  echo "  • Ensure Go bin is in PATH: export PATH=\"\$PATH:$(go env GOPATH 2>/dev/null)/bin\""
  echo "  • Start etcd: etcd &"
  echo "  • Start PostgreSQL: service postgresql start"
  echo "  • Run tests: go test ./..."
fi
echo ""
