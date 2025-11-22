#!/bin/bash
# Stop etcd for GitHub Codespaces
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Stopping etcd..."

# Check if etcd is running
if ! pgrep -x "etcd" > /dev/null; then
    echo "✓ etcd is not running"
    exit 0
fi

# Stop etcd gracefully
echo "Sending SIGTERM to etcd..."
pkill -x "etcd"

# Wait for etcd to stop
echo "Waiting for etcd to stop..."
for i in {1..10}; do
    if ! pgrep -x "etcd" > /dev/null; then
        echo "✓ etcd stopped successfully"
        exit 0
    fi
    sleep 1
done

# Force kill if still running
echo "etcd did not stop gracefully, sending SIGKILL..."
pkill -9 -x "etcd" || true

sleep 1

if ! pgrep -x "etcd" > /dev/null; then
    echo "✓ etcd stopped (forced)"
    exit 0
else
    echo "✗ Failed to stop etcd"
    exit 1
fi
