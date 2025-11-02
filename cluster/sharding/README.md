# Cluster Sharding

This package implements the shard-to-node mapping functionality for the Goverse cluster with support for shard migration and state tracking.

## Overview

Goverse uses a fixed sharding model with **8192 shards**. Each object is deterministically mapped to a shard using FNV-1a hashing, and shards are distributed across cluster nodes. The cluster leader is responsible for creating and maintaining the shard-to-node mapping in etcd.

### Enhanced Shard State Management

Each shard tracks detailed state information including:
- **Target Node**: The node where the shard should ultimately reside (leader-specified)
- **Current Node**: The node where the shard is currently located
- **State**: The current state of the shard (AVAILABLE, MIGRATING, or OFFLINE)

This design enables controlled shard migration when nodes join or leave the cluster, ensuring data consistency and preventing race conditions.

## Components

### Shard ID Calculation

```go
import "github.com/xiaonanln/goverse/cluster/sharding"

// Hash an object ID to a shard (0-8191)
shardID := sharding.GetShardID("object-123")
```

### Shard States

```go
// ShardStateAvailable: Shard is ready and available for operations on the current node
// ShardStateMigrating: Shard is in the process of being migrated
// ShardStateOffline: Shard is offline and not available on any node
```

### Shard Mapper

The `ShardMapper` manages the mapping of shards to nodes with migration support:

```go
// Create a shard mapper (typically done by the cluster)
mapper := sharding.NewShardMapper(etcdManager)

// Leader creates initial mapping
nodes := []string{"node1", "node2", "node3"}
mapping, err := mapper.CreateShardMapping(ctx, nodes)
if err != nil {
    log.Fatal(err)
}

// Store mapping in etcd
err = mapper.StoreShardMapping(ctx, mapping)
if err != nil {
    log.Fatal(err)
}

// Get node for an object (returns current node location)
node, err := mapper.GetNodeForObject(ctx, "object-123")
if err != nil {
    log.Fatal(err)
}

// Get detailed shard information
shardID := sharding.GetShardID("object-123")
shardInfo, err := mapper.GetShardInfo(ctx, shardID)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Shard %d: target=%s, current=%s, state=%s\n",
    shardInfo.ShardID, shardInfo.TargetNode, shardInfo.CurrentNode, shardInfo.State)
```

### Shard Migration

The package provides methods for coordinated shard migration:

```go
// Mark a shard for migration
err := mapper.MarkShardForMigration(ctx, shardID)

// Update shard state during migration
err := mapper.UpdateShardState(ctx, shardID, sharding.ShardStateMigrating, "node2")

// Complete migration (marks shard as AVAILABLE on target node)
err := mapper.CompleteMigration(ctx, shardID)

// Query shards by state
migratingShards, err := mapper.GetShardsInState(ctx, sharding.ShardStateMigrating)

// Query shards on a specific node
nodeShards, err := mapper.GetShardsOnNode(ctx, "node1")
```

## Cluster Integration

The cluster package provides high-level methods for shard mapping:

```go
import "github.com/xiaonanln/goverse/cluster"

c := cluster.Get()

// Check if this node is the leader
if c.IsLeader() {
    // Initialize shard mapping (first time)
    err := c.InitializeShardMapping(ctx)
    if err != nil {
        log.Fatal(err)
    }
    
    // Or update when nodes change
    err = c.UpdateShardMapping(ctx)
    if err != nil {
        log.Fatal(err)
    }
}

// Get node for an object (any node can call this)
node, err := c.GetNodeForObject(ctx, "object-123")
if err != nil {
    log.Fatal(err)
}
```

## Distribution Algorithm

### Initial Mapping

When creating a shard mapping:
1. Nodes are sorted lexicographically for determinism
2. Shards are assigned using round-robin: `node = sortedNodes[shardID % len(nodes)]`
   - Shard 0 → node at index (0 % 3) = 0
   - Shard 1 → node at index (1 % 3) = 1
   - Shard 2 → node at index (2 % 3) = 2
   - Shard 3 → node at index (3 % 3) = 0 (back to first node)
   - And so on...
