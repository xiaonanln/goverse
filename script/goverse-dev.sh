#!/usr/bin/env bash
# Wrapper script to run commands in the xiaonanln/goverse:dev Docker container
# Usage: ./script/goverse-dev.sh <command> [args...]
# Example: ./script/goverse-dev.sh go test ./cluster

set -euo pipefail

# Determine the project root directory (parent of script directory)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Docker image name
IMAGE_NAME="xiaonanln/goverse:dev"

# Check if Docker is available
if ! command -v docker >/dev/null 2>&1; then
    echo "Error: docker is not installed or not in PATH" >&2
    exit 1
fi

# Check if the image exists
if ! docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
    echo "Error: Docker image '$IMAGE_NAME' not found" >&2
    echo "Please pull it first with:" >&2
    echo "  docker pull xiaonanln/goverse:dev" >&2
    echo "Or build it locally with:" >&2
    echo "  docker build -f docker/Dockerfile.dev -t xiaonanln/goverse:dev ." >&2
    exit 1
fi

# If no arguments provided, show usage
if [ $# -eq 0 ]; then
    cat <<EOF
Usage: $0 <command> [args...]

Runs commands inside the xiaonanln/goverse:dev Docker container with the current directory mounted.

Examples:
  $0 go test ./cluster
  $0 go build ./...
  $0 bash
  $0 go run examples/persistence/main.go

The container will automatically start etcd and PostgreSQL in the background.
EOF
    exit 0
fi

# Run the command in Docker with current directory mounted
# Use -it for interactive terminal if stdin is a terminal
# Use --rm to automatically remove the container when it exits
if [ -t 0 ]; then
    # Interactive mode (stdin is a terminal)
    exec docker run -it --rm \
        -v "$PROJECT_ROOT:/app" \
        -w /app \
        "$IMAGE_NAME" \
        "$@"
else
    # Non-interactive mode (stdin is not a terminal, e.g., piped input)
    exec docker run --rm \
        -v "$PROJECT_ROOT:/app" \
        -w /app \
        "$IMAGE_NAME" \
        "$@"
fi
