#!/bin/bash
# Start PostgreSQL for testing inside Docker container
# This script is meant to be run inside the goverse-dev container
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Starting PostgreSQL..."

# Check if PostgreSQL is already running
if pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
    echo "✓ PostgreSQL is already running"
    exit 0
fi

# Start PostgreSQL in background
su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start' > /tmp/postgres.log 2>&1

# Wait for PostgreSQL to be ready
echo "Waiting for PostgreSQL to be ready..."
for _ in {1..30}; do
    if pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
        echo "✓ PostgreSQL is ready"
        exit 0
    fi
    sleep 1
done

echo "✗ PostgreSQL failed to start within 30 seconds"
echo "PostgreSQL logs:"
cat /tmp/postgres.log
exit 1
