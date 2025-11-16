#!/bin/bash
# Stop etcd inside Docker container
# This script is meant to be run inside the xiaonanln/goverse:dev container
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Stopping etcd..."

# Check if etcd is running
if ! pgrep -x "etcd" > /dev/null; then
    echo "✓ etcd is not running"
    exit 0
fi

# Kill etcd process
if pkill -x "etcd"; then
    echo "Sent termination signal to etcd"
    
    # Wait for etcd to stop (up to 10 seconds)
    for _ in {1..10}; do
        if ! pgrep -x "etcd" > /dev/null; then
            echo "✓ etcd stopped successfully"
            exit 0
        fi
        sleep 1
    done
    
    # Force kill if still running
    if pgrep -x "etcd" > /dev/null; then
        echo "etcd did not stop gracefully, forcing termination..."
        pkill -9 -x "etcd" || true
        sleep 1
    fi
fi

# Final check
if pgrep -x "etcd" > /dev/null; then
    echo "✗ Failed to stop etcd"
    exit 1
else
    echo "✓ etcd stopped successfully"
    exit 0
fi
