#!/bin/bash
set -euo pipefail

# Proto files to compile
PROTO_FILES=(
    "proto/goverse.proto"
    "client/proto/client.proto"
    "inspector/proto/inspector.proto"
    "samples/chat/proto/chat.proto"
)

# Common protoc options for Go
PROTOC_OPTS="--go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative"

echo "Compiling proto files..."

for proto_file in "${PROTO_FILES[@]}"; do
    if [[ -f "$proto_file" ]]; then
        echo "  Processing $proto_file"
        protoc $PROTOC_OPTS "$proto_file"
    else
        echo "  Warning: $proto_file not found, skipping"
    fi
done

# Generate Python proto files for integration tests
echo "Generating Python proto files..."
if command -v python3 &> /dev/null; then
    # Ensure proto directory is a Python package
    touch proto/__init__.py
    
    # Generate Python proto files from goverse.proto
    if [[ -f "proto/goverse.proto" ]]; then
        if python3 -m grpc_tools.protoc \
            -I. \
            --python_out=. \
            --grpc_python_out=. \
            proto/goverse.proto 2>&1; then
            echo "  ✅ Python proto files generated successfully"
        else
            echo "  Note: Python grpcio-tools not available, skipping Python proto generation"
            echo "        Install with: pip install grpcio-tools"
        fi
    fi
else
    echo "  Note: python3 not found, skipping Python proto generation"
fi

echo "Proto compilation completed."
