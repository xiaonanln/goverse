# MinQuorum Example

This example demonstrates how to configure a GoVerse cluster with a minimum node requirement (quorum). This ensures the cluster waits for a specific number of nodes to be available before becoming ready, which is important for production deployments.

## Overview

The `MinQuorum` configuration parameter allows you to specify the minimum number of nodes that must be present in the cluster before it's considered stable and ready to handle requests. This is useful for:

- **High Availability**: Ensuring you have enough nodes for fault tolerance
- **Load Distribution**: Waiting for sufficient nodes before accepting traffic
- **Graceful Startup**: Preventing partial cluster operations during deployment

## Running the Example

### Prerequisites

- Go 1.21 or later
- etcd running on `localhost:2379`

### Start etcd

```bash
# If you have Docker:
docker run -d --name etcd \
  -p 2379:2379 \
  -p 2380:2380 \
  quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd \
  --advertise-client-urls http://0.0.0.0:2379 \
  --listen-client-urls http://0.0.0.0:2379
```

### Start the Cluster Nodes

Open three terminal windows and run:

**Terminal 1:**
```bash
cd examples/minquorum
go run main.go --minQuorum=3 --port=7001
```

**Terminal 2:**
```bash
cd examples/minquorum
go run main.go --minQuorum=3 --port=7002
```

**Terminal 3:**
```bash
cd examples/minquorum
go run main.go --minQuorum=3 --port=7003
```

## What to Observe

1. **Before All Nodes Join**: Each node will start and log that it's waiting for the cluster to become ready.

2. **After All Nodes Join**: Once the third node starts, all nodes will log that the cluster is now ready.

3. **Leader Election**: The node with the smallest address (localhost:7001) will become the leader and manage shard mapping.

## Example Output

When starting with `--minQuorum=3`:

```
2025/11/05 05:30:00 Starting node on port 7001 with minimum quorum requirement: 3
2025/11/05 05:30:00 Waiting for cluster to become ready (requires 3 nodes)...
2025/11/05 05:30:00 Node running on port 7001 (waiting for 3 total nodes)
[INFO] [Cluster] Acting as leader: localhost:7001; nodes: 1 (min: 3), sharding map: 0, revision: 1
[WARN] [Cluster] Cluster has 1 nodes but requires minimum of 3 nodes - waiting for more nodes to join
```

After all 3 nodes join:
```
[INFO] [Cluster] Acting as leader: localhost:7001; nodes: 3 (min: 3), sharding map: 8192, revision: 5
2025/11/05 05:30:15 ✓ Cluster is now READY! All 3 minimum quorum are available.
```

## Key Configuration

```go
config := &goverseapi.ServerConfig{
    ListenAddress:    "localhost:7001",
    AdvertiseAddress: "localhost:7001",
    EtcdAddress:      "localhost:2379",
    EtcdPrefix:       "/goverse-example",
    MinQuorum:        3, // Require at least 3 nodes
}
```

## How It Works

1. **Cluster Initialization**: Each node connects to etcd and registers itself
2. **Node Counting**: The cluster tracks how many nodes are currently registered
3. **Stability Check**: The cluster is only marked as "ready" when:
   - The number of nodes >= MinQuorum
   - The node list has been stable for the configured duration (10 seconds by default)
   - Shard mapping has been successfully created
4. **Leader Management**: The leader node monitors node count and only creates/updates shard mapping when MinQuorum is met

## Production Considerations

- **Default Value**: If MinQuorum is not set, it defaults to 1
- **Deployment Strategy**: Set MinQuorum to match your expected cluster size
- **Scaling**: When scaling up, existing nodes continue to operate while waiting for new nodes
- **Scaling Down**: If the cluster drops below MinQuorum after being ready, it continues to operate but logs warnings

## See Also

- [GoVerse README](../../README.md) - Main project documentation
- [Chat Example](../samples/chat/) - Complete distributed application example
