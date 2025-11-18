#!/bin/bash
# Start PostgreSQL for GitHub Codespaces
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Starting PostgreSQL..."

# Check if PostgreSQL is already running
if pg_isready -q 2>/dev/null; then
    echo "✓ PostgreSQL is already running and healthy"
    exit 0
fi

# Try to start PostgreSQL using service command
if command -v service >/dev/null 2>&1; then
    echo "Starting PostgreSQL via service..."
    sudo service postgresql start
elif command -v pg_ctlcluster >/dev/null 2>&1; then
    echo "Starting PostgreSQL via pg_ctlcluster..."
    CLUSTER_INFO=$(pg_lsclusters -h | head -1)
    if [[ -n "$CLUSTER_INFO" ]]; then
        VERSION=$(echo "$CLUSTER_INFO" | awk '{print $1}')
        CLUSTER=$(echo "$CLUSTER_INFO" | awk '{print $2}')
        sudo pg_ctlcluster "$VERSION" "$CLUSTER" start
    else
        echo "✗ No PostgreSQL clusters found"
        exit 1
    fi
else
    echo "✗ Unable to find PostgreSQL control commands"
    exit 1
fi

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to be ready..."
for i in {1..30}; do
    if pg_isready -q 2>/dev/null; then
        echo "✓ PostgreSQL is ready"
        exit 0
    fi
    sleep 1
done

echo "✗ PostgreSQL failed to start within 30 seconds"
exit 1
