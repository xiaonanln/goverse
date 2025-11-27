#!/bin/bash
# Generate Python protobuf code for TicTacToe

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROTO_FILE="$SCRIPT_DIR/tictactoe.proto"
OUTPUT_DIR="$SCRIPT_DIR/python"

echo "Generating Python protobuf code..."
echo "  Proto file: $PROTO_FILE"
echo "  Output dir: $OUTPUT_DIR"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Generate Python code
protoc --python_out="$OUTPUT_DIR" --proto_path="$SCRIPT_DIR" "$PROTO_FILE"

# Create __init__.py to make it a package
touch "$OUTPUT_DIR/__init__.py"

echo "✅ Python protobuf code generated successfully!"
echo "   Output: $OUTPUT_DIR/tictactoe_pb2.py"
