#!/bin/bash
# Run Go unit tests inside Docker container
# This script is meant to be run inside the goverse-dev container

set -euo pipefail

echo "========================================"
echo "Starting Go Unit Tests"
echo "========================================"
echo

# Clean up any stale etcd data directory to ensure a fresh start
if [ -d "/app/default.etcd" ]; then
    echo "Removing stale etcd data directory at /app/default.etcd"
    rm -rf /app/default.etcd || true
fi

# Start etcd
if ! bash /app/script/docker/start-etcd.sh; then
    echo "✗ Failed to start etcd"
    exit 1
fi

echo 
echo "Running Go unit tests..."
echo 

# run all go tests (no caching) and fail fast on errors
if ! go test ./... -count=1; then
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
