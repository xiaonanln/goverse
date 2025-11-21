# GitHub Copilot Instructions for Goverse

**Meta**: Keep instructions short, clear, concise.

## Project Overview

Goverse is a **distributed object runtime for Go** implementing the **virtual actor model**. Objects are stateful entities with unique IDs. The runtime handles placement, routing, lifecycle, and fault-tolerance using etcd for coordination and a fixed 8192-shard model.

**Gateway refactor**: See [GATEWAY_DESIGN.md](../GATEWAY_DESIGN.md)

## Key Components

- **node/** - Object lifecycle, management, per-object locking
- **cluster/** - Leadership, shard mapping, node coordination
- **gate/** - Gateway for client connections (separate from node)
- **server/** - Node gRPC server (Goverse service only)
- **goverseapi/** - High-level API wrappers
- **proto/** - Protocol definitions

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
2. `keyLock.Lock(objID)` - Per-object serialization  
3. `objectsMu` - Map access (brief, use double-check)

```go
// Standard pattern
node.stopMu.RLock()
defer node.stopMu.RUnlock()
unlockKey := node.keyLock.Lock(objID)
defer unlockKey()
// Brief objectsMu access for map operations
```

**Rules:**
- Never acquire higher lock while holding lower
- Release map locks before calling object methods
- `DeleteObject` is idempotent (succeeds if already gone or node stopped)

## Build & Test

**Always run `./script/compile-proto.sh` first** before building or testing.

```bash
./script/compile-proto.sh  # Required first step
go build ./...
go test -v -p 1 ./...
```

Proto files: `proto/goverse.proto`, `client/proto/gateway.proto`, `gate/proto/gate.proto`

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
testServer := testutil.NewTestServerHelper("localhost:47001", mockServer)
testServer.Start(ctx)
defer testServer.Stop()
```

### Test Conventions

- Use `t.Fatalf()` not `t.Errorf()` (fail fast)
- Exception: Use `t.Error*()` from goroutines
- Table-driven tests with `t.Run()`
- Skip long tests (>10s) in short mode: `if testing.Short() { t.Skip() }`
