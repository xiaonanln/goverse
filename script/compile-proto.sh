#!/bin/bash
set -euo pipefail

# Proto files to compile
PROTO_FILES=(
    "proto/pulse.proto"
    "client/proto/client.proto"
    "inspector/proto/inspector.proto"
    "samples/chat/proto/chat.proto"
)

# Common protoc options
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

echo "Proto compilation completed."
