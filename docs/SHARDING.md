# Sharding

GoVerse uses a fixed shard model to distribute objects across cluster nodes. Every object maps to exactly one shard, and each shard is owned by exactly one node at any given time. The shard count is fixed at cluster creation and does not change at runtime.

---

## Table of Contents

- [Overview](#overview)
- [Hash Algorithm](#hash-algorithm)
- [Object ID Formats](#object-id-formats)
- [Shard-to-Node Mapping](#shard-to-node-mapping)
- [Shard Lifecycle](#shard-lifecycle)
- [Shard Locking](#shard-locking)
- [Rebalancing](#rebalancing)
- [etcd Storage Format](#etcd-storage-format)
- [Configuration](#configuration)
- [Routing](#routing)
- [Testing](#testing)
- [Key Source Files](#key-source-files)

---

## Overview

GoVerse partitions the object space into a fixed number of shards (default 8192). When a call targets an object, the runtime computes which shard owns that object and routes the call to the node currently holding that shard.

The system uses two levels of indirection:

1. **Object ID -> Shard**: deterministic hash (FNV-1a)
2. **Shard -> Node**: dynamic mapping stored in etcd

This design means object placement is entirely determined by the shard mapping. Adding or removing nodes triggers shard reassignment, not per-object migration.

---

## Hash Algorithm

Shard assignment uses FNV-1a 32-bit hashing:

```
shardID = fnv32a(objectID) % numShards
```

Implementation: `cluster/sharding/sharding.go`

```go
hasher := fnv.New32a()
hasher.Write([]byte(objectID))
hashValue := hasher.Sum32()
return int(hashValue) % numShards
```

The same object ID always produces the same shard ID, so routing is consistent without coordination.

---

## Object ID Formats

GoVerse supports three object ID formats, each with different routing behavior. Format parsing is in `util/objectid/parser.go`.

### Regular Format

Any string without a `/` character.

```
"user-12345"
"session-abc"
```

Routes via FNV-1a hash to a shard, then to that shard's owning node.

### Fixed-Shard Format

```
shard#<shardID>/<objectID>
```

Examples: `shard#5/object-123`, `shard#10/counter`

Bypasses the hash and routes directly to the specified shard. The shard ID must be in range `[0, numShards)`. This is used for auto-load objects that need one instance per shard.

### Fixed-Node Format

```
<nodeAddress>/<objectID>
```

Examples: `localhost:7001/client-123`, `10.0.0.5:47001/status`

Bypasses sharding entirely and routes directly to the specified node. Used for client objects and per-node singleton objects. Fixed-node objects also skip shard locking since they are not part of the shard system.

---

## Shard-to-Node Mapping

Each shard tracks two node assignments:

```go
type ShardInfo struct {
    TargetNode  string   // Desired owner (set by leader)
    CurrentNode string   // Actual owner (set when node claims the shard)
    Flags       []string // e.g., ["pinned"]
    ModRevision int64    // etcd revision for optimistic concurrency
}
```

**TargetNode** is the desired state set by the cluster leader. **CurrentNode** is the actual state set when a node claims ownership. The distinction enables safe, multi-step shard migration.

Routing always uses **CurrentNode**. If CurrentNode is empty or refers to a node no longer in the cluster, the call fails with an error rather than falling back to TargetNode. This ensures objects are only accessed on nodes that have actually claimed ownership.

---

## Shard Lifecycle

### Initial Assignment

When the cluster starts and a leader is elected, the leader creates the initial shard mapping using round-robin distribution over sorted node addresses:

```
shard[i].TargetNode = sortedNodes[i % len(nodes)]
```

This produces an even initial distribution.

### Claiming

Each node periodically checks for shards where `TargetNode == thisNode` and `CurrentNode` is either empty or refers to a dead node. When such shards are found, the node:

1. Acquires write locks on all target shards (sorted order, to prevent deadlock)
2. Sets `CurrentNode = thisNode` in etcd
3. Releases locks

After claiming, the node can serve objects for those shards.

### Migration (Rebalance)

When the leader decides to move a shard from Node A to Node B:

1. Leader sets `TargetNode = NodeB` (CurrentNode remains NodeA)
2. Node A detects the mismatch and evicts local objects for that shard
3. Node A releases the shard: sets `CurrentNode = ""` in etcd
4. Node B detects an unclaimed shard targeted to it and claims it: sets `CurrentNode = NodeB`

During steps 2-4, calls to objects on that shard will fail. Objects are virtual actors, so they will be reactivated on the new node when the next call arrives after migration completes.

### Node Failure

When a node leaves the cluster:

1. Leader detects the node is gone
2. Leader reassigns orphaned shards (where TargetNode is the dead node) to surviving nodes
3. Surviving nodes claim the reassigned shards (CurrentNode was pointing to the dead node, which is no longer in the active node list)

### Stability Guard

Shard operations (claiming, releasing, initial mapping creation) require the cluster state to have been stable for a configurable duration (`ClusterStateStabilityDuration`, default 10s). This prevents churn from rapid node joins and leaves.

---

## Shard Locking

The `cluster/shardlock/` package prevents race conditions between object operations and shard ownership transitions.

### Lock Types

- **Read lock** (`AcquireRead`): Acquired before object operations (create, call, delete). Multiple reads can proceed concurrently on the same shard.
- **Write lock** (`AcquireWrite`, `AcquireWriteMultiple`): Acquired during shard ownership transitions (claim, release). Exclusive with both reads and other writes.

### Deadlock Prevention

`AcquireWriteMultiple` sorts shard IDs in ascending order before acquiring locks, preventing deadlocks when multiple operations lock overlapping shard sets.

### Fixed-Node Bypass

Objects with fixed-node format IDs (containing `/`) skip shard locking entirely since they are not part of the shard system.

### Example: Why Locking Matters

Without shard locks, a race condition is possible:

```
Goroutine 1 (release):  reads shard state, decides to release shard 42
Goroutine 2 (create):   validates shard 42 ownership, starts creating object
Goroutine 1 (release):  updates etcd, releases shard 42
Result: object created on a shard this node no longer owns
```

With shard locks, Goroutine 2 blocks until the release completes, then sees the updated state and routes correctly.

---

## Rebalancing

Rebalancing is performed by the cluster leader and runs periodically. Implementation: `cluster/consensusmanager/consensusmanager.go`, `RebalanceShards()`.

### Trigger Conditions

Rebalancing occurs when both conditions are true:

1. `maxShards - minShards >= 2` (at least 2 shards difference between most-loaded and least-loaded nodes)
2. `maxShards - minShards > imbalanceThreshold * idealLoad` (the difference exceeds the configured threshold as a fraction of the ideal per-node load)

Where `idealLoad = numShards / numNodes`.

With the default threshold of 0.2 and 8192 shards across 4 nodes, the ideal load is 2048, and rebalancing triggers when the difference exceeds ~410 shards.

### Batch Size

The leader migrates up to `RebalanceShardsBatchSize` shards per cycle (default: `max(1, numShards/128)`, which is 64 for 8192 shards). After each batch, the system waits for stabilization before the next cycle.

### Pinned Shards

Shards with the `pinned` flag are excluded from rebalancing. They stay on their assigned node regardless of load imbalance.

---

## etcd Storage Format

Shard mappings are stored as individual keys in etcd.

**Key format**: `<prefix>/shard/<shardID>`

Example: `/goverse/shard/0`, `/goverse/shard/8191`

**Value format**: comma-separated fields

```
<targetNode>,<currentNode>[,f=<flag1>,f=<flag2>,...]
```

Examples:

| Key | Value | Meaning |
|-----|-------|---------|
| `/goverse/shard/0` | `node1:47001,node1:47001` | Shard 0 assigned to and claimed by node1 |
| `/goverse/shard/1` | `node2:47001,` | Shard 1 targeted to node2, not yet claimed |
| `/goverse/shard/2` | `node1:47001,node1:47001,f=pinned` | Shard 2 pinned to node1 |

Updates use etcd transactions with `ModRevision` checks for optimistic concurrency control. A worker pool (sized at `numShards/512`, minimum 1) parallelizes writes.

---

## Configuration

Sharding-related fields in `cluster.Config` (defaults from `DefaultConfig()`):

| Field | Default | Description |
|-------|---------|-------------|
| `NumShards` | 8192 | Total number of shards. Fixed at cluster creation. |
| `ClusterStateStabilityDuration` | 10s | How long node membership must be stable before shard operations proceed. |
| `ShardMappingCheckInterval` | 5s | How often nodes check for shards to claim or release. |
| `RebalanceShardsBatchSize` | `max(1, NumShards/128)` | Maximum shards migrated per rebalance cycle. |
| `ImbalanceThreshold` | 0.2 | Imbalance as fraction of ideal load before rebalancing triggers. |
| `MinQuorum` | 1 | Minimum nodes required for cluster stability. |

---

## Routing

When a call targets an object, the routing path is:

1. Parse the object ID format (regular, fixed-shard, or fixed-node)
2. For fixed-node: return the node address directly
3. For regular/fixed-shard: compute the shard ID
4. Look up `CurrentNode` for that shard
5. Validate that `CurrentNode` is non-empty and present in the active node list
6. Route the call to that node via gRPC

Gates (client-facing proxies) follow the same routing logic but never claim shards or host objects. They use the cluster's shard mapping to forward calls to the correct node.

If a shard is in the middle of migration (CurrentNode is empty or refers to a dead node), calls fail immediately. Callers can retry, and the call will succeed once the new node has claimed the shard.

---

## Testing

### Test Shard Count

Tests use 64 shards instead of 8192 for faster execution:

```go
const TestNumShards = 64  // util/testutil/sharding.go
```

Valid test shard IDs: 0-63.

### Generating Object IDs for Specific Shards

```go
objID := testutil.GetObjectIDForShard(10, "TestObject")
// Returns an ID like "TestObject-10-0" that hashes to shard 10
```

The helper tries up to 10,000 candidates to find an ID that hashes to the target shard.

### Cluster Readiness

`testutil.WaitForClustersReady()` waits until all shards have `CurrentNode == TargetNode`, meaning the cluster has fully stabilized and all shards are claimed.

---

## Key Source Files

| File | Contents |
|------|----------|
| `cluster/sharding/sharding.go` | `NumShards` constant, `GetShardID()` hash function |
| `cluster/shardlock/shardlock.go` | Per-shard read/write locking |
| `cluster/consensusmanager/consensusmanager.go` | `ShardInfo` type, shard mapping management, claim/release/rebalance logic, etcd storage |
| `cluster/cluster.go` | High-level routing API (`GetCurrentNodeForObject`, `GetNodeForShard`) |
| `cluster/config.go` | Shard-related configuration and defaults |
| `util/objectid/parser.go` | Object ID format parsing and validation |
| `util/testutil/sharding.go` | `TestNumShards`, `GetObjectIDForShard()` |
