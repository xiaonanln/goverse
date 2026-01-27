# Goverse Docker Development Environment

This directory contains Docker files for the Goverse development environment.

## Dockerfile.dev

The `Dockerfile.dev` provides a complete development environment with all dependencies pre-installed, including:

- Go 1.25
- Protocol Buffers compiler and Go plugins
- etcd (distributed key-value store)
- PostgreSQL (relational database for persistence)
- Python 3 and required packages for testing

### Using the Pre-built Image

The recommended way is to use the pre-built image from Docker Hub:

```bash
docker pull xiaonanln/goverse:dev
docker run -it --rm xiaonanln/goverse:dev
```

### Building the Image Locally

If you need to build the image locally:

```bash
docker build -f docker/Dockerfile.dev -t xiaonanln/goverse:dev .
```

Or use the provided build script:

```bash
./script/docker-build-dev.sh
```

Note: The build script creates a local `goverse-dev` image for testing purposes.

### Automatic Service Management

The container uses an entrypoint script that **automatically starts etcd and PostgreSQL** when the container starts. The entrypoint is reentrant, meaning:

- It checks if services are already running before attempting to start them
- Multiple commands can be run without restarting services unnecessarily
- Services start in the background and remain available throughout the container's lifetime

You don't need to manually start etcd or PostgreSQL - they're ready to use immediately.

To run a specific command with services auto-started:

```bash
docker run -it --rm xiaonanln/goverse:dev go test ./...
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

For more information about using PostgreSQL with Goverse objects, see [docs/POSTGRES_SETUP.md](../docs/POSTGRES_SETUP.md).

## etcd Setup

etcd is pre-installed for cluster coordination and is **automatically started** by the entrypoint script.

To manually start etcd (if needed):

```bash
# Inside the container
etcd &
```

## Development Workflow

1. Pull or build the Docker image:
   ```bash
   # Option 1: Pull pre-built image (recommended)
   docker pull xiaonanln/goverse:dev
   
   # Option 2: Build locally
   docker build -f docker/Dockerfile.dev -t xiaonanln/goverse:dev .
   ```

2. Run the container:
   ```bash
   docker run -it --rm -v $(pwd):/app xiaonanln/goverse:dev
   ```
   
   Or use the convenience wrapper script:
   ```bash
   ./script/goverse-dev.sh bash
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

## Dockerfile.node

The `Dockerfile.node` provides a production-ready container for Goverse node applications.

Unlike gate and inspector, there is no pre-built node binary. A goverse node is **your own Go application** that imports goverse as a library, registers object types, and runs a server. This Dockerfile builds your application and packages it into a minimal container.

### How It Works

The Dockerfile uses a build argument `APP_PATH` to specify which Go package to compile. Point it at your application's `main` package:

```bash
docker build -f docker/Dockerfile.node \
  --build-arg APP_PATH=./cmd/myapp \
  -t my-goverse-node .
```

If no `APP_PATH` is provided, it defaults to `./samples/chat/server`.

### Writing a Node Application

A minimal node application looks like this:

```go
package main

import (
    "context"
    "github.com/xiaonanln/goverse/goverseapi"
)

type MyObject struct {
    goverseapi.BaseObject
}

func main() {
    goverseapi.RegisterObjectType((*MyObject)(nil))
    server := goverseapi.NewServer()
    if err := server.Run(context.Background()); err != nil {
        panic(err)
    }
}
```

Place this in your project (e.g. `cmd/myapp/main.go`), then build:

```bash
docker build -f docker/Dockerfile.node \
  --build-arg APP_PATH=./cmd/myapp \
  -t myapp-node .
```

### Features

- **Multi-stage build**: Uses Go 1.25 for compilation and Alpine Linux for minimal runtime
- **Security**: Runs as non-root user (uid/gid 1000)
- **Health checks**: Includes built-in health check using `/healthz` endpoint
- **Small size**: Alpine + static binary
- **Ports**:
  - 50051: gRPC port for inter-node communication
  - 8080: HTTP port for metrics, health checks, and pprof

### Building the Default Image

```bash
docker build -f docker/Dockerfile.node -t goverse-node:latest .
```

This builds the chat sample server (`samples/chat/server`).

### Running the Node Container

