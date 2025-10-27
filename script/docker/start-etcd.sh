#!/bin/bash
# Start etcd for testing inside Docker container
# This script is meant to be run inside the goverse-dev container

set -euo pipefail

echo "Starting etcd..."

# Start etcd in background
etcd \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://0.0.0.0:2379 \
  > /tmp/etcd.log 2>&1 &

ETCD_PID=$!
echo "etcd started with PID: $ETCD_PID"

# Wait for etcd to be ready
echo "Waiting for etcd to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:2379/health > /dev/null 2>&1; then
        echo "✓ etcd is ready"
        exit 0
    fi
    sleep 1
done

echo "✗ etcd failed to start within 30 seconds"
echo "etcd logs:"
cat /tmp/etcd.log
exit 1
