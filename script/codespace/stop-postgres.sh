#!/bin/bash
# Stop PostgreSQL for GitHub Codespaces
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Stopping PostgreSQL..."

# Check if PostgreSQL is running
if ! pg_isready -q 2>/dev/null; then
    echo "✓ PostgreSQL is not running"
    exit 0
fi

# Try to stop PostgreSQL using service command
if command -v service >/dev/null 2>&1; then
    echo "Stopping PostgreSQL via service..."
    sudo service postgresql stop
elif command -v pg_ctlcluster >/dev/null 2>&1; then
    echo "Stopping PostgreSQL via pg_ctlcluster..."
    CLUSTER_INFO=$(pg_lsclusters -h | head -1)
    if [[ -n "$CLUSTER_INFO" ]]; then
        VERSION=$(echo "$CLUSTER_INFO" | awk '{print $1}')
        CLUSTER=$(echo "$CLUSTER_INFO" | awk '{print $2}')
        sudo pg_ctlcluster "$VERSION" "$CLUSTER" stop
    else
        echo "✗ No PostgreSQL clusters found"
        exit 1
    fi
else
    echo "✗ Unable to find PostgreSQL control commands"
    exit 1
fi

# Wait for PostgreSQL to stop
echo "Waiting for PostgreSQL to stop..."
for i in {1..10}; do
    if ! pg_isready -q 2>/dev/null; then
        echo "✓ PostgreSQL stopped successfully"
        exit 0
    fi
    sleep 1
done

echo "✗ PostgreSQL did not stop within 10 seconds"
exit 1
