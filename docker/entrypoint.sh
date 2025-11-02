#!/bin/bash
# Docker entrypoint script for Goverse development container
# This script ensures etcd and PostgreSQL services are running before executing commands
# It is reentrant - checks if services are already running before attempting to start them

set -e

# Function to check if etcd is running
is_etcd_running() {
    if curl -s http://localhost:2379/health > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Function to check if PostgreSQL is running
is_postgres_running() {
    if pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Function to start etcd
start_etcd() {
    echo "Starting etcd..."
    
    # Clean up any stale etcd data directory to ensure a fresh start
    if [ -d "/app/default.etcd" ]; then
        echo "Removing stale etcd data directory at /app/default.etcd"
        if ! rm -rf /app/default.etcd 2>/dev/null; then
            echo "Warning: Could not remove stale etcd data directory"
        fi
    fi
    
    # Start etcd in background
    # Note: Listens on 0.0.0.0 for development convenience (consistent with script/docker/start-etcd.sh)
    etcd \
      --listen-client-urls http://0.0.0.0:2379 \
      --advertise-client-urls http://0.0.0.0:2379 \
      > /tmp/etcd.log 2>&1 &
    
    ETCD_PID=$!
    echo "etcd started with PID: $ETCD_PID"
    
    # Wait for etcd to be ready
    echo "Waiting for etcd to be ready..."
    for i in {1..30}; do
        if is_etcd_running; then
            echo "✓ etcd is ready"
            return 0
        fi
        sleep 1
    done
    
    echo "✗ etcd failed to start within 30 seconds"
    echo "etcd logs:"
    cat /tmp/etcd.log
    return 1
}

# Function to start PostgreSQL
start_postgres() {
    echo "Starting PostgreSQL..."
    
    # Start PostgreSQL in background
    # Note: Uses wildcard for version-independent path (consistent with script/docker/start-postgres.sh)
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start' > /tmp/postgres.log 2>&1
    
    # Wait for PostgreSQL to be ready
    echo "Waiting for PostgreSQL to be ready..."
    for i in {1..30}; do
        if is_postgres_running; then
            echo "✓ PostgreSQL is ready"
            return 0
        fi
        sleep 1
    done
    
    echo "✗ PostgreSQL failed to start within 30 seconds"
    echo "PostgreSQL logs:"
    cat /tmp/postgres.log
    return 1
}

# Main entrypoint logic
echo "========================================"
echo "Goverse Docker Entrypoint"
echo "========================================"
echo ""

# Check and start etcd if needed
if is_etcd_running; then
    echo "✓ etcd is already running"
else
    start_etcd
fi

echo ""

# Check and start PostgreSQL if needed
if is_postgres_running; then
    echo "✓ PostgreSQL is already running"
else
    start_postgres
fi

echo ""
echo "========================================"
echo "Services are ready"
echo "========================================"
echo ""

# Execute the provided command or default to bash
if [ "$#" -eq 0 ]; then
    exec bash
else
    exec "$@"
fi
