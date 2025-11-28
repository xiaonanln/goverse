#!/bin/bash
# Run all integration tests inside Docker container
# This script is meant to be run inside the xiaonanln/goverse:dev container

set -euo pipefail

echo "========================================"
echo "Starting Integration Tests"
echo "========================================"
echo

./script/compile-proto.sh

echo
echo "Running Go unit tests..."
echo

# run all go tests (no caching) and fail fast on errors
if ! go test -v -p 1 -parallel 1 ./... -count=1; then
    echo "✗ Go unit tests failed"
    exit 1
fi

echo "✓ Go unit tests passed"

echo
echo "Running etcd restart tests..."
echo
if ! go test -tags=etcd_restart -v -p 1 -parallel 1 -run=^.*Reconnection$ ./...; then
    echo "✗ Etcd restart tests failed"
    exit 1
fi
echo "✓ Etcd restart tests passed"echo
echo "Running integration tests..."
echo

# Run the tests with coverage enabled
export ENABLE_COVERAGE=true
export GOCOVERDIR=/tmp/coverage

# Create coverage directory
mkdir -p "$GOCOVERDIR"

# Run the counter sample test first
TEST_EXIT_CODE=0
echo "Running counter sample tests..."
if ! python3 tests/samples/counter/test_counter.py; then
    TEST_EXIT_CODE=$?
    echo
    echo "========================================"
    echo "✗ Counter sample tests failed with exit code $TEST_EXIT_CODE"
    echo "========================================"
    # Clean up and exit early
    if [ -d "/app/default.etcd" ]; then
        rm -rf /app/default.etcd || true
    fi
    exit $TEST_EXIT_CODE
fi
echo "✓ Counter sample tests passed"
echo

# Run the chat sample test
echo "Running chat sample tests..."
if python3 tests/samples/chat/test_chat.py "$@"; then
    echo
    echo "========================================"
    echo "✓ All tests passed"
    echo "========================================"
else
    TEST_EXIT_CODE=$?
    echo
    echo "========================================"
    echo "✗ Chat sample tests failed with exit code $TEST_EXIT_CODE"
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
