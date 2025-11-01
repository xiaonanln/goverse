#!/bin/bash
# Run chat integration tests inside Docker container
# This script is meant to be run inside the goverse-dev container

set -euo pipefail

echo "========================================"
echo "Starting Chat Integration Tests"
echo "========================================"
echo

./script/compile-proto.sh

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

# Start PostgreSQL
if ! bash /app/script/docker/start-postgres.sh; then
    echo "✗ Failed to start PostgreSQL"
    exit 1
fi

echo
echo "Running Go unit tests..."
echo

# run all go tests (no caching) and fail fast on errors
if ! go test -p 1 -v ./... -count=1; then
    echo "✗ Go unit tests failed"
    exit 1
fi

echo "✓ Go unit tests passed"

echo
echo "Running integration tests..."
echo

# Run the tests with coverage enabled
export ENABLE_COVERAGE=true
export GOCOVERDIR=/tmp/coverage

# Create coverage directory
mkdir -p "$GOCOVERDIR"

# Run the test
TEST_EXIT_CODE=0
if python3 tests/integration/test_chat.py "$@"; then
    echo
    echo "========================================"
    echo "✓ All tests passed"
    echo "========================================"
else
    TEST_EXIT_CODE=$?
    echo
    echo "========================================"
    echo "✗ Tests failed with exit code $TEST_EXIT_CODE"
    echo "========================================"
fi

# Clean up etcd data directory after tests
echo
echo "Cleaning up etcd data directory..."
if [ -d "/app/default.etcd" ]; then
    rm -rf /app/default.etcd || true
    echo "Removed /app/default.etcd"
fi

exit $TEST_EXIT_CODE
