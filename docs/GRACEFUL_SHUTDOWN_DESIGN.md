# Graceful Shutdown Mode Design

## Problem Statement

Currently, GoVerse nodes have only one shutdown behavior: they immediately stop, keeping their shard assignments and node registration in etcd. This works well for rolling updates where the node will restart and reclaim its shards, but it's suboptimal for scale-down operations where the node won't return.

For scale-down, keeping shard assignments in etcd means:
1. Other nodes can't immediately take over the shards
2. The leader must detect the node failure and reassign shards
3. Recovery time is longer than necessary

We need two distinct shutdown modes:
1. **Quick shutdown** (current behavior): Keep assignments for fast restart
2. **Clean shutdown**: Proactively release shards for faster cluster recovery

## Shutdown Modes

### Quick Shutdown (Default)
- **Use case**: Rolling updates, node restarts, maintenance
- **Behavior**: Stop immediately, keep shard assignments and node registration in etcd
- **Recovery**: Node can reclaim its shards quickly on restart
- **Trade-off**: Temporary unavailability until leader reassigns dead node's shards

### Clean Shutdown (Graceful)
- **Use case**: Scale-down, permanent node removal
- **Behavior**: Release all owned shards → wait for handoff → unregister node → exit
- **Recovery**: Other nodes can immediately claim released shards
- **Trade-off**: Slower shutdown process, but faster cluster recovery

## API Design

### ShutdownMode Type
```go
package consensusmanager

type ShutdownMode int

const (
    ShutdownModeQuick ShutdownMode = iota
    ShutdownModeClean
)

func (m ShutdownMode) String() string {
    switch m {
    case ShutdownModeQuick:
        return "quick"
    case ShutdownModeClean:
        return "clean"  
    default:
        return "unknown"
    }
}
```

### ConsensusManager API
```go
// Shutdown gracefully shuts down the consensus manager with the specified mode
func (cm *ConsensusManager) Shutdown(ctx context.Context, mode ShutdownMode) error
```

### Integration Points
- `cluster.Cluster.Stop()` will accept a mode parameter
- `server.Server.Run()` signal handler will use ShutdownModeQuick by default
- Environment variable `GOVERSE_SHUTDOWN_MODE=clean` can override the default
- Future: CLI flag `--shutdown-mode=clean` for explicit control

## Implementation Plan

### Phase 1: Core Infrastructure (This PR)
**Goal**: Add shutdown mode support without changing existing signal handling

**Changes**:
1. **Add ShutdownMode type** in `cluster/consensusmanager/consensusmanager.go`
2. **Add Shutdown method** to ConsensusManager:
   ```go
   func (cm *ConsensusManager) Shutdown(ctx context.Context, mode ShutdownMode) error
   ```
3. **Implement clean shutdown logic**:
   - Release all shards owned by this node (set CurrentNode to empty)
   - Wait for other nodes to claim them (with timeout)
   - Only then allow normal unregistration to proceed
4. **Add comprehensive tests** for shutdown modes
5. **Update cluster.Stop()** to accept shutdown mode parameter

**NOT in scope for this PR**:
- Signal handling changes (still uses current behavior)
- CLI flags or environment variables 
- Gate server shutdown changes

### Phase 2: Signal Integration (Follow-up PR)
- Environment variable support for `GOVERSE_SHUTDOWN_MODE`
- Signal handling changes in `server/server.go`
- CLI flag support in server config
- Gate server shutdown mode support

### Phase 3: Advanced Features (Future PRs)  
- Timeout configuration for shard release
- Metrics for shutdown operations
- Kubernetes integration guidance

## Sequence Diagrams

### Current Shutdown (Quick Mode)
```
Node                ConsensusManager         etcd                Other Nodes
 |                        |                   |                      |
 | SIGTERM               |                   |                      |
 |-----> Stop()           |                   |                      |
 |       |                |                   |                      |
 |       |---> StopWatch() |                   |                      |
 |       |---> unregister  |                   |                      |
 |       |                |---> DELETE /nodes/addr                   |
 |       |                |                   |                      |
 | Exit  |                |                   |                      |
 |                        |                   |                      |
 |                        |                   |                      |
 |                        |                   | Leader detects        |
 |                        |                   | dead node and         |
 |                        |                   | reassigns shards      |
 |                        |                   |<--------------------- |
```

