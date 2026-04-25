#!/bin/bash
# Compile proto files for the bomberman sample
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PROTOC_OPTS="--go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative"

echo "Compiling bomberman proto files..."

if [[ -f "proto/bomberman.proto" ]]; then
    echo "  Processing proto/bomberman.proto"
    protoc $PROTOC_OPTS "proto/bomberman.proto"
else
    echo "  Error: proto/bomberman.proto not found"
    exit 1
fi

if command -v python3 &> /dev/null; then
    touch proto/__init__.py

    if python3 -m grpc_tools.protoc \
        -I. \
        --python_out=. \
        --grpc_python_out=. \
        proto/bomberman.proto 2>&1; then
        echo "  ✅ Python proto files generated for proto/bomberman.proto"
    else
        echo "  Note: Python grpcio-tools not available, skipping Python proto generation"
        echo "        Install with: pip install grpcio-tools"
    fi
else
    echo "  Note: python3 not found, skipping Python proto generation"
fi

echo "Bomberman proto compilation completed."
