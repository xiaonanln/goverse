# Docker Test Scripts

This directory contains scripts for managing services and running tests inside Docker containers.

## Service Management Scripts

All service management scripts are **reentrant**, meaning they can be executed multiple times without causing errors or inconsistencies.

### Start Scripts

- **`start-etcd.sh`** - Start etcd for testing
  - Checks if etcd is already running before starting
  - Returns success (exit 0) if etcd is already running and healthy
  - Restarts etcd if the process exists but is not responding
  - Waits up to 30 seconds for etcd to become ready

- **`start-postgres.sh`** - Start PostgreSQL for testing
  - Checks if PostgreSQL is already running before starting
  - Returns success (exit 0) if PostgreSQL is already running
  - Waits up to 30 seconds for PostgreSQL to become ready

### Stop Scripts

- **`stop-etcd.sh`** - Stop etcd
  - Checks if etcd is running before attempting to stop
  - Returns success (exit 0) if etcd is not running
  - Gracefully terminates etcd (SIGTERM), with fallback to force kill (SIGKILL)
  - Waits up to 10 seconds for graceful shutdown

- **`stop-postgres.sh`** - Stop PostgreSQL
  - Checks if PostgreSQL is running before attempting to stop
  - Returns success (exit 0) if PostgreSQL is not running
  - Uses `pg_ctl stop` with fast mode, fallback to immediate mode if needed
  - Waits up to 30 seconds for graceful shutdown

### Usage Examples

```bash
# Start services (can be run multiple times safely)
./script/docker/start-etcd.sh
./script/docker/start-postgres.sh

# Stop services (can be run multiple times safely)
./script/docker/stop-etcd.sh
./script/docker/stop-postgres.sh

# Start again (no errors, will detect services are running)
./script/docker/start-etcd.sh
./script/docker/start-postgres.sh
```

## Test Scripts

- **`test-go.sh`** - Run Go unit tests with etcd
- **`test-chat.sh`** - Run chat integration tests with etcd and PostgreSQL
- **`test-all.sh`** - Run all tests
- **`test-reentrant.sh`** - Test script to verify reentrant behavior of start/stop scripts

#### Etcd Restart Tests

Some tests restart the etcd service to test reconnection behavior. These tests are isolated using the `etcd_restart` build tag to prevent interference with other tests.

```bash
# Run normal tests (fast, parallel)
go test -p 1 ./...

# Run etcd restart tests (slow, sequential)
go test -tags=etcd_restart -v -p=1 -run=^.*Reconnection$ ./...

# Both are automatically run by test-go.sh and test-all.sh
```

### Running Tests

```bash
# Run Go unit tests
./script/docker/test-go.sh

# Run integration tests
./script/docker/test-chat.sh

# Run all tests
./script/docker/test-all.sh

# Test script reentrancy
./script/docker/test-reentrant.sh
```

## Design Notes

### Reentrancy

All start and stop scripts follow these principles:

1. **Idempotent operations**: Running a script multiple times produces the same result
2. **Safe state checks**: Scripts check the current state before taking action
3. **Clear exit codes**: Scripts return 0 on success, non-zero on failure
4. **Informative output**: Scripts use ✓ and ✗ symbols to indicate success/failure
5. **Graceful degradation**: Stop scripts attempt graceful shutdown before forcing

### Error Handling

All scripts use `set -euo pipefail` for robust error handling:
- `set -e`: Exit immediately if a command exits with a non-zero status
- `set -u`: Treat unset variables as an error
- `set -o pipefail`: Return the exit status of the last command in a pipe that failed

### Environment

These scripts are designed to run inside the `xiaonanln/goverse:dev` Docker container where:
- etcd is available in PATH
- PostgreSQL is installed and configured
- The postgres user exists for running PostgreSQL commands
