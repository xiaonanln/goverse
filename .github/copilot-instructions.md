# GitHub Copilot Instructions for Goverse

**Meta**: Keep instructions short, clear, concise.

## Project Overview

Goverse is a **distributed object runtime for Go** implementing the **virtual actor model**. Objects are stateful entities with unique IDs. The runtime handles placement, routing, lifecycle, and fault-tolerance using etcd for coordination and a fixed 8192-shard model.

## Key Components

- **node/** - Object lifecycle, management, per-object locking
- **cluster/** - Leadership, shard mapping, node coordination
- **gate/** - Gate for client connections (separate from node)
- **server/** - Node gRPC server (Goverse service only)
- **goverseapi/** - High-level API wrappers
- **proto/** - Protocol definitions
- **docs/RELIABLE_CALLS_DESIGN.md** - Reliable call design doc covering architecture and PR plan

## Base Patterns

```go
// Objects extend BaseObject
type MyObject struct {
    goverseapi.BaseObject
    mu sync.Mutex  // Protect state
}

// Register and use
goverseapi.RegisterObjectType((*MyObject)(nil))
goverseapi.CreateObject(ctx, "MyObject", "uniqueId", nil)
resp, _ := goverseapi.CallObject(ctx, "MyObject-id", "Method", req)
```

## Node Locking (CRITICAL)

**Lock hierarchy** (acquire in this order):
1. `stopMu.RLock()` - Lifecycle protection
2. `objectLifecycleLock.Lock(objID)` - Per-object serialization  
3. `objectsMu` - Map access (brief, use double-check)

```go
// Standard pattern
node.stopMu.RLock()
defer node.stopMu.RUnlock()
unlockKey := node.objectLifecycleLock.Lock(objID)
defer unlockKey()
// Brief objectsMu access for map operations
```

**Rules:**
- Never acquire higher lock while holding lower
- Release map locks before calling object methods
- `DeleteObject` is idempotent (succeeds if already gone or node stopped)

## Build & Test

**Always compile protos first** before building or testing:
- Linux/macOS: `./script/compile-proto.sh`
- Windows: `.\script\win\compile-proto.cmd`

```bash
# Linux/macOS
./script/compile-proto.sh
go build ./...
go test -v -p 1 ./...

# Windows (PowerShell)
.\script\win\compile-proto.cmd
go build ./...
go test -v -p 1 ./...
```

Proto files: `proto/goverse.proto`, `gate/proto/gate.proto`

## Code Conventions

- Run `go fmt ./...` and `go vet ./...` before commits
- Remove deprecated methods entirely (no deprecation notices)
- Use `sync.Mutex` in objects, lock at method start with `defer unlock()`
- Use `context.Context` for cancellation/timeouts
- Log via `util/logger`
- Keep code robust, simple, no over-design
- Watch out for concurrency issues - review all shared state access

### Goroutine Safety (CRITICAL)

**Always prevent resource leaks when adding goroutines:**
- Use context cancellation (`ctx.Done()`) for graceful shutdown
- Clean up resources with `defer` (tickers, channels, connections)
- Avoid fire-and-forget goroutines without shutdown mechanism

### Lock Safety (CRITICAL)

**Always prevent deadlocks and premature unlocks:**
- Follow lock hierarchy strictly (see Node Locking section)
- Use `defer` immediately after lock acquisition
- Never hold locks across async operations or goroutine boundaries
- Never call methods that acquire locks while holding locks (check call chain)

### Map Key vs Field Lookup (CRITICAL)

**Ensure consistent identifier usage in maps and lookups:**
- When a function takes an identifier (e.g., `nodeAddress`), verify the map key matches
- Common bug: storing by `ID` but looking up by `Address` (or vice versa)
- Function names should match their implementation (e.g., `IsNodeRegistered(address)` should lookup by address)
- Test with different identifier types to catch cross-identifier lookup issues

### Proto Message Conversion

**Use `util/protohelper`** for converting between `proto.Message`, `anypb.Any`, and `[]byte`:

```go
import "github.com/xiaonanln/goverse/util/protohelper"

// proto.Message <-> anypb.Any
anyMsg, err := protohelper.MsgToAny(msg)
msg, err := protohelper.AnyToMsg(anyMsg)

// proto.Message <-> []byte (via anypb.Any)
data, err := protohelper.MsgToBytes(msg)
msg, err := protohelper.BytesToMsg(data)

// anypb.Any <-> []byte
data, err := protohelper.AnyToBytes(anyMsg)
anyMsg, err := protohelper.BytesToAny(data)
```

## Testing

**Coverage targets**: util/object/client 100%, cluster 90%+

**Philosophy**: Write essential tests, but do not over-test. Focus on critical paths and edge cases.

### etcd Tests

Tests auto-skip if etcd unavailable. For full coverage, run:
```bash
docker run -d --name etcd-test -p 2379:2379 quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379
```

**Always use `testutil.PrepareEtcdPrefix(t, "localhost:2379")`** for test isolation:

```go
func TestWithEtcd(t *testing.T) {
    prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
    mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
    if err != nil {
        t.Skipf("Skipping - etcd unavailable: %v", err)
        return
    }
    defer mgr.Close()
    // Test code - cleanup automatic
}
```

### Integration Test Readiness

**Use `testutil.WaitForClustersReady()` to ensure all clusters are fully ready** before test assertions:
```go
// Wait for multiple clusters (nodes and gates) to be fully ready
testutil.WaitForClustersReady(t, nodeCluster1, nodeCluster2, gateCluster)
```

This is preferred over multiple `WaitForClusterReady()` calls because:
- Performs comprehensive cross-cluster validation
- Ensures all nodes see each other in cluster state
- Verifies gates are connected to all nodes
- Confirms shard mappings are complete (CurrentNode == TargetNode)

### Mock Helpers

**Persistence**: Use `node.NewMockPersistenceProvider()` - **only access via thread-safe methods**:
```go
provider.HasStoredData("id")
provider.GetStoredData("id")  // Returns copy
provider.GetStorageCount()
```
Never access `provider.storage` directly (causes races).

**Distributed tests**: Use `testutil.NewMockGoverseServer()` + `testutil.NewTestServerHelper()` for node-to-node gRPC:
```go
mockServer := testutil.NewMockGoverseServer()
mockServer.SetNode(node1)
nodeAddr := testutil.GetFreeAddress()  // Dynamic port allocation
testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
testServer.Start(ctx)
defer testServer.Stop()
```

**Dynamic ports (CRITICAL)**: **Never use hardcoded ports when starting actual servers in tests** - always use `testutil.GetFreeAddress()`:
```go
// CORRECT: Dynamic port allocation for actual servers
nodeAddr := testutil.GetFreeAddress()  // Returns "localhost:12345"
gateAddr := testutil.GetFreeAddress()  // Different free port
testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
testServer.Start(ctx)  // Actually binds to port

// WRONG: Hardcoded ports with actual servers
nodeAddr := "localhost:48020"  // ❌ Port conflict when server starts
gateAddr := "localhost:48021"  // ❌ Fails in parallel or if port busy

// OK: Hardcoded ports without actual servers (routing tests, etc.)
nodeAddr := "localhost:48020"  // ✅ Fine if no server binds to this port
cluster.GetCurrentNodeForObject(ctx, objID)  // Just routing logic
```

**Why dynamic ports are critical:**
- Prevents port conflicts when running tests in parallel
- Avoids failures from leftover processes using ports
- Enables multiple test instances to run simultaneously
- Required for reliable CI/CD execution

### Shard Configuration in Tests

**Production**: 8192 shards (default)  
**Tests**: 64 shards (via `testutil.TestNumShards`)

**Always use `testutil.TestNumShards` for test shard counts:**

```go
// Node configuration
nodeConfig := &node.Config{
    NumShards: testutil.TestNumShards,  // Required
    // ... other config
}

// Gate configuration
gateConfig := &gateserver.GateServerConfig{
    NumShards: testutil.TestNumShards,  // Required
    // ... other config
}

// Cluster configuration
clusterConfig := &cluster.Config{
    NumShards: testutil.TestNumShards,  // Required
    // ... other config
}
```

**Why 64 shards in tests:**
- Faster shard operations and leadership elections
- Reduced resource usage in test environments
- Still validates distributed behavior
- Valid shard IDs: 0-63 (not 0-8191)

**Common mistakes:**
- Omitting NumShards (defaults to 8192, causes slow tests)
- Using hardcoded shard IDs >= 64 (use 0-63 range)
- Inconsistent shard counts across test components

**Shard-specific objects**: Use `testutil.GetObjectIDForShard()` to generate IDs for specific shards:
```go
// Get object IDs that hash to specific shards
objA := testutil.GetObjectIDForShard(5, "TestObjectA")   // Shard 5
objB := testutil.GetObjectIDForShard(5, "TestObjectB")   // Same shard
objC := testutil.GetObjectIDForShard(10, "TestObjectC")  // Different shard
```

### Test Conventions

- Use `t.Fatalf()` not `t.Errorf()` (fail fast)
- Exception: Use `t.Error*()` from goroutines
- Table-driven tests with `t.Run()`
- Skip long tests (>10s) in short mode: `if testing.Short() { t.Skip() }`

## Inspector UI (cmd/inspector/web)

**Data flow**: Backend → SSE events → JS `graphData` → D3 node objects → D3 rendering

**Field naming consistency (CRITICAL)**:
- JSON from backend uses snake_case (`calls_per_minute`)
- JS node objects use camelCase (`callsPerMinute`)
- Always verify both sides match when adding new fields

**D3 data binding quirk**:
- `.data(nodes, d => d.id)` on existing elements keeps OLD bound data
- Click handlers receive stale data unless you either:
  - Look up fresh data from `graphData` source array, OR
  - Force re-binddata on updates
- `updateGraphIncremental()` must be called to refresh visual labels

**Adding new object fields to graph**:
1. Add field in `buildGraphNodesAndLinks()` node object
2. Add field in `updateGraphIncremental()` node object (same place)
3. Use camelCase matching what rendering functions expect
4. Call `updateGraphIncremental()` on SSE update events to refresh display
