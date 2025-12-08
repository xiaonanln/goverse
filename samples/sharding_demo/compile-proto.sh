#!/bin/bash
# Compile proto files for the sharding_demo sample
set -euo pipefail

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Common protoc options for Go
PROTOC_OPTS="--go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative"

echo "Compiling sharding_demo proto files..."

if [[ -f "proto/sharding_demo.proto" ]]; then
    echo "  Processing proto/sharding_demo.proto"
    protoc $PROTOC_OPTS "proto/sharding_demo.proto"
else
    echo "  Error: proto/sharding_demo.proto not found"
    exit 1
fi

# Generate Python proto files if python3 is available
if command -v python3 &> /dev/null; then
    # Ensure proto directory is a Python package
    touch proto/__init__.py
    
    if python3 -m grpc_tools.protoc \
        -I. \
        --python_out=. \
        --grpc_python_out=. \
        proto/sharding_demo.proto 2>&1; then
        echo "  ✅ Python proto files generated for proto/sharding_demo.proto"
    else
        echo "  Note: Python grpcio-tools not available, skipping Python proto generation"
        echo "        Install with: pip install grpcio-tools"
    fi
else
    echo "  Note: python3 not found, skipping Python proto generation"
fi

echo "Sharding demo proto compilation completed."
