# ConsensusManager

The `ConsensusManager` is a component that centralizes all etcd interactions for the Goverse cluster, maintaining an in-memory view of the cluster state and notifying components of changes in real-time.

## Overview

The ConsensusManager replaces direct etcd access patterns throughout the cluster code with a unified, efficient approach:

- **Single etcd watch**: Monitors the entire `/goverse` prefix for all state changes
- **In-memory state**: Maintains current node list and shard mapping without repeated etcd reads
- **Real-time notifications**: Alerts listeners when nodes join/leave or shard mapping updates
- **Thread-safe**: All operations are protected with appropriate locking mechanisms

## Key Features

### 1. Unified Watch Mechanism

Instead of multiple watches or polling:
```go
// Initialize and start watching
cm.Initialize(ctx)
cm.StartWatch(ctx)
```

This establishes a single watch on the `/goverse` prefix that captures:
- Node additions/removals (`/goverse/nodes/...`)
- Shard mapping updates (`/goverse/shardmapping`)

### 2. In-Memory State Management

The ConsensusManager maintains:
- **Node list**: Currently registered nodes with timestamps of last change
- **Shard mapping**: Current shard-to-node assignments
- **Stability tracking**: When the node list last changed (for leader decisions)

### 3. State Change Notifications

Components can register as listeners to receive callbacks:
```go
type StateChangeListener interface {
    OnNodesChanged(nodes []string)
    OnShardMappingChanged(mapping *sharding.ShardMapping)
}

cm.AddListener(myListener)
```

### 4. Thread-Safe Operations

All public methods are thread-safe:
```go
// Safe to call from multiple goroutines
nodes := cm.GetNodes()
leader := cm.GetLeaderNode()
mapping, _ := cm.GetShardMapping()
```

## Architecture

### Initialization Flow

1. **Create**: `cm := NewConsensusManager(etcdManager)`
2. **Initialize**: `cm.Initialize(ctx)` - Loads initial state from etcd
3. **Watch**: `cm.StartWatch(ctx)` - Begins watching for changes

### Watch Event Processing

```
etcd watch → event received → handler processes
                               ↓
                      update in-memory state
                               ↓
                      notify all listeners
```

### State Queries

All queries are served from in-memory state (no etcd round-trip):
- `GetNodes()` - List of registered nodes
- `GetLeaderNode()` - Current leader (lexicographically smallest node)
- `GetShardMapping()` - Current shard assignments
- `GetNodeForObject(objectID)` - Which node currently handles this object (uses CurrentNode only; fails if not set or not alive)
- `GetNodeForShard(shardID)` - Which node currently owns this shard (uses CurrentNode only; fails if not set or not alive)

### State Modifications (Leader only)

The leader can modify cluster state:
- `CreateShardMapping()` - Generate initial mapping
- `UpdateShardMapping()` - Update mapping when nodes change
- `storeShardMapping(ctx, mapping)` - Persist to etcd

## Shard Migration and CurrentNode vs TargetNode

Each shard has two node references:

- **TargetNode**: The node that should handle this shard (desired state) - used for planning/migration only
- **CurrentNode**: The node that currently handles this shard (actual state) - used for object routing

**Important**: `GetNodeForObject` and `GetNodeForShard` use **only** `CurrentNode` for routing. They never fall back to `TargetNode`. This ensures:

1. **Correct routing**: Objects are always routed to where they actually exist
2. **Fail-fast on unclaimed shards**: If `CurrentNode` is not set, the lookup fails immediately
3. **Fail-fast on node failure**: If `CurrentNode` is not in the active node list, the lookup fails immediately
4. **Clear separation**: `TargetNode` is for planning/migration; `CurrentNode` is for routing

