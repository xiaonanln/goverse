# Distributed Cache Sample

This sample demonstrates a distributed in-memory cache implementation using GoVerse's distributed object runtime. It showcases how goverse handles:

- **Distributed cache entries**: 1000+ DistributedCache grains automatically sharded across cluster nodes
- **Node-local managers**: CacheManager grains coordinate cache operations on each node
- **Automatic routing**: Keys are automatically routed to the correct node based on sharding
- **Dynamic activation**: Cache entry grains are created on-demand and can be deactivated when idle
- **TTL support**: Cache entries can have optional time-to-live for automatic expiration
- **Client connectivity**: Clients can connect to any node to access the entire cache

## Architecture

### Components

1. **DistributedCache Grain** (`DistributedCache.go`)
   - Represents a single cache entry (key-value pair)
   - Each grain is identified by `DistributedCache-<key>`
   - Supports optional TTL (time-to-live) for automatic expiration
   - Automatically distributed across cluster nodes using goverse's sharding
   - Created on-demand when a key is first accessed

2. **CacheManager Grain** (`CacheManager.go`)
   - Node-local manager for cache operations
   - Routes requests to appropriate DistributedCache grains
   - Tracks cache hits and misses
   - Provides cache statistics
   - One per node with ID format: `CacheManager0`, `CacheManager1`, etc.

3. **CacheClient** (`CacheClient.go`)
   - Client-side interface for cache operations
   - Connects to the local CacheManager
   - Provides Set, Get, Delete, and GetStats operations

### How It Works

1. **Client connects** to any node in the cluster
2. **Client calls** cache operations (Set/Get/Delete) through CacheClient
3. **CacheClient routes** to local CacheManager grain
4. **CacheManager** determines target DistributedCache grain ID from key
5. **Goverse routing** automatically forwards to correct node based on shard mapping
6. **DistributedCache grain** is activated on-demand if not already running
7. **Operation executes** on the grain and result returns to client

```
┌──────────┐      ┌─────────────┐      ┌──────────────┐      ┌───────────────────┐
│  Client  │─────▶│ CacheClient │─────▶│ CacheManager │─────▶│ DistributedCache  │
└──────────┘      └─────────────┘      └──────────────┘      └───────────────────┘
                                              │                        │
                                              │                   (key: user:123)
                                              │                        │
                                              ▼                        ▼
                                        [Routes based          [Sharded across
                                         on key hash]           cluster nodes]
```

## Running the Sample

### Prerequisites

1. Install etcd (required for goverse cluster coordination):
   ```bash
   # macOS
   brew install etcd
   
   # Linux
   sudo apt-get install etcd
   
   # Or use Docker
   docker run -d -p 2379:2379 -p 2380:2380 --name etcd quay.io/coreos/etcd:latest
   ```

2. Compile protocol buffers (from repository root):
   ```bash
   ./script/compile-proto.sh
   ```

### Single Node

1. Start etcd (if not already running):
   ```bash
   etcd
   ```

2. Start the cache server:
   ```bash
   cd samples/distributed-cache/server
   go run . -listen localhost:47000 -advertise localhost:47000 -client-listen localhost:48000
   ```

3. In another terminal, run the client:
   ```bash
   cd samples/distributed-cache/client
   go run . -server localhost:48000
   ```

### Multi-Node Cluster

To demonstrate distributed sharding across multiple nodes:

1. Start etcd:
   ```bash
   etcd
   ```

2. Start node 1:
   ```bash
   cd samples/distributed-cache/server
   go run . -listen localhost:47000 -advertise localhost:47000 -client-listen localhost:48000 -metrics-listen localhost:9100
   ```

3. Start node 2 (in another terminal):
   ```bash
   cd samples/distributed-cache/server
   go run . -listen localhost:47001 -advertise localhost:47001 -client-listen localhost:48001 -metrics-listen localhost:9101
   ```

4. Start node 3 (in another terminal):
   ```bash
   cd samples/distributed-cache/server
   go run . -listen localhost:47002 -advertise localhost:47002 -client-listen localhost:48002 -metrics-listen localhost:9102
   ```

