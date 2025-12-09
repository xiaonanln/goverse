# pgadmin - PostgreSQL Database Management Tool for GoVerse

The `pgadmin` tool provides command-line utilities for managing PostgreSQL databases used by GoVerse for object persistence.

## Features

- **init**: Initialize database schema (create tables and indexes)
- **verify**: Verify database connection and schema integrity
- **reset**: Drop and recreate database schema (with confirmation prompt)
- **status**: Show database status and statistics

## Installation

```bash
go install github.com/xiaonanln/goverse/cmd/pgadmin@latest
```

Or build from source:

```bash
cd cmd/pgadmin
go build
```

## Usage

### Using Configuration File

If you have a GoVerse configuration file with PostgreSQL settings:

```bash
# Initialize schema
pgadmin --config config.yml init

# Verify connection and schema
pgadmin --config config.yml verify

# Show database status
pgadmin --config config.yml status

# Reset schema (will prompt for confirmation)
pgadmin --config config.yml reset
```

### Using Command Line Flags

You can also specify connection parameters directly:

```bash
# Initialize schema
pgadmin --host localhost --port 5432 --user goverse \
        --password goverse --database goverse init

# Verify connection
pgadmin --host localhost --user goverse --password goverse \
        --database goverse verify

# Show status
pgadmin --host localhost --user goverse --password goverse \
        --database goverse status
```

## Commands

### init

Initializes the database schema by creating all required tables and indexes:
- `goverse_objects` - Stores persistent object state as JSONB
- `goverse_requests` - Tracks request state for exactly-once semantics
- Indexes for efficient queries
- Triggers for automatic timestamp updates

```bash
pgadmin --config config.yml init
```

Output example:
```
Initializing PostgreSQL schema...
  Host: localhost:5432
  Database: goverse
  User: goverse
✓ Connected to database
✓ Schema initialized successfully
✓ Table 'goverse_objects' created
✓ Table 'goverse_requests' created

Database schema initialized successfully!
```

### verify

Verifies that the database connection works and all required tables and indexes exist:

```bash
pgadmin --config config.yml verify
```

Output example:
```
Verifying PostgreSQL database...
  Host: localhost:5432
  Database: goverse
✓ Connection successful
✓ Table 'goverse_objects' exists
✓ Table 'goverse_requests' exists
✓ Index 'idx_goverse_objects_type' exists
✓ Index 'idx_goverse_objects_updated_at' exists
✓ Index 'idx_goverse_requests_object_status' exists

Database verification complete!
```

### status

Shows comprehensive database status including:
- Connection status and latency
- PostgreSQL version
- Table existence and row counts
- Database size

```bash
pgadmin --config config.yml status
```

Output example:
```
PostgreSQL Database Status
==========================
Host:     localhost:5432
Database: goverse
User:     goverse

Connection: ✓ (latency: 2ms)
Version:    PostgreSQL 16.0 on x86_64-pc-linux-gnu, compiled by gcc...

Tables:
-------
  goverse_objects: ✓ (1234 rows)
  goverse_requests: ✓ (567 rows)

Database Size: 8192 kB
```

### reset

Drops and recreates the database schema. **WARNING: This deletes all data!**

The command will prompt for confirmation before proceeding:

```bash
pgadmin --config config.yml reset
```

Output example:
```
WARNING: This will delete all data in the database!
Are you sure you want to continue? (yes/no): yes

Resetting PostgreSQL schema...
  Host: localhost:5432
  Database: goverse
✓ Dropped existing tables
✓ Schema recreated successfully

Database schema reset complete!
```

## Configuration File Format

The tool can read PostgreSQL configuration from a GoVerse YAML config file:

```yaml
postgres:
  host: localhost
  port: 5432
  user: goverse
  password: goverse
  database: goverse
  sslmode: disable  # Use "require" in production
```

## Command Line Options

```
  -config string
        Path to YAML configuration file
  -database string
        PostgreSQL database (default "goverse")
  -host string
        PostgreSQL host (default "localhost")
  -password string
        PostgreSQL password (default "goverse")
  -port int
        PostgreSQL port (default 5432)
  -sslmode string
        PostgreSQL SSL mode (default "disable")
  -user string
        PostgreSQL user (default "goverse")
```

## Database Schema

The tool manages the following schema:

### goverse_objects Table

Stores persistent object state using JSONB:

```sql
CREATE TABLE goverse_objects (
    object_id VARCHAR(255) PRIMARY KEY,
    object_type VARCHAR(255) NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_goverse_objects_type ON goverse_objects(object_type);
CREATE INDEX idx_goverse_objects_updated_at ON goverse_objects(updated_at);
```

### goverse_requests Table

Tracks request state for exactly-once semantics in inter-object calls:

```sql
CREATE TABLE goverse_requests (
    request_id VARCHAR(255) PRIMARY KEY,
    object_id VARCHAR(255) NOT NULL,
    object_type VARCHAR(255) NOT NULL,
    method_name VARCHAR(255) NOT NULL,
    request_data BYTEA NOT NULL,
    result_data BYTEA,
    error_message TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_goverse_requests_object_status 
    ON goverse_requests(object_id, status);
```

## Examples

### Quick Setup for Development

```bash
# 1. Start PostgreSQL (if not running)
docker run -d --name goverse-postgres \
  -e POSTGRES_USER=goverse \
  -e POSTGRES_PASSWORD=goverse \
  -e POSTGRES_DB=goverse \
  -p 5432:5432 \
  postgres:16

# 2. Initialize schema
pgadmin --host localhost --user goverse --password goverse \
        --database goverse init

# 3. Verify setup
pgadmin --host localhost --user goverse --password goverse \
        --database goverse status
```

### Using with Environment Variables

You can avoid exposing passwords in command line by using a config file with environment variable references:

```yaml
postgres:
  host: ${DB_HOST:-localhost}
  port: ${DB_PORT:-5432}
  user: ${DB_USER:-goverse}
  password: ${DB_PASSWORD}
  database: ${DB_NAME:-goverse}
  sslmode: require
```

Then:
```bash
export DB_PASSWORD=my-secret-password
pgadmin --config config.yml init
```

## Troubleshooting

### Connection Refused

If you see `connection refused`:
1. Check that PostgreSQL is running
2. Verify the host and port are correct
3. Check firewall settings

### Authentication Failed

If you see `password authentication failed`:
1. Verify the username and password
2. Check PostgreSQL's `pg_hba.conf` settings
3. Try connecting with `psql` to verify credentials

### Permission Denied

If you see `permission denied`:
1. Verify the user has appropriate permissions
2. Grant permissions: `GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;`

## See Also

- [PostgreSQL Setup Guide](../../docs/POSTGRES_SETUP.md) - Detailed PostgreSQL setup instructions
- [Persistence Example](../../examples/persistence/main.go) - Example of using PostgreSQL with GoVerse objects
- [Configuration Documentation](../../docs/CONFIGURATION.md) - Complete GoVerse configuration reference
