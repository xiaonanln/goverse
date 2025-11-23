#!/bin/bash
# Run Go unit tests inside Docker container
# This script is meant to be run inside the xiaonanln/goverse:dev container
# Usage: ./script/docker/test-go.sh [-race] [-etcd-restart] [-etcd-restart-only]

set -euo pipefail

# Parse command line arguments
RACE_FLAG=""
ETCD_RESTART=false
ETCD_RESTART_ONLY=false
for arg in "$@"; do
    case $arg in
        -race)
            RACE_FLAG="-race"
            echo "Race detector enabled"
            ;;
        -etcd-restart)
            ETCD_RESTART=true
            echo "Etcd restart tests enabled"
            ;;
        -etcd-restart-only)
            ETCD_RESTART_ONLY=true
            echo "Running only etcd restart tests"
            ;;
        *)
            echo "Unknown option: $arg"
            echo "Usage: ./script/docker/test-go.sh [-race] [-etcd-restart] [-etcd-restart-only]"
            exit 1
            ;;
    esac
done

echo "========================================"
echo "Starting Go Unit Tests"
echo "========================================"
echo

./script/compile-proto.sh

if [ "$ETCD_RESTART_ONLY" = false ]; then
    echo
    echo "Running Go unit tests..."
    echo

    # run all go tests (no caching) and fail fast on errors
    if ! go test ./... -p 1 -parallel 1 -count=1 -v -failfast $RACE_FLAG; then
        echo "✗ Go unit tests failed"
        exit 1
    fi

    echo "✓ Go unit tests passed"
fi

if [ "$ETCD_RESTART" = true ] || [ "$ETCD_RESTART_ONLY" = true ]; then
    echo
    echo "Running etcd restart tests..."
    echo
    if ! go test -tags=etcd_restart -v -p 1 -parallel 1 -run=^.*Reconnection$ $RACE_FLAG ./...; then
        echo "✗ Etcd restart tests failed"
        exit 1
    fi
    echo "✓ Etcd restart tests passed"
fi

# Clean up etcd data directory after tests
echo
echo "Cleaning up etcd data directory..."
if [ -d "/app/default.etcd" ]; then
    rm -rf /app/default.etcd || true
    echo "Removed /app/default.etcd"
fi
