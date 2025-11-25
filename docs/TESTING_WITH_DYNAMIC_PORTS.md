# Testing with Dynamic Ports

## Overview

To enable parallel test execution and avoid port conflicts, tests should use dynamic port allocation instead of hardcoded ports. This document explains the utilities available and patterns to follow.

## Problem

Previously, many tests used hardcoded ports:
```go
// ❌ Hardcoded port - can cause conflicts
nodeAddr := "localhost:47000"
gateAddr := "localhost:49000"
```

This caused several issues:
1. Tests cannot run in parallel (`-p 1` required)
2. Port conflicts when multiple tests run simultaneously
3. Flakiness in CI/CD when ports are already in use

## Solution

Use the dynamic port allocation utilities in `util/testutil`:

### 1. Get a Free Port Number

```go
port := testutil.GetFreePort()
// Use: localhost:12345 (actual port will vary)
```

### 2. Get a Free Address (Recommended)

```go
addr := testutil.GetFreeAddress()
// Returns: "localhost:12345" (port will vary)
```

## Examples

### Example 1: Simple Node Test

```go
func TestMyNodeFeature(t *testing.T) {
    // Get a free address for the node
    nodeAddr := testutil.GetFreeAddress()

    // Create node with dynamic address
    n := node.NewNode(nodeAddr)
    n.RegisterObjectType((*MyObject)(nil))

    // ... rest of test
}
```

### Example 2: Multiple Nodes (Distributed Test)

```go
func TestDistributedFeature(t *testing.T) {
    ctx := context.Background()

    // Get free addresses for multiple nodes
    addr1 := testutil.GetFreeAddress()
    addr2 := testutil.GetFreeAddress()

    // Create nodes
    node1 := node.NewNode(addr1)
    node2 := node.NewNode(addr2)

    // Start mock servers
    server1 := testutil.NewTestServerHelper(addr1, mockServer1)
    server1.Start(ctx)
    defer server1.Stop()

    server2 := testutil.NewTestServerHelper(addr2, mockServer2)
    server2.Start(ctx)
    defer server2.Stop()

    // ... rest of test
}
```

### Example 3: Gate Integration Test

```go
func TestGateNodeIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    ctx := context.Background()
    etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

    // Get free addresses
    nodeAddr := testutil.GetFreeAddress()
    gateAddr := testutil.GetFreeAddress()

    // Create node cluster
    nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
    testNode := nodeCluster.GetThisNode()

    // Start mock server for node
    mockNodeServer := testutil.NewMockGoverseServer()
    mockNodeServer.SetNode(testNode)
    mockNodeServer.SetCluster(nodeCluster)
    nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
    nodeServer.Start(ctx)
    defer nodeServer.Stop()

    // Create gate server
    gwConfig := &GateServerConfig{
        ListenAddress:    gateAddr,
        AdvertiseAddress: gateAddr,
        EtcdAddress:      "localhost:2379",
        EtcdPrefix:       etcdPrefix,
    }
    gwServer, err := NewGateServer(gwConfig)
    // ... rest of test
}
```

## Best Practices

1. **Always use dynamic ports in tests**: Use `testutil.GetFreeAddress()` for all server addresses
2. **Get addresses early**: Allocate all needed addresses at the start of your test
3. **Clean up resources**: Always use `defer` to stop servers and clean up
4. **Enable parallel tests**: Tests with dynamic ports can run in parallel:
   ```bash
   go test -v -parallel 4 ./...
   ```

## Migration Guide

To migrate existing tests:

### Step 1: Add testutil import
```go
import (
    // ... existing imports
    "github.com/xiaonanln/goverse/util/testutil"
)
```

### Step 2: Replace hardcoded addresses
```go
// Before
nodeAddr := "localhost:47000"

// After
nodeAddr := testutil.GetFreeAddress()
```

### Step 3: Update TestServerHelper usage
```go
// Before
testServer := testutil.NewTestServerHelper("localhost:47001", mockServer)
testServer.Start(ctx)

// After
nodeAddr := testutil.GetFreeAddress()
testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
testServer.Start(ctx)
```

## Running Tests

With dynamic ports, tests can now run in parallel:

```bash
# Run all tests in parallel
go test -v -parallel 4 ./...

# Run specific package tests in parallel
go test -v -parallel 3 ./gate

# Run with race detector
go test -v -race -parallel 2 ./...
```

## Troubleshooting

### Port already in use
If you still see "address already in use" errors:
- Ensure all tests use dynamic port allocation
- Check that servers are properly cleaned up with `defer server.Stop()`
- Verify tests don't share global state that uses specific ports

### Tests fail with "connection refused"
- Ensure servers have started before making connections
- Allow time for servers to initialize (add small delays if needed)
- Verify the correct address is being used for connections

### Tests are slow
- Dynamic port allocation is very fast (microseconds)
- Slowness is likely due to other factors (etcd, network delays)
- Consider using `testing.Short()` to skip long-running integration tests
