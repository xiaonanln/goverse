#!/bin/bash
# Run chat integration tests inside Docker container
# This script is meant to be run inside the goverse-dev container

set -euo pipefail

echo "========================================"
echo "Starting Chat Integration Tests"
echo "========================================"
echo

# Start etcd
if ! bash /app/script/docker/start-etcd.sh; then
    echo "✗ Failed to start etcd"
    exit 1
fi

echo
echo "Running integration tests..."
echo

# Run the tests with coverage enabled
export ENABLE_COVERAGE=true
export GOCOVERDIR=/tmp/coverage

# Create coverage directory
mkdir -p "$GOCOVERDIR"

# Run the test
if python3 tests/integration/test_chat.py "$@"; then
    echo
    echo "========================================"
    echo "✓ All tests passed"
    echo "========================================"
    exit 0
else
    TEST_EXIT_CODE=$?
    echo
    echo "========================================"
    echo "✗ Tests failed with exit code $TEST_EXIT_CODE"
    echo "========================================"
    exit $TEST_EXIT_CODE
fi