5. Connect clients to any node:
   ```bash
   # Client connecting to node 1
   cd samples/distributed-cache/client
   go run . -server localhost:48000
   
   # Client connecting to node 2 (in another terminal)
   cd samples/distributed-cache/client
   go run . -server localhost:48001
   ```

All clients will access the same distributed cache regardless of which node they connect to!

## API Examples

### Set a Cache Entry

```go
// Set with no expiration
resp, err := goverseapi.CallObject(ctx, "CacheManager", "CacheManager0", "Set", &cache_pb.CacheManager_SetRequest{
    Key:        "user:1001",
    Value:      "Alice",
    TtlSeconds: 0, // No expiration
})

// Set with TTL (expires after 60 seconds)
resp, err := goverseapi.CallObject(ctx, "CacheManager", "CacheManager0", "Set", &cache_pb.CacheManager_SetRequest{
    Key:        "session:abc",
    Value:      "active",
    TtlSeconds: 60,
})
```

### Get a Cache Entry

```go
resp, err := goverseapi.CallObject(ctx, "CacheManager", "CacheManager0", "Get", &cache_pb.CacheManager_GetRequest{
    Key: "user:1001",
})

getResp := resp.(*cache_pb.CacheManager_GetResponse)
if getResp.Found {
    fmt.Printf("Value: %s\n", getResp.Value)
} else {
    fmt.Println("Key not found or expired")
}
```

### Delete a Cache Entry

```go
resp, err := goverseapi.CallObject(ctx, "CacheManager", "CacheManager0", "Delete", &cache_pb.CacheManager_DeleteRequest{
    Key: "user:1001",
})
```

### Get Cache Statistics

```go
resp, err := goverseapi.CallObject(ctx, "CacheManager", "CacheManager0", "Stats", &cache_pb.CacheManager_StatsRequest{})

statsResp := resp.(*cache_pb.CacheManager_StatsResponse)
fmt.Printf("Hits: %d, Misses: %d\n", statsResp.HitCount, statsResp.MissCount)
```

## Testing

Run the tests:
```bash
cd samples/distributed-cache/server
go test -v
```

The tests demonstrate:
- Setting and getting cache entries
- TTL expiration behavior
- Delete operations
- Cache statistics tracking
- Concurrent access patterns

## Key Features Demonstrated

### 1. Automatic Sharding
- Cache entries are automatically distributed based on key hash
- Goverse handles routing to the correct node transparently
- Add/remove nodes and entries rebalance automatically

### 2. Dynamic Grain Activation
- DistributedCache grains are created on first access
- Idle grains can be deactivated to save memory
- Reactivated automatically when accessed again

### 3. Node-Local Coordination
- Each node has its own CacheManager
- Clients connect to local manager for better locality
- Managers coordinate across nodes as needed

### 4. Fault Tolerance
- If a node fails, its cache entries are lost (in-memory only)
- Cluster automatically rebalances remaining entries
- Clients can reconnect to other nodes

### 5. Scalability
- Add more nodes to handle more cache entries
- Horizontal scaling of both storage and throughput
- No single point of contention

## Performance Considerations

- **In-Memory Only**: No persistence, all data lost on node restart
- **TTL Checking**: Done on access, no background cleanup thread
- **Grain Overhead**: Each cache entry is a grain with some overhead
- **Best For**: 
  - Distributed session storage
  - Temporary computation results
  - Frequently accessed data with locality
  - Systems needing automatic sharding

## Extending the Sample

Ideas for enhancement:
1. **Persistence**: Add `ToData`/`FromData` for PostgreSQL persistence
2. **Eviction**: Implement LRU eviction when memory is low
3. **Replication**: Store replicas for fault tolerance
4. **Bulk Operations**: Add batch get/set for efficiency
5. **Pub/Sub**: Notify subscribers when keys change
6. **Namespace**: Support cache namespaces/prefixes
7. **Compression**: Compress large values automatically

## Related Documentation

- [GoVerse Documentation](../../../docs/GET_STARTED.md)
- [Chat Sample](../chat/) - Another example of distributed objects
- [Sharding Details](../../../cluster/sharding/README.md)

## License

Same as GoVerse - MIT License
