# Testing Guide

## Test Organization

GoVerse tests are organized into several categories:

### Normal Tests

Regular unit and integration tests that can run in parallel:

```bash
go test ./...
```

These tests cover:
- Unit tests for individual components
- Integration tests that don't modify shared infrastructure
- Tests that use etcd but don't restart it

### Etcd Restart Tests

Tests that restart the etcd service to verify reconnection behavior. These tests are isolated using the `etcd_restart` build tag to prevent interference with parallel tests:

```bash
go test -tags=etcd_restart -p=1 ./...
```

The `-p=1` flag ensures these tests run sequentially (one package at a time) since restarting etcd affects all tests.

Tests with the `etcd_restart` build tag:
- `cluster/consensusmanager/consensusmanager_watch_robustness_test.go` - Tests consensus manager watch reconnection
- `cluster/etcdmanager/keepalive_reconnection_test.go` - Tests lease keepalive reconnection

### Running All Tests

To run both normal and etcd restart tests:

```bash
# Run normal tests (parallel)
go test ./...

# Run etcd restart tests (sequential)
go test -tags=etcd_restart -p=1 ./...
```

Or use the convenience scripts:

```bash
# Docker environment
./script/docker/test-go.sh     # Runs both test categories
./script/docker/test-all.sh    # Runs all tests including integration tests

# CI environment (.github/workflows/test.yml)
# Automatically runs both categories
```

## Build Tags

Build tags allow selective compilation of test files. The `etcd_restart` tag ensures tests that restart etcd are only compiled and run when explicitly requested.

### Using Build Tags

File with build tag:
```go
//go:build etcd_restart
// +build etcd_restart

package mypackage

import "testing"

func TestSomethingThatRestartsEtcd(t *testing.T) {
    // This test only runs when: go test -tags=etcd_restart
}
```

### Why Isolate Etcd Restart Tests?

1. **Performance**: Normal tests can run in parallel across packages, significantly faster than sequential execution
2. **Reliability**: Restarting etcd during parallel tests can cause random failures in unrelated tests
3. **Clarity**: Clearly separates disruptive tests from regular tests

## Test Isolation

### Etcd Prefix Isolation

Tests that use etcd must use unique prefixes to prevent interference:

```go
func TestWithEtcd(t *testing.T) {
    // PrepareEtcdPrefix provides automatic test isolation
    prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
    
    mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
    // ...
}
```

### Parallel Tests

Tests that can run in parallel should use `t.Parallel()`:

```go
func TestSomething(t *testing.T) {
    t.Parallel()  // Can run in parallel with other tests
    
    // Test code...
}
```

Do NOT use `t.Parallel()` in tests that:
- Restart etcd
- Modify global state
- Use singleton resources

## CI/CD Integration

GitHub Actions workflow (`.github/workflows/test.yml`) runs:

1. **Normal tests** - Runs with coverage, parallel execution across packages
2. **Etcd restart tests** - Runs sequentially after normal tests

This ensures fast feedback for most tests while still validating reconnection scenarios.

## Local Development

### Quick Test Iteration

```bash
# Test a specific package
go test ./cluster/etcdmanager/

# Test a specific function
go test ./cluster/etcdmanager/ -run TestKeepAliveRetry

# Verbose output
go test -v ./cluster/etcdmanager/
```

### Testing With Etcd Restart

```bash
# Run only etcd restart tests
go test -tags=etcd_restart -v ./cluster/consensusmanager/
go test -tags=etcd_restart -v ./cluster/etcdmanager/

# Run all etcd restart tests
go test -tags=etcd_restart -p=1 -v ./...
```

### Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Coverage for specific package
go test -coverprofile=coverage.out ./cluster/etcdmanager/
go tool cover -html=coverage.out
```

## Best Practices

1. **Use PrepareEtcdPrefix**: Always use `testutil.PrepareEtcdPrefix()` for etcd integration tests
2. **Enable Parallel**: Use `t.Parallel()` unless the test modifies shared state
3. **Isolate Disruptive Tests**: Use build tags for tests that restart services or modify global state
4. **Clean Up**: Use `t.Cleanup()` for automatic cleanup
5. **Skip When Appropriate**: Use `t.Skip()` when prerequisites aren't met
6. **Fast Tests**: Keep unit tests fast; use mocks for slow dependencies
7. **Clear Names**: Test names should clearly describe what is being tested

## Troubleshooting

### Tests Fail with "connection refused"

- Ensure etcd is running: `sudo systemctl status etcd`
- Check etcd is listening on correct port: `curl http://localhost:2379/health`

### Tests Interfere with Each Other

- Check if test is using unique etcd prefix via `PrepareEtcdPrefix()`
- Consider if test should use `t.Parallel()`

### Etcd Restart Tests Fail

- These tests require GitHub Actions environment or proper etcd setup
- They should skip automatically in environments without proper infrastructure
- Check `testutil.IsGitHubActions()` condition

### Coverage Not Generated

- Ensure `-coverprofile` flag is used
- Check that tests are actually running (not all skipped)
- For etcd restart tests, coverage is collected separately
