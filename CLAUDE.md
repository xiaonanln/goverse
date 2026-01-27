# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Goverse is a **distributed object runtime for Go** implementing the **virtual actor model** (inspired by Orleans). Objects are stateful entities with unique IDs. The runtime handles placement, routing, lifecycle, and fault-tolerance using etcd for coordination and a fixed 8192-shard model.

## Architecture

**Node + Gate** architecture:
- **Node** hosts distributed objects, manages lifecycle, handles inter-node routing via gRPC
- **Gate** accepts client connections (gRPC and HTTP REST), routes calls to the correct node
- **Cluster** orchestrates nodes/gates, manages leadership election via etcd, maintains shard-to-node mapping
- **Sharding** uses FNV-1a hash: `hash(objectID) % numShards` to determine object placement

Key packages:
- **node/** - Object lifecycle, management, per-object locking
- **cluster/** - Leadership, shard mapping, node coordination (sub-packages: `consensusmanager/`, `sharding/`, `nodeconnections/`, `shardlock/`)
- **gate/** - Client connections, separate binary (`cmd/gate`)
- **server/** - High-level node server wrapper (Node + Cluster + metrics + pprof)
- **goverseapi/** - Public API: `RegisterObjectType()`, `CreateObject()`, `CallObject()`, `NewServerWithConfig()`
- **object/** - Base object types, persistence interface
- **proto/** - Core protobuf definitions
- **util/testutil/** - Test helpers (ports, clusters, sharding, mocks)
- **util/protohelper/** - Proto message conversion utilities

Object model:
```go
type MyObject struct {
    goverseapi.BaseObject
    mu sync.Mutex
}
goverseapi.RegisterObjectType((*MyObject)(nil))
goverseapi.CreateObject(ctx, "MyObject", "uniqueId", nil)
resp, _ := goverseapi.CallObject(ctx, "MyObject-id", "Method", req)
```

## Build & Test

**Always compile protos first** before building or testing:
```bash
# Windows (primary dev environment)
.\script\win\compile-proto.cmd
go build ./...
go test -v -p 1 ./...

# Linux/macOS
./script/compile-proto.sh
```

Run a single test:
```bash
go test -v -run TestName ./package/...
```

Proto files: `proto/goverse.proto`, `gate/proto/gate.proto`, `cmd/inspector/proto/inspector.proto`

## Code Conventions

- Run `go fmt ./...` and `go vet ./...` before commits
- Remove deprecated methods entirely (no deprecation notices)
- Use `sync.Mutex` in objects, lock at method start with `defer unlock()`
- Use `context.Context` for cancellation/timeouts
- Log via `util/logger` (e.g., `logger.NewLogger("ComponentName")`)
- Keep code robust, simple, no over-design
- Watch out for concurrency issues - review all shared state access

## Node Lock Hierarchy (CRITICAL)

Acquire in this order only:
1. `stopMu.RLock()` - Lifecycle protection
2. `objectLifecycleLock.Lock(objID)` - Per-object serialization
3. `objectsMu` - Map access (brief, use double-check)

Rules:
- Never acquire higher lock while holding lower
- Release map locks before calling object methods
- `DeleteObject` is idempotent (succeeds if already gone or node stopped)

## Goroutine Safety (CRITICAL)

- Use context cancellation (`ctx.Done()`) for graceful shutdown
- Clean up resources with `defer` (tickers, channels, connections)
- Avoid fire-and-forget goroutines without shutdown mechanism

## Lock Safety (CRITICAL)

- Follow lock hierarchy strictly
- Use `defer` immediately after lock acquisition
- Never hold locks across async operations or goroutine boundaries
- Never call methods that acquire locks while holding locks (check call chain)

## Map Key vs Field Lookup (CRITICAL)

- When a function takes an identifier, verify the map key matches
- Common bug: storing by `ID` but looking up by `Address` (or vice versa)
- Function names should match their implementation
- Test with different identifier types to catch cross-identifier lookup issues

## Proto Message Conversion

Use `util/protohelper` for conversions:
```go
import "github.com/xiaonanln/goverse/util/protohelper"
protohelper.MsgToAny(msg)      // proto.Message -> anypb.Any
protohelper.AnyToMsg(anyMsg)   // anypb.Any -> proto.Message
protohelper.MsgToBytes(msg)    // proto.Message -> []byte
protohelper.BytesToMsg(data)   // []byte -> proto.Message
protohelper.AnyToBytes(anyMsg) // anypb.Any -> []byte
protohelper.BytesToAny(data)   // []byte -> anypb.Any
```

## Testing

**Coverage targets**: util/object/client 100%, cluster 90%+. Write essential tests, do not over-test. Focus on critical paths and edge cases.

### etcd Tests

Tests auto-skip if etcd unavailable. Always use `testutil.PrepareEtcdPrefix(t, "localhost:2379")` for test isolation.

### Integration Test Readiness

Use `testutil.WaitForClustersReady()` to ensure all clusters are fully ready before assertions. Preferred over multiple `WaitForClusterReady()` calls.

### Dynamic Ports (CRITICAL)

**Never use hardcoded ports when starting actual servers in tests.** Always use `testutil.GetFreeAddress()`. Hardcoded ports are only OK when no server actually binds to them (routing tests, etc.).

### Shard Configuration

- **Production**: 8192 shards (default)
- **Tests**: 64 shards via `testutil.TestNumShards` - always use this constant
- Valid test shard IDs: 0-63
- Use `testutil.GetObjectIDForShard()` for shard-specific object IDs

### Mock Helpers

- **Persistence**: Use `node.NewMockPersistenceProvider()` - only access via thread-safe methods (`HasStoredData`, `GetStoredData`, `GetStorageCount`). Never access `provider.storage` directly.
- **Distributed tests**: Use `testutil.NewMockGoverseServer()` + `testutil.NewTestServerHelper()` for node-to-node gRPC.

### Test Conventions

- Use `t.Fatalf()` not `t.Errorf()` (fail fast)
- Exception: Use `t.Error*()` from goroutines
- Table-driven tests with `t.Run()`
- Skip long tests (>10s) in short mode: `if testing.Short() { t.Skip() }`

## Inspector UI (cmd/inspector/web)

- Data flow: Backend -> SSE events -> JS `graphData` -> D3 node objects -> D3 rendering
- JSON from backend uses snake_case; JS uses camelCase - verify both sides match
- D3 `.data()` with key function keeps OLD bound data on existing elements; look up fresh data from `graphData` source array or force re-bind
- When adding new object fields: update both `buildGraphNodesAndLinks()` and `updateGraphIncremental()`

## Key Documentation

- **docs/RELIABLE_CALLS_DESIGN.md** - Exactly-once call semantics design
- **docs/GET_STARTED.md** - Core concepts and quick start
- **docs/GOVERSEAPI.md** - Public API reference
- **docs/CONFIGURATION.md** - YAML configuration reference
- **docs/design/HTTP_GATE.md** - REST/HTTP endpoint details
