#!/bin/bash
# Run Go unit tests inside Docker container
# This script is meant to be run inside the goverse-dev container

set -euo pipefail

echo "========================================"
echo "Starting Go Unit Tests"
echo "========================================"
echo

./script/compile-proto.sh

echo
echo "Running Go unit tests..."
echo

# run all go tests (no caching) and fail fast on errors
if ! go test ./... -p 1 -count=1 -v -failfast; then
    echo "✗ Go unit tests failed"
    exit 1
fi

echo "✓ Go unit tests passed"

# Clean up etcd data directory after tests
echo
echo "Cleaning up etcd data directory..."
if [ -d "/app/default.etcd" ]; then
    rm -rf /app/default.etcd || true
    echo "Removed /app/default.etcd"
fi
