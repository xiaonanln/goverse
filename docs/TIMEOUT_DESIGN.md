# CallObject/CreateObject Timeout Design

## Overview

This document describes the timeout handling design for operations in Goverse, ensuring operations don't hang indefinitely and fail gracefully when timeouts occur.

**Operations requiring timeouts:**
- `CallObject` - RPC calls to object methods
- `CreateObject` - Object creation and activation
- `DeleteObject` - Object deletion and cleanup
- `RegisterGate` - Gate registration streaming RPC
- Etcd operations (reads, writes, watches)
- gRPC connection establishment

**Operations NOT requiring timeouts:**
- `PushMessageToClient` - Non-blocking operation using buffered channels with `select { case ... default: }` pattern; returns immediately if channel is full

## Goals

1. **Prevent indefinite hangs** - All operations must have reasonable time limits
2. **Configurable timeouts** - Allow users to adjust timeouts based on their needs
3. **Graceful degradation** - Timeout failures should be clear and actionable
4. **Distributed timeout propagation** - Timeouts must work across node boundaries
5. **Cancel in-flight operations** - Cleanup resources when timeouts occur

## Non-Goals

- Automatic retry logic (out of scope for timeout design)
- Timeout adjustment based on historical performance
- Fine-grained timeout control per object type (use context deadlines instead)

## Current State

Currently, operations rely on context deadlines:
- Client sets context timeout: `ctx, cancel := context.WithTimeout(ctx, 30*time.Second)`
- Context propagates through the call chain
- Operations check `ctx.Done()` periodically

**Problems:**
1. No default timeout if client doesn't set one
2. Timeout values inconsistent across codebase
3. No guidance on appropriate timeout values
4. No timeout-specific error types

## Proposed Design

### 1. Configuration-Based Default Timeouts

Add timeout configuration fields to `ServerConfig`, `GateServerConfig`, and `ClusterConfig`:
- `DefaultCallTimeout`, `DefaultCreateTimeout`, `DefaultDeleteTimeout` for main operations
- `EtcdTimeout`, `ConnectionTimeout` for infrastructure
- Hardcoded fallback defaults if not configured (CallObject: 30s, CreateObject: 10s, etc.)

### 2. Automatic Deadline Enforcement

Wrap public APIs to apply default timeouts when context has no deadline:
```go
if _, hasDeadline := ctx.Deadline(); !hasDeadline {
    ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
    defer cancel()
}
```

Apply at entry points: `Cluster.CallObject()`, `Cluster.CreateObject()`, `Gate.CallObject()`, etc.

### 3. Timeout Error Types

Create `TimeoutError` struct with operation type, object ID, and wrapped context error:
- Allows distinguishing timeouts from other errors
- Provides context for debugging (which operation, which object)
- Use `errors.Is(err, context.DeadlineExceeded)` for detection

### 4. Distributed Timeout Propagation

Ensure context deadlines propagate correctly:
- gRPC automatically propagates context deadlines
- Use `grpc.WaitForReady(false)` to fail fast on timeouts
- Node-to-node calls use slightly shorter timeouts than client calls

### 5. Object Method Timeout Support

Add `BaseObject.CheckTimeout(ctx)` helper for object methods to periodically check timeout:
- Call before/after expensive operations
- Enables graceful early exit instead of waiting for full timeout

### 6. Recommended Timeout Values

**Default timeouts:**
- **CallObject**: 30 seconds (most object methods should complete quickly)
- **CreateObject**: 10 seconds (object creation should be fast)
- **DeleteObject**: 10 seconds (deletion should be fast)
- **Node-to-node calls**: 25 seconds (slightly less than CallObject to allow propagation)
- **Etcd operations**: 60 seconds (allows for transient network issues and cluster recovery)
- **gRPC connection**: 30 seconds (connection establishment)
- **Maximum timeout**: 5 minutes (safety limit)

**No timeout needed:**
- **PushMessageToClient**: Non-blocking - uses buffered channel with immediate return on full buffer

**Guidelines for users:**
- Simple queries: 5-10 seconds
- Standard operations: 30 seconds (default)
- Complex operations: 1-2 minutes
- Long-running operations: Use async pattern instead of long timeout
- Infrastructure operations (etcd, connections): 30-60 seconds

**Note:** `PushMessageToClient` is non-blocking and does not require timeout configuration.

### 7. Monitoring and Metrics

Add Prometheus metrics:
- `goverse_operation_timeouts_total` counter (by operation, node)
- `goverse_operation_duration_seconds` histogram (by operation, node, success)
- Enables tracking timeout rates and operation latency

## Implementation Plan

### Priority Order

**Start with these (highest impact, most common):**

