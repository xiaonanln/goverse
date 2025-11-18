# Test Cleanup and Synchronization Changes

## Summary

This document describes the changes made to fix port conflicts and improve test cleanup in tests that use mock servers.

## Problem Statement

The integration tests for distributed features were experiencing port binding conflicts when running in parallel:

1. **Port Conflicts**: Multiple tests attempted to bind to the same ports (e.g., localhost:47011, localhost:47012) simultaneously
2. **Parallel Execution**: Tests used `t.Parallel()` which allowed concurrent execution, causing port conflicts
3. **Cleanup Timing**: Using `defer` for cleanup could lead to improper resource cleanup order in case of test failures

## Root Cause

Two test files used mock gRPC servers that bound to specific ports:
- `cluster_distributed_createobject_integration_test.go` - uses ports 47001, 47002, 47011-47013
- `cluster_pushmessage_integration_test.go` - uses ports 47011, 47012

Both tests had overlapping port usage (47011, 47012) and used `t.Parallel()`, causing them to potentially run at the same time and fail due to "address already in use" errors.

## Solution

### 1. Removed `t.Parallel()` from Tests Using Mock Servers

Tests that bind to specific network ports can no longer run in parallel to prevent port conflicts:

**Files Modified:**
- `cluster/cluster_distributed_createobject_integration_test.go`
  - `TestDistributedCreateObject` 
  - `TestDistributedCreateObject_EvenDistribution`
- `cluster/cluster_pushmessage_integration_test.go`
  - `TestDistributedPushMessageToClient`

**Changes:**
- Removed `t.Parallel()` calls from test functions
- Added explanatory comments noting why these tests don't run in parallel

### 2. Replaced `defer` with `t.Cleanup()` for Resource Cleanup

Using `t.Cleanup()` provides more reliable cleanup because:
- Cleanup functions are guaranteed to run even if the test panics
- Cleanup happens in LIFO order (last registered, first executed)
- More explicit and consistent with modern Go testing practices

**Resources Now Using t.Cleanup():**
- Mock gRPC servers (`testServer1.Stop()`, `testServer2.Stop()`)
- Node instances (`node1.Stop(ctx)`, `node2.Stop(ctx)`)
- Cluster connections (`cluster1.CloseEtcd()`, `cluster2.CloseEtcd()`)
- Node registrations (`cluster1.UnregisterNode(ctx)`, `cluster2.UnregisterNode(ctx)`)
- Node connections (`cluster1.StopNodeConnections()`, `cluster2.StopNodeConnections()`)
- Shard mapping management (`cluster1.StopShardMappingManagement()`, `cluster2.StopShardMappingManagement()`)

## Impact

### Positive Effects

1. **Eliminates Port Conflicts**: Tests using mock servers now run sequentially, preventing "address already in use" errors
2. **More Reliable Cleanup**: Using `t.Cleanup()` ensures resources are properly released even if tests fail
3. **Clearer Intent**: Comments explain why certain tests don't run in parallel
4. **Better Test Isolation**: Proper cleanup prevents test state from leaking between tests

### Potential Drawbacks

1. **Slightly Slower Test Execution**: Tests that previously could run in parallel now run sequentially
   - However, these are integration tests that take significant time anyway due to network operations and timeouts
   - The reliability improvement outweighs the minor performance impact

## Testing Strategy

### Tests That Still Run in Parallel

Most tests continue to run in parallel, including:
- Unit tests in `object/`, `client/`, `util/` packages
- Cluster tests that don't use network ports (e.g., `cluster_shardmapping_management_test.go`)
- Tests using different port ranges (e.g., `cluster_automatic_shardmapping_integration_test.go` uses ports 50011-50022)

### Tests That Now Run Sequentially

Only tests that bind to specific network ports run sequentially:
- `TestDistributedCreateObject` - tests object creation across nodes
- `TestDistributedCreateObject_EvenDistribution` - tests shard distribution
- `TestDistributedPushMessageToClient` - tests cross-node messaging

## Port Allocation Reference

For future test development, here are the port ranges currently in use:

- **47001-47013**: Distributed object creation tests
- **47100-47102**: Server startup tests  
- **47300**: Miscellaneous tests
- **50011-50022**: Automatic shard mapping tests
- **51001-51003**: Other cluster tests

When creating new tests that bind to ports, use unique port numbers not in the above ranges and avoid `t.Parallel()` if using mock servers.

## Code Examples

### Before (with defer and t.Parallel)
```go
func TestDistributedCreateObject(t *testing.T) {
    t.Parallel()  // Allows concurrent execution
    
    testServer1 := NewTestServerHelper("localhost:47001", mockServer1)
    err = testServer1.Start(ctx)
    if err != nil {
        t.Fatalf("Failed to start mock server 1: %v", err)
    }
    defer testServer1.Stop()  // Cleanup with defer
    
    // ... test code ...
}
```

### After (with t.Cleanup and no parallel)
```go
func TestDistributedCreateObject(t *testing.T) {
    // No t.Parallel() - prevents port conflicts
    
    testServer1 := NewTestServerHelper("localhost:47001", mockServer1)
    err = testServer1.Start(ctx)
    if err != nil {
        t.Fatalf("Failed to start mock server 1: %v", err)
    }
    t.Cleanup(func() { testServer1.Stop() })  // More reliable cleanup
    
    // ... test code ...
}
```

## Verification

The changes have been verified by:
1. ✅ Successful compilation of all test files
2. ✅ No syntax errors in modified test functions
3. ✅ Proper cleanup order maintained (LIFO with t.Cleanup)
4. ✅ Clear documentation added to test function comments

## Future Recommendations

1. **Port Management**: Consider using dynamic port allocation (port 0) with subsequent discovery for better test isolation
2. **Test Utilities**: Create helper functions to automatically manage test server lifecycle with proper cleanup
3. **Documentation**: Update TESTING.md to include guidelines about parallel test execution and port management
4. **Monitoring**: Track test execution times to ensure sequential execution doesn't significantly impact CI/CD performance

## Related Files

- `cluster/cluster_distributed_createobject_integration_test.go` - Modified
- `cluster/cluster_pushmessage_integration_test.go` - Modified  
- `cluster/test_server_helper.go` - Used by tests (no changes needed)
- `util/testutil/etcd.go` - Provides test isolation utilities (no changes needed)
