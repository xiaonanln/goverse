#!/bin/bash

# Compile protocol buffer definitions for handwriting sample

set -e

# Get the directory of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "Compiling handwriting.proto..."
protoc --go_out=. --go_opt=paths=source_relative \
    proto/handwriting.proto

echo "Proto compilation complete!"
