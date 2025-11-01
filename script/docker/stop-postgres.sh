#!/bin/bash
# Stop PostgreSQL inside Docker container
# This script is meant to be run inside the goverse-dev container
# This script is reentrant - it can be run multiple times safely

set -euo pipefail

echo "Stopping PostgreSQL..."

# Check if PostgreSQL is running
if ! pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
    echo "✓ PostgreSQL is not running"
    exit 0
fi

# Stop PostgreSQL using pg_ctl
if su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop -m fast' > /tmp/postgres-stop.log 2>&1; then
    echo "Sent termination signal to PostgreSQL"
    
    # Wait for PostgreSQL to stop (up to 30 seconds)
    for _ in {1..30}; do
        if ! pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
            echo "✓ PostgreSQL stopped successfully"
            exit 0
        fi
        sleep 1
    done
    
    # If still running after 30 seconds, try immediate shutdown
    if pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
        echo "PostgreSQL did not stop gracefully, forcing immediate shutdown..."
        su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop -m immediate' > /tmp/postgres-stop.log 2>&1 || true
        sleep 2
    fi
else
    # pg_ctl stop might fail if already stopped, which is fine
    if ! pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
        echo "✓ PostgreSQL is not running"
        exit 0
    fi
fi

# Final check
if pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
    echo "✗ Failed to stop PostgreSQL"
    echo "PostgreSQL stop logs:"
    cat /tmp/postgres-stop.log 2>/dev/null || true
    exit 1
else
    echo "✓ PostgreSQL stopped successfully"
    exit 0
fi