1. **CallObject** - Most frequently used operation, affects all object interactions
   - Add deadline enforcement to `Cluster.CallObject()`
   - Benefits: Prevents hanging on slow/stuck object methods
   - Risk: High - can cause indefinite hangs in production

2. **CreateObject** - Second most common, blocks activation pipeline
   - Add deadline enforcement to `Cluster.CreateObject()`
   - Benefits: Prevents hanging during object activation
   - Risk: High - creation failures can cascade

3. **Etcd operations** - Infrastructure dependency, affects all operations
   - Add timeouts to etcd reads/writes in `EtcdManager`
   - Benefits: Prevents cluster-wide hangs on etcd issues
   - Risk: Critical - etcd hangs block everything

**Then move to these:**

4. **DeleteObject** - Less frequent but important for cleanup
5. **gRPC connection establishment** - Affects startup and node discovery
6. **RegisterGate** - Only affects gate startup/reconnection

**No timeout needed:**
- **PushMessageToClient** - Already non-blocking (uses buffered channel with immediate error on full buffer)

### Detailed Implementation Phases

### Phase 1: Configuration (Week 1)
- [ ] Add timeout configuration fields to ServerConfig, GateServerConfig, ClusterConfig
- [ ] Add default timeout constants
- [ ] Update configuration validation

### Phase 2: Error Types (Week 1)
- [ ] Create `util/errors` package
- [ ] Define TimeoutError type
- [ ] Add IsTimeout helper function

### Phase 3: Enforcement (Week 2)
- [ ] **Priority 1:** Add deadline enforcement to Cluster.CallObject
- [ ] **Priority 2:** Add deadline enforcement to Cluster.CreateObject
- [ ] **Priority 3:** Add timeout to etcd operations
- [ ] Add deadline enforcement to Cluster.DeleteObject
- [ ] Add deadline enforcement to Gate operations
- [ ] Update node-to-node call timeout handling
- [ ] Add timeout to gRPC connection establishment

Note: `PushMessageToClient` does not need deadline enforcement as it is already non-blocking.

### Phase 4: Object Support (Week 2)
- [ ] Add CheckTimeout helper to BaseObject
- [ ] Document timeout best practices for object methods
- [ ] Update examples to use timeouts

### Phase 5: Metrics (Week 3)
- [ ] Add timeout counter metrics
- [ ] Add operation duration histograms
- [ ] Update Prometheus integration docs

### Phase 6: Testing (Week 3)
- [ ] Add timeout tests for CallObject
- [ ] Add timeout tests for CreateObject
- [ ] Add timeout tests for DeleteObject
- [ ] Add timeout propagation tests (node-to-node)
- [ ] Add gate timeout tests
- [ ] Add etcd timeout tests
- [ ] Add connection timeout tests

Note: No timeout tests needed for `PushMessageToClient` as it is non-blocking.

## Testing Strategy

### Unit Tests
```go
func TestCallObjectTimeout(t *testing.T) {
    // Test that CallObject respects context timeout
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    
    // Call an object method that takes longer than timeout
    _, err := cluster.CallObject(ctx, "SlowObject", "id", "SlowMethod", req)
    
    if !errors.IsTimeout(err) {
        t.Fatalf("Expected timeout error, got: %v", err)
    }
}

func TestDefaultTimeout(t *testing.T) {
    // Test that default timeout is applied when context has no deadline
    ctx := context.Background() // No deadline
    
    config := &cluster.Config{
        DefaultCallTimeout: 5 * time.Second,
    }
    
    // Verify timeout is applied
    // ...
}
```

### Integration Tests
```go
func TestTimeoutPropagation(t *testing.T) {
    // Test that timeout propagates across node boundaries
    // Node1 calls Node2, timeout should propagate
}

func TestGateTimeout(t *testing.T) {
    // Test that gate applies timeout to client requests
}
```

## Backward Compatibility

- Existing code that sets context timeouts will continue to work
- Default timeouts only apply when no deadline is set
- Configuration fields are optional (use defaults if not set)
- No breaking changes to existing APIs

## Open Questions

1. **Should we expose timeout configuration via gRPC metadata?**
   - Allows clients to override default timeouts per-request
   - Pro: Flexibility for special cases
   - Con: Complexity, potential for abuse

2. **Should we implement timeout budget tracking?**
   - Track remaining time as request flows through system
   - Pro: Better timeout allocation in deep call chains
   - Con: Implementation complexity

3. **Should we add timeout warnings?**
   - Log warning when operation approaches timeout (e.g., 90% of deadline)
   - Pro: Early detection of slow operations
   - Con: Log noise

## References

- [Google SRE Book - Handling Overload](https://sre.google/sre-book/handling-overload/)
- [gRPC Deadlines](https://grpc.io/blog/deadlines/)
- [Go Context Timeouts](https://go.dev/blog/context)
