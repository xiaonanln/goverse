#!/bin/bash
set -euo pipefail

# Proto files to compile (samples have their own compile-proto.sh scripts)
PROTO_FILES=(
    "proto/goverse.proto"
    "client/proto/gate.proto"
    "inspector/proto/inspector.proto"
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
    # Ensure proto directories are Python packages
    touch proto/__init__.py
    touch inspector/proto/__init__.py
    touch client/proto/__init__.py
    
    # Generate Python proto files from goverse.proto
    if [[ -f "proto/goverse.proto" ]]; then
        if python3 -m grpc_tools.protoc \
            -I. \
            --python_out=. \
            --grpc_python_out=. \
            proto/goverse.proto 2>/dev/null; then
            echo "  ✅ Python proto files generated for proto/goverse.proto"
        else
            echo "  Note: Python grpcio-tools not available, skipping Python proto generation"
            echo "        Install with: pip install grpcio-tools"
        fi
    fi
    
    # Generate Python proto files from inspector/proto/inspector.proto
    if [[ -f "inspector/proto/inspector.proto" ]]; then
        if python3 -m grpc_tools.protoc \
            -I. \
            --python_out=. \
            --grpc_python_out=. \
            inspector/proto/inspector.proto 2>/dev/null; then
            echo "  ✅ Python proto files generated for inspector/proto/inspector.proto"
        else
            echo "  Note: Failed to generate Python proto files for inspector.proto"
        fi
    fi
    
    # Generate Python proto files from client/proto/gate.proto
    if [[ -f "client/proto/gate.proto" ]]; then
        if python3 -m grpc_tools.protoc \
            -I. \
            --python_out=. \
            --grpc_python_out=. \
            client/proto/gate.proto 2>/dev/null; then
            echo "  ✅ Python proto files generated for client/proto/gate.proto"
        else
            echo "  Note: Failed to generate Python proto files for gate.proto"
        fi
    fi
else
    echo "  Note: python3 not found, skipping Python proto generation"
fi

echo "Proto compilation completed."
