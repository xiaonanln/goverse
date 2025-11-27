#!/bin/bash
# Compile proto files for the tictactoe sample
set -euo pipefail

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Common protoc options for Go
PROTOC_OPTS="--go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative"

echo "Compiling tictactoe proto files..."

if [[ -f "proto/tictactoe.proto" ]]; then
    echo "  Processing proto/tictactoe.proto"
    protoc $PROTOC_OPTS "proto/tictactoe.proto"
else
    echo "  Error: proto/tictactoe.proto not found"
    exit 1
fi

# Generate Python proto files if python3 is available
if command -v python3 &> /dev/null; then
    # Ensure python output directory exists
    mkdir -p proto/python
    touch proto/python/__init__.py
    
    if python3 -m grpc_tools.protoc \
        -Iproto \
        --python_out=proto/python \
        --grpc_python_out=proto/python \
        proto/tictactoe.proto 2>&1; then
        echo "  ✅ Python proto files generated for proto/tictactoe.proto"
    else
        echo "  Note: Python grpcio-tools not available, skipping Python proto generation"
        echo "        Install with: pip install grpcio-tools"
    fi
else
    echo "  Note: python3 not found, skipping Python proto generation"
fi

echo "Tictactoe proto compilation completed."
