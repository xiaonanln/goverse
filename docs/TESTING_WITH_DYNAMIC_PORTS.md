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
port, err := testutil.GetFreePort()
if err != nil {
    t.Fatalf("Failed to get free port: %v", err)
}
// Use: localhost:12345 (actual port will vary)
```

### 2. Get a Free Address (Recommended)

```go
addr, err := testutil.GetFreeAddress()
if err != nil {
    t.Fatalf("Failed to get free address: %v", err)
}
// Returns: "localhost:12345" (port will vary)
```

### 3. Use TestServerHelper with Dynamic Ports

`TestServerHelper` automatically captures the actual bound address when using `:0`:

```go
// Create server with dynamic port allocation
mockServer := testutil.NewMockGoverseServer()
mockServer.SetNode(node)
testServer := testutil.NewTestServerHelper("localhost:0", mockServer)

err := testServer.Start(ctx)
if err != nil {
    t.Fatalf("Failed to start server: %v", err)
}
defer testServer.Stop()

// Get the actual bound address
actualAddr := testServer.GetAddress()
// actualAddr will be something like "127.0.0.1:45678"
```

## Examples

### Example 1: Simple Node Test

```go
func TestMyNodeFeature(t *testing.T) {
    // Get a free address for the node
    nodeAddr, err := testutil.GetFreeAddress()
    if err != nil {
        t.Fatalf("Failed to get free address: %v", err)
    }

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
    addr1, err := testutil.GetFreeAddress()
    if err != nil {
        t.Fatalf("Failed to get address for node1: %v", err)
    }
    addr2, err := testutil.GetFreeAddress()
    if err != nil {
        t.Fatalf("Failed to get address for node2: %v", err)
    }

    // Create nodes
    node1 := node.NewNode(addr1)
    node2 := node.NewNode(addr2)

    // Start mock servers
    server1 := testutil.NewTestServerHelper("localhost:0", mockServer1)
    server1.Start(ctx)
    defer server1.Stop()

    server2 := testutil.NewTestServerHelper("localhost:0", mockServer2)
    server2.Start(ctx)
    defer server2.Stop()

    // Get actual addresses
    actualAddr1 := server1.GetAddress()
    actualAddr2 := server2.GetAddress()

    // ... rest of test
}
```

### Example 3: Gateway Integration Test

```go
func TestGateNodeIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    ctx := context.Background()
    etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

    // Get free addresses
    nodeAddr, err := testutil.GetFreeAddress()
    if err != nil {
        t.Fatalf("Failed to get node address: %v", err)
    }

    gateAddr, err := testutil.GetFreeAddress()
    if err != nil {
        t.Fatalf("Failed to get gate address: %v", err)
    }

    // Create node cluster
    nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
    testNode := nodeCluster.GetThisNode()

    // Start mock server for node
    mockNodeServer := testutil.NewMockGoverseServer()
    mockNodeServer.SetNode(testNode)
    mockNodeServer.SetCluster(nodeCluster)
    nodeServer := testutil.NewTestServerHelper("localhost:0", mockNodeServer)
    nodeServer.Start(ctx)
    defer nodeServer.Stop()

    actualNodeAddr := nodeServer.GetAddress()

    // Create gateway server
    gwConfig := &GatewayServerConfig{
        ListenAddress:    gateAddr,
        AdvertiseAddress: gateAddr,
        EtcdAddress:      "localhost:2379",
        EtcdPrefix:       etcdPrefix,
    }
    gwServer, err := NewGatewayServer(gwConfig)
    // ... rest of test
}
```

## Best Practices

1. **Always use dynamic ports in tests**: Use `testutil.GetFreeAddress()` or `localhost:0` with TestServerHelper
2. **Get addresses early**: Allocate all needed addresses at the start of your test
3. **Use TestServerHelper's GetAddress()**: After starting a server with `:0`, get the actual address
4. **Clean up resources**: Always use `defer` to stop servers and clean up
5. **Enable parallel tests**: Tests with dynamic ports can run in parallel:
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
nodeAddr, err := testutil.GetFreeAddress()
if err != nil {
    t.Fatalf("Failed to get free address: %v", err)
}
```

### Step 3: Update TestServerHelper usage
```go
// Before
testServer := testutil.NewTestServerHelper("localhost:47001", mockServer)
testServer.Start(ctx)

// After
testServer := testutil.NewTestServerHelper("localhost:0", mockServer)
testServer.Start(ctx)
actualAddr := testServer.GetAddress()
// Use actualAddr where needed
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
- Make sure to use `testServer.GetAddress()` after starting the server
- Don't use the original address if you specified `:0`
- Allow time for servers to start before making connections

### Tests are slow
- Dynamic port allocation is very fast (microseconds)
- Slowness is likely due to other factors (etcd, network delays)
- Consider using `testing.Short()` to skip long-running integration tests
