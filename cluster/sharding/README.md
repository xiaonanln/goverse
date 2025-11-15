# Cluster Sharding

This package implements the shard-to-node mapping functionality for the Goverse cluster.

## Overview

Goverse uses a fixed sharding model with **8192 shards**. Each object is deterministically mapped to a shard using FNV-1a hashing, and shards are distributed across cluster nodes. The cluster leader is responsible for creating and maintaining the shard-to-node mapping in etcd.

## Components

### Shard ID Calculation

```go
import "github.com/xiaonanln/goverse/cluster/sharding"

// Hash an object ID to a shard (0-8191)
shardID := sharding.GetShardID("object-123")
```

### Shard Mapper

The `ShardMapper` manages the mapping of shards to nodes:

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
err = mapper.storeShardMapping(ctx, mapping)
if err != nil {
    log.Fatal(err)
}

// Get node for an object
node, err := mapper.GetCurrentNodeForObject(ctx, "object-123")
if err != nil {
    log.Fatal(err)
}
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
node, err := c.GetCurrentNodeForObject(ctx, "object-123")
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
2. Only shards on removed nodes are reassigned
3. New nodes don't receive shards until explicitly redistributed

### Example Distribution

With 3 nodes (node1, node2, node3) and 8192 shards:
- node1: shards 0, 3, 6, 9, ... (2731 shards)
- node2: shards 1, 4, 7, 10, ... (2731 shards)
- node3: shards 2, 5, 8, 11, ... (2730 shards)

Total: 8192 shards (2731 + 2731 + 2730)

## Storage Format

Shard mappings are stored in etcd as individual keys, one for each shard:

- **Key format**: `/goverse/shard/<shard-id>` (e.g., `/goverse/shard/0`, `/goverse/shard/1`, etc.)
- **Value**: Node address as a string (e.g., `"localhost:47001"`)

Each of the 8192 shards has its own key in etcd. This allows for:
- More granular watching and updates
- Better scalability for large clusters
- Reduced network overhead when only specific shards change

Example etcd keys:
```
/goverse/shard/0 = "localhost:47001"
/goverse/shard/1 = "localhost:47002"
/goverse/shard/2 = "localhost:47001"
...
/goverse/shard/8191 = "localhost:47002"
```

**Note**: The ConsensusManager encapsulates all complexity of reading and writing individual shard keys. Users of the cluster package don't need to interact with these keys directly.

## Leader Responsibilities

The cluster leader (node with smallest address) is responsible for:
1. Creating the initial shard mapping when the cluster starts
2. Updating the mapping when nodes join or leave
3. Storing the mapping in etcd for other nodes to read

## Performance

- Local caching: Mappings are cached in memory after first read
- Cache invalidation: Use `InvalidateCache()` to force reload from etcd
- Lookup complexity: O(1) for both GetNodeForShard and GetCurrentNodeForObject

## Testing

Run tests with:
```bash
go test ./cluster/sharding/...
```

Tests cover:
- Shard ID calculation and distribution
- Mapping creation and updates
- Node addition and removal
- Serialization and caching
- Edge cases and error handling