```bash
docker run -d \
  --name goverse-node \
  -p 50051:50051 \
  -p 8080:8080 \
  -v /path/to/config.yaml:/etc/goverse/config.yaml \
  goverse-node:latest \
  --config=/etc/goverse/config.yaml
```

### Health Checks

The node server exposes health endpoints on its HTTP port:

```bash
# Liveness probe — returns 200 if the process is alive
curl http://localhost:8080/healthz

# Readiness probe — returns 200 when the node has joined the cluster
curl http://localhost:8080/ready
```

### Available Node Endpoints

- `/healthz` - Liveness check (always 200 when running)
- `/ready` - Readiness check (200 when cluster is joined and node is not shutting down)
- `/metrics` - Prometheus metrics
- `/debug/pprof/*` - pprof profiling endpoints

### CI/CD

The node image is automatically built and pushed to Docker Hub via GitHub Actions (`.github/workflows/docker.yml`) on every push to `main` or `develop` branches.

Images are tagged with:
- `latest` - Latest build from the branch
- `<short-sha>` - Specific commit (first 7 chars of commit SHA)

### Kubernetes Deployment

See `k8s/nodes/statefulset.yaml` for Kubernetes deployment manifests. The K8s manifests inject environment variables for node identity and advertise addresses via the downward API.

### Security Considerations

- Container runs as non-root user (goverse:goverse, uid:gid 1000:1000)
- Only exposes necessary ports
- Static binary with stripped symbols

## Dockerfile.gate

The `Dockerfile.gate` provides a production-ready container for the Goverse gate component.

### Features

- **Multi-stage build**: Uses Go 1.25 for compilation and Alpine Linux for minimal runtime
- **Security**: Runs as non-root user (uid/gid 1000)
- **Health checks**: Includes built-in health check using `/healthz` endpoint
- **Small size**: ~20MB final image (Alpine + static binary)
- **Ports**:
  - 60051: gRPC port for client connections
  - 8080: HTTP port for REST API and health checks

### Building the Gate Image

```bash
docker build -f docker/Dockerfile.gate -t xiaonanln/goverse-gate:latest .
```

### Running the Gate Container

Basic run:
```bash
docker run -d \
  --name goverse-gate \
  -p 60051:60051 \
  -p 8080:8080 \
  xiaonanln/goverse-gate:latest \
  --listen=:60051 \
  --http-listen=:8080 \
  --etcd=etcd:2379 \
  --etcd-prefix=/goverse
```

With custom configuration:
```bash
docker run -d \
  --name goverse-gate \
  -p 60051:60051 \
  -p 8080:8080 \
  xiaonanln/goverse-gate:latest \
  --listen=:60051 \
  --http-listen=:8080 \
  --etcd=etcd1:2379,etcd2:2379 \
  --advertise=gate.example.com:60051
```

### Health Checks

The gate image includes a built-in health check that queries the `/healthz` endpoint:

```bash
# Check health status
curl http://localhost:8080/healthz
# Expected response: {"status":"ok"}
```

Docker will automatically use the `HEALTHCHECK` directive to monitor container health.

### Available Gate Endpoints

When HTTP is enabled (`--http-listen`), the gate exposes:

- `/healthz` - Health check endpoint
- `/metrics` - Prometheus metrics
- `/api/v1/objects/call/{type}/{id}/{method}` - Call object methods
- `/api/v1/objects/create/{type}/{id}` - Create objects
- `/api/v1/objects/delete/{id}` - Delete objects
- `/api/v1/events/stream` - SSE event stream for push messages
- `/debug/pprof/*` - pprof profiling endpoints

### CI/CD

The gate image is automatically built and pushed to Docker Hub via GitHub Actions (`.github/workflows/docker.yml`) on every push to `main` or `develop` branches.

Images are tagged with:
- `latest` - Latest build from the branch
- `<short-sha>` - Specific commit (first 7 chars of commit SHA)

### Kubernetes Deployment

See `k8s/gate-deployment.yaml` for Kubernetes deployment manifests with proper security settings.

### Security Considerations

- Container runs as non-root user (goverse:goverse, uid:gid 1000:1000)
- Read-only root filesystem in Kubernetes deployments
- No privilege escalation
- All Linux capabilities dropped
- Only exposes necessary ports