3. This ensures even distribution across all nodes

### Updates

When the node list changes:
1. Existing shard-to-node assignments are preserved when possible
2. Shards on removed nodes are marked with new target nodes and may enter MIGRATING or OFFLINE state
3. New nodes don't receive shards until explicitly redistributed
4. Shards where the current node is still available but target node changed are marked as MIGRATING

### Shard Migration Process

When a shard needs to migrate from one node to another:

1. **Identification**: The leader detects that a shard's current node differs from its target node
2. **Marking**: The shard is marked as MIGRATING state
3. **Cleanup**: The source node completes all operations on objects in that shard
4. **Transfer**: Object state is transferred to the target node (TODO: implement actual transfer)
5. **Completion**: After successful transfer, `CompleteMigration()` marks the shard as AVAILABLE on the target node

During migration:
- Objects can still be accessed on the current node
- New object creations are directed to the target node
- The migration state is persisted in etcd for coordination

### Example Distribution

With 3 nodes (node1, node2, node3) and 8192 shards:
- node1: shards 0, 3, 6, 9, ... (2731 shards)
- node2: shards 1, 4, 7, 10, ... (2731 shards)
- node3: shards 2, 5, 8, 11, ... (2730 shards)

Total: 8192 shards (2731 + 2731 + 2730)

## Storage Format

Shard mappings are stored in etcd at `/goverse/shardmapping` using Protocol Buffers:

```protobuf
message ShardInfo {
  int32 shard_id = 1;
  string target_node = 2;     // Where the shard should be
  string current_node = 3;    // Where the shard currently is
  ShardState state = 4;       // AVAILABLE, MIGRATING, or OFFLINE
}

message ShardMappingProto {
  map<int32, ShardInfo> shards = 1;
  repeated string nodes = 2;
  int64 version = 3;
}
```

This enhanced format enables:
- Tracking of shard location during migration
- State-based queries and filtering
- Atomic state transitions coordinated via etcd

## Leader Responsibilities

The cluster leader (node with smallest address) is responsible for:
1. Creating the initial shard mapping when the cluster starts
2. Updating the mapping when nodes join or leave
3. Managing shard state transitions (AVAILABLE → MIGRATING → AVAILABLE)
4. Storing the mapping in etcd for other nodes to read
5. Coordinating shard migrations to ensure data consistency

## Migration Coordination

The shard migration process ensures:
- **Atomic state transitions**: State changes are persisted in etcd before taking effect
- **No data loss**: Objects remain accessible on the current node during migration
- **Clean handoff**: Source node must complete cleanup before target node takes ownership
- **Fault tolerance**: Migration state survives node failures and can be resumed

## Performance

- Local caching: Mappings are cached in memory after first read
- Cache invalidation: Use `InvalidateCache()` to force reload from etcd
- Lookup complexity: O(1) for both GetNodeForShard and GetNodeForObject
- State queries: O(n) scan over all shards, results are sorted

## Testing

Run tests with:
```bash
go test ./cluster/sharding/...
```

Tests cover:
- Shard ID calculation and distribution
- Mapping creation and updates
- Node addition and removal
- Shard state transitions (AVAILABLE, MIGRATING, OFFLINE)
- Migration workflow (mark, update state, complete)
- State-based queries (GetShardsInState, GetShardsOnNode)
- Protobuf serialization and deserialization
- Caching behavior
- Edge cases and error handling

## Migration TODO

The current implementation provides the framework for shard migration. The following features are planned:

1. **Actual object transfer**: Implement the transfer of object state from source to target node
2. **Background migration**: Automatically migrate shards when nodes join/leave
3. **Migration throttling**: Rate-limit migrations to avoid overloading the cluster
4. **Progress tracking**: Monitor and report migration progress
5. **Rollback support**: Handle migration failures gracefully
