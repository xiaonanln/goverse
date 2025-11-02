# Goverse Docker Development Environment

This directory contains Docker files for the Goverse development environment.

## Dockerfile.dev

The `Dockerfile.dev` provides a complete development environment with all dependencies pre-installed, including:

- Go 1.25
- Protocol Buffers compiler and Go plugins
- etcd (distributed key-value store)
- PostgreSQL (relational database for persistence)
- Python 3 and required packages for testing

### Building the Image

```bash
docker build -f docker/Dockerfile.dev -t goverse-dev .
```

### Running the Container

```bash
docker run -it --rm goverse-dev
```

### Automatic Service Management

The container uses an entrypoint script that **automatically starts etcd and PostgreSQL** when the container starts. The entrypoint is reentrant, meaning:

- It checks if services are already running before attempting to start them
- Multiple commands can be run without restarting services unnecessarily
- Services start in the background and remain available throughout the container's lifetime

You don't need to manually start etcd or PostgreSQL - they're ready to use immediately.

To run a specific command with services auto-started:

```bash
docker run -it --rm goverse-dev go test ./...
```

## PostgreSQL Setup

PostgreSQL is pre-installed and configured in the development container with the following defaults:

- **Database**: `goverse`
- **User**: `goverse`
- **Password**: `goverse`
- **postgres user password**: `postgres`
- **Data directory**: `/var/lib/postgresql/data`

### Starting PostgreSQL Manually

PostgreSQL is **automatically started** by the entrypoint script. If you need to manually control it:

```bash
# Inside the container
su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data start'
```

### Stopping PostgreSQL

```bash
# Inside the container
su - postgres -c '/usr/lib/postgresql/*/bin/pg_ctl -D /var/lib/postgresql/data stop'
```

### Using PostgreSQL with Goverse

The database and user are already created, so you can immediately run the persistence example:

```bash
# Inside the container
cd /app/examples/persistence
go run main.go
```

### Verifying PostgreSQL Setup

A test script is provided to verify that PostgreSQL is correctly configured:

```bash
# Inside the container
/app/docker/test-postgres.sh
```

This script will:
- Verify PostgreSQL is installed
- Start the PostgreSQL server
- Test database and user creation
- Verify connection and permissions
- Test table creation and data insertion
- Stop the PostgreSQL server

### Connecting to PostgreSQL

You can connect to the database using psql:

```bash
# As the goverse user
psql -h localhost -U goverse -d goverse

# As the postgres superuser
su - postgres -c "psql"
```

### PostgreSQL Configuration

The PostgreSQL instance is configured to:
- Listen on all interfaces (`0.0.0.0`)
- Use MD5 password authentication
- Accept connections from any IP address

This configuration is suitable for development but should be hardened for production use.

For more information about using PostgreSQL with Goverse objects, see [docs/postgres-setup.md](../docs/postgres-setup.md).

## etcd Setup

etcd is pre-installed for cluster coordination and is **automatically started** by the entrypoint script.

To manually start etcd (if needed):

```bash
# Inside the container
etcd &
```

## Development Workflow

1. Build the Docker image:
   ```bash
   docker build -f docker/Dockerfile.dev -t goverse-dev .
   ```

2. Run the container:
   ```bash
   docker run -it --rm -v $(pwd):/app goverse-dev
   ```

3. Inside the container, you can:
   - Run tests: `go test ./...`
   - Build: `go build ./...`
   - Start PostgreSQL and run persistence examples
   - Start etcd and run cluster examples

## Notes

- The container includes all Go dependencies pre-downloaded
- Protocol buffers are pre-compiled
- All shell scripts in `/app/script` are executable
- The working directory is set to `/app`