Example lifecycle:
```go
// Initial state: Shard not yet claimed
ShardInfo{TargetNode: "node1", CurrentNode: ""}
GetNodeForShard(shardID) // Error: "shard X has no current node (not yet claimed)"

// Node claims shard
ShardInfo{TargetNode: "node1", CurrentNode: "node1"}
GetNodeForShard(shardID) // Returns: "node1"

// During migration: Leader updates TargetNode
ShardInfo{TargetNode: "node2", CurrentNode: "node1"}
GetNodeForShard(shardID) // Returns: "node1" (still at old location)

// Migration complete: Node2 claims ownership
ShardInfo{TargetNode: "node2", CurrentNode: "node2"}
GetNodeForShard(shardID) // Returns: "node2"

// Node failure: Node2 leaves cluster
ShardInfo{TargetNode: "node2", CurrentNode: "node2"}  // Node2 removed from active nodes
GetNodeForShard(shardID) // Error: "current node node2 for shard X is not in active node list"
```

Queries **always require CurrentNode to be set and alive**. There is no fallback to `TargetNode` - this ensures objects are never routed incorrectly.

## Usage Example

```go
// Setup
etcdMgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/goverse")
etcdMgr.Connect()

cm := consensusmanager.NewConsensusManager(etcdMgr)

// Initialize from etcd
err := cm.Initialize(ctx)
if err != nil {
    log.Fatal(err)
}

// Start watching for changes
err = cm.StartWatch(ctx)
if err != nil {
    log.Fatal(err)
}

// Query state
nodes := cm.GetNodes()
leader := cm.GetLeaderNode()

// Leader manages shard mapping
if leader == myNodeAddress {
    mapping, _ := cm.CreateShardMapping()
    cm.storeShardMapping(ctx, mapping)
}

// Find which node handles an object
node, _ := cm.GetNodeForObject("user-12345")

// Cleanup
cm.StopWatch()
```

## Integration with Cluster

The `Cluster` type now uses `ConsensusManager` internally:

```go
// Old approach (multiple etcd interactions)
nodes := c.etcdManager.GetNodes()
mapping, _ := c.shardMapper.GetShardMapping(ctx)

// New approach (single in-memory source)
nodes := c.GetNodes()        // delegates to ConsensusManager
mapping, _ := c.GetShardMapping(ctx)  // no etcd call
```

## Benefits

### Performance
- **Reduced etcd load**: Single watch instead of multiple polls/reads
- **No latency**: Queries served from memory, no network round-trips
- **Efficient updates**: Incremental changes via watch events

### Consistency
- **Single source of truth**: All components see the same state
- **Real-time updates**: Changes propagate immediately via notifications
- **Atomic view**: State updates are atomic and consistent

### Maintainability
- **Centralized logic**: All etcd interaction in one place
- **Clear responsibilities**: ConsensusManager owns cluster state
- **Easier testing**: Mock ConsensusManager instead of etcd

## Implementation Details

### Thread Safety

Uses `sync.RWMutex` for:
- Read operations (GetNodes, GetLeaderNode, etc.) use `RLock()`
- Write operations (state updates from watch) use `Lock()`

### Notification Pattern

Listeners are notified asynchronously:
```go
go cm.notifyNodesChanged()
go cm.notifyShardMappingChanged(mapping)
```

This prevents deadlocks and ensures the watch loop remains responsive.

### Error Handling

- Watch errors are logged but don't crash the watch loop
- Connection failures trigger reconnection (handled by etcd client)
- State queries return errors if state is unavailable

## Testing

See `consensusmanager_test.go` for comprehensive unit tests covering:
- State management
- Listener notifications
- Shard mapping creation/updates
- Thread safety
- Error conditions

## Migration Notes

When migrating code to use ConsensusManager:

1. Replace `etcdManager.GetNodes()` with `consensusManager.GetNodes()`
2. Replace `shardMapper.GetShardMapping(ctx)` with `consensusManager.GetShardMapping()`
3. Replace `shardMapper.InvalidateCache()` calls (now automatic via watch)
4. Remove manual etcd polling loops

## Future Enhancements

Potential improvements:
- [ ] Metrics for state updates and query latency
- [ ] Configurable watch retry policies
- [ ] State snapshots for debugging
- [ ] Listener priority/ordering