### Proposed Clean Shutdown
```
Node                ConsensusManager         etcd                Other Nodes  
 |                        |                   |                      |
 | SIGTERM               |                   |                      |
 |-----> Stop(Clean)      |                   |                      |
 |       |                |                   |                      |
 |       |---> Shutdown(Clean)                |                      |
 |       |                |                   |                      |
 |       |                | Release shards    |                      |
 |       |                |---> PUT /shard/N (CurrentNode="")        |
 |       |                |                   |---> Watch event ---->|
 |       |                |                   |                      |
 |       |                | Wait for claim    |                      |
 |       |                |<--- GET /shard/N <----|<--- Claim shard  |
 |       |                |                   |<--- PUT /shard/N ----|
 |       |                |                   |                      |
 |       |---> StopWatch() |                   |                      |
 |       |---> unregister  |                   |                      |
 |       |                |---> DELETE /nodes/addr                   |
 | Exit  |                |                   |                      |
```

## Error Handling

### Timeout Scenarios
- **Shard release timeout**: If other nodes don't claim released shards within timeout, proceed with unregistration anyway
- **Context cancellation**: If shutdown context is cancelled, immediately proceed to unregistration
- **etcd errors**: Log errors but continue with shutdown process

### Partial Failures
- **Some shards fail to release**: Log errors, continue with remaining shards
- **Some shards not claimed**: After timeout, proceed with unregistration
- **Network partition**: Shutdown should complete locally even if etcd is unreachable

## Configuration

### Default Behavior
- Current behavior unchanged: Quick shutdown by default
- No breaking changes to existing APIs

### Environment Variables (Phase 2)
```bash
# Override default shutdown mode
GOVERSE_SHUTDOWN_MODE=clean

# Timeout for clean shutdown shard release
GOVERSE_CLEAN_SHUTDOWN_TIMEOUT=30s
```

### Config File Support (Phase 2)
```yaml
cluster:
  shutdown_mode: clean
  clean_shutdown_timeout: 30s
```

## Testing Strategy

### Unit Tests
- `TestConsensusManager_Shutdown_QuickMode`
- `TestConsensusManager_Shutdown_CleanMode`
- `TestConsensusManager_Shutdown_Timeout`
- `TestConsensusManager_Shutdown_PartialFailure`

### Integration Tests
- Multi-node cluster with real etcd
- Verify shard migration during clean shutdown
- Verify other nodes claim released shards
- Verify timeout behavior

### Error Injection Tests
- etcd unavailable during shutdown
- Context cancellation during shutdown
- Network partitions during shutdown

## Metrics and Observability

### Metrics (Future Enhancement)
```
goverse_shutdown_total{mode="clean|quick", status="success|timeout|error"}
goverse_shutdown_duration_seconds{mode="clean|quick"}  
goverse_shards_released_total{node}
goverse_clean_shutdown_timeouts_total
```

### Logging
- INFO: Shutdown mode selection
- INFO: Shard release progress 
- WARN: Shard release timeouts
- ERROR: Shutdown failures

## Migration and Compatibility

### Backward Compatibility
- Existing `cluster.Stop()` calls use Quick mode (no behavior change)
- No changes to public APIs in Phase 1
- All existing tests pass unchanged

### Migration Path
1. Deploy Phase 1 changes (infrastructure only)
2. Test clean shutdown manually via API
3. Deploy Phase 2 (environment variable support)
4. Gradually enable clean shutdown for scale-down scenarios
5. Add to Kubernetes deployment guides

## Future Enhancements

### Advanced Shutdown Coordination
- Graceful connection draining before shard release
- Coordinated shutdown for batch operations
- Rolling shutdown with min-replicas enforcement

### Kubernetes Integration
- Pod deletion hooks to trigger clean shutdown
- PreStop lifecycle hooks
- Shutdown mode detection via Kubernetes annotations

### Multi-Shard Atomic Release
- Release shards in batches for faster handoff
- Priority-based shard release ordering
- Load-aware shard assignment during shutdown

## References

- SHARDING_TODO.md P1: "Add optional shard release for permanent shutdown"
- MILESTONE_V0.1.0.md: "Graceful Shutdown Mode (P1)"
- cluster/consensusmanager/consensusmanager.go: Shard management logic
- server/server.go: Current signal handling
- cluster/cluster.go: Current Stop() implementation