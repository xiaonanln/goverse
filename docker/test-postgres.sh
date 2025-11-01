#!/bin/bash
# Test script to verify PostgreSQL setup in Docker container
# This script should be run inside the Goverse Docker development container

set -e  # Exit on error

echo "=== Testing PostgreSQL Setup in Docker Container ==="
echo ""

# Check if PostgreSQL binaries are available
echo "1. Checking PostgreSQL installation..."
if ! command -v psql &> /dev/null; then
    echo "❌ psql command not found"
    exit 1
fi
echo "✓ PostgreSQL client is installed"

if ! ls /usr/lib/postgresql/*/bin/pg_ctl &> /dev/null; then
    echo "❌ pg_ctl command not found"
    exit 1
fi
echo "✓ PostgreSQL server tools are installed"

# Check if data directory exists
echo ""
echo "2. Checking PostgreSQL data directory..."
if [ ! -d "/var/lib/postgresql/data" ]; then
    echo "❌ PostgreSQL data directory not found at /var/lib/postgresql/data"
    exit 1
fi
echo "✓ PostgreSQL data directory exists"

# Start PostgreSQL
echo ""
echo "3. Starting PostgreSQL server..."
su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start' > /dev/null 2>&1
sleep 2

# Check if PostgreSQL is running
if ! pg_isready -h localhost -p 5432 -U postgres > /dev/null 2>&1; then
    echo "❌ PostgreSQL server is not responding"
    exit 1
fi
echo "✓ PostgreSQL server is running"

# Test connection as postgres user
echo ""
echo "4. Testing connection as postgres user..."
if ! su - postgres -c "psql -c 'SELECT version();'" > /dev/null 2>&1; then
    echo "❌ Cannot connect as postgres user"
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
    exit 1
fi
echo "✓ Can connect as postgres user"

# Check if goverse database exists
echo ""
echo "5. Checking if goverse database exists..."
DB_EXISTS=$(su - postgres -c "psql -lqt | cut -d \| -f 1 | grep -w goverse" || echo "")
if [ -z "$DB_EXISTS" ]; then
    echo "❌ goverse database not found"
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
    exit 1
fi
echo "✓ goverse database exists"

# Check if goverse user exists
echo ""
echo "6. Checking if goverse user exists..."
USER_EXISTS=$(su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='goverse'\"" || echo "")
if [ "$USER_EXISTS" != "1" ]; then
    echo "❌ goverse user not found"
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
    exit 1
fi
echo "✓ goverse user exists"

# Test connection as goverse user
echo ""
echo "7. Testing connection as goverse user..."
if ! PGPASSWORD=goverse psql -h localhost -U goverse -d goverse -c 'SELECT 1;' > /dev/null 2>&1; then
    echo "❌ Cannot connect as goverse user"
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
    exit 1
fi
echo "✓ Can connect as goverse user with password authentication"

# Test creating a table
echo ""
echo "8. Testing table creation and data insertion..."
if ! PGPASSWORD=goverse psql -h localhost -U goverse -d goverse -c '
    CREATE TABLE IF NOT EXISTS test_table (id SERIAL PRIMARY KEY, name TEXT);
    INSERT INTO test_table (name) VALUES ('\''test'\'');
    SELECT COUNT(*) FROM test_table;
    DROP TABLE test_table;
' > /dev/null 2>&1; then
    echo "❌ Cannot create table and insert data"
    su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
    exit 1
fi
echo "✓ Can create tables and insert data"

# Stop PostgreSQL
echo ""
echo "9. Stopping PostgreSQL server..."
su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop' > /dev/null 2>&1
sleep 1
echo "✓ PostgreSQL server stopped"

echo ""
echo "=== All PostgreSQL tests passed! ==="
echo ""
echo "PostgreSQL is ready to use with:"
echo "  Database: goverse"
echo "  User: goverse"
echo "  Password: goverse"
echo ""
echo "To start PostgreSQL:"
echo "  su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start'"
echo ""
echo "To run the persistence example:"
echo "  cd /app/examples/persistence && go run main.go"
