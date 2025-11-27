#!/bin/bash
# Compile proto files for the counter sample
set -euo pipefail

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Common protoc options for Go
PROTOC_OPTS="--go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative"

echo "Compiling counter proto files..."

if [[ -f "proto/counter.proto" ]]; then
    echo "  Processing proto/counter.proto"
    protoc $PROTOC_OPTS "proto/counter.proto"
else
    echo "  Error: proto/counter.proto not found"
    exit 1
fi

echo "Counter proto compilation completed."
