# Goverse Demo Server

This demo server showcases Goverse's sharding and auto-load features with a cluster of distributed objects.

## Overview

The demo includes four types of objects:

1. **SimpleCounter** - Normal distributed objects
   - Created by NodeMonitors from a hardcoded list of 100 counter names (Counter-001 to Counter-100)
   - Distributed across shards based on hash-based sharding
   - Each counter can be incremented and queried

2. **ShardMonitor** - Per-shard auto-load objects
   - One ShardMonitor per shard (8192 total in production, 64 in tests)
   - Tracks shard-specific statistics like object count
   - Created automatically when nodes claim their shards
   - Object ID format: `shard#N/ShardMonitor`

3. **NodeMonitor** - Simulated per-node objects
   - One NodeMonitor per node (simulated via multiple auto-load entries with different IDs)
   - Aggregates stats from ShardMonitors on the node
   - Periodically scans the counter list and creates counters that hash to shards owned by its node
   - Object IDs: `NodeMonitor-1`, `NodeMonitor-2`, `NodeMonitor-3`

4. **GlobalMonitor** - Global singleton auto-load object
   - Single instance for the entire cluster
   - Provides global statistics and logging
   - Object ID: `GlobalMonitor`

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      GlobalMonitor                          │
│                  (Singleton, Auto-load)                     │
│              Tracks global cluster stats                    │
└─────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
    ┌─────────▼─────────┐         ┌─────────▼─────────┐
    │   NodeMonitor-1   │         │   NodeMonitor-2   │
    │   (Auto-load)     │   ...   │   (Auto-load)     │
    │ Creates Counters  │         │ Creates Counters  │
    └─────────┬─────────┘         └─────────┬─────────┘
              │                               │
    ┌─────────▼──────────────────────────────▼─────────┐
    │              Node 1 (Shards 0-2730)              │
    │  ┌──────────────┐ ┌──────────────┐              │
    │  │ShardMonitor#0│ │ShardMonitor#1│  ...         │
    │  └──────────────┘ └──────────────┘              │
    │  ┌──────────────┐ ┌──────────────┐              │
    │  │ Counter-001  │ │ Counter-042  │  ...         │
    │  └──────────────┘ └──────────────┘              │
    └──────────────────────────────────────────────────┘
```

## Configuration

The demo uses a YAML configuration file (`demo-cluster.yml`) that defines:

- 3 nodes (localhost:47001-47003)
- 2 gates (localhost:47101-47102)
- 1 inspector (localhost:48200)
- Auto-load objects:
  - 1 GlobalMonitor (singleton)
  - Per-shard ShardMonitors
  - 3 NodeMonitors (simulated per-node)

## Running the Demo

### Prerequisites

1. **etcd** - Must be running on localhost:2379
   ```bash
   # Option 1: Use Docker
   docker run -d --name etcd-demo -p 2379:2379 quay.io/coreos/etcd:latest \
     /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
     --advertise-client-urls http://localhost:2379
   
   # Option 2: Install and run locally
   brew install etcd  # macOS
   # or
   sudo apt-get install etcd  # Linux
   
   etcd
   ```

2. **Go 1.21+** - Required to build the demo server

### Starting the Cluster

```bash
# Run the entire cluster with one command
./run-cluster.sh
```

This script will:
1. Check if etcd is running (start it if needed)
2. Build the demo server
3. Start 3 node servers
4. Start 2 gate servers (if goverse-gate is available)
5. Start the inspector (if goverse-inspector is available)

### Manual Start (Alternative)

You can also start components manually:

```bash
# Terminal 1: Start node 1
go run main.go --config demo-cluster.yml --node-id node-1

# Terminal 2: Start node 2
go run main.go --config demo-cluster.yml --node-id node-2

# Terminal 3: Start node 3
go run main.go --config demo-cluster.yml --node-id node-3

# Terminal 4: Start gate 1 (if available)
goverse-gate --config demo-cluster.yml --node-id gate-1

# Terminal 5: Start gate 2 (if available)
goverse-gate --config demo-cluster.yml --node-id gate-2

# Terminal 6: Start inspector (if available)
goverse-inspector --config demo-cluster.yml
```

## Observing the Demo

### Log Files

When using `run-cluster.sh`, logs are written to:
- `/tmp/demo-node-1.log`
- `/tmp/demo-node-2.log`
- `/tmp/demo-node-3.log`
- `/tmp/demo-gate-1.log`
- `/tmp/demo-gate-2.log`
- `/tmp/demo-inspector.log`

### What to Look For

1. **Cluster Initialization**
   - Nodes register with etcd
   - Shard mapping is computed and distributed
   - Auto-load objects are created

2. **Auto-load Object Creation**
   - ShardMonitor objects: One per shard on each node
   - NodeMonitor objects: Created on nodes that own their respective shards
   - GlobalMonitor: Created on the node that owns its shard

3. **Counter Creation**
   - NodeMonitors periodically scan the counter list (every 15 seconds)
   - Each NodeMonitor tries to create all 100 counters
   - Only counters that hash to shards owned by the node are created
   - Creation is idempotent - already-existing counters are skipped

4. **Logs to Watch**
   ```
   NodeMonitor NodeMonitor-1 created, will periodically create counters
   NodeMonitor NodeMonitor-1: Scanning counter list for objects to create...
   Created counter SimpleCounter-Counter-042 (shard 1523)
   ShardMonitor created for shard 42
   GlobalMonitor created at 2024-01-15T10:30:00Z
   ```

### Inspector UI

If the inspector is running, visit http://localhost:48200 to:
- View the cluster topology
- See object distribution across nodes
- Monitor shard assignments
- Track object lifecycle

## Demonstrating Sharding Features

### Shard Distribution

Objects are distributed across 8192 shards using consistent hashing. Each counter's shard is determined by its object ID:

```go
shardID := objectid.GetShardID(counterID, sharding.NumShards)
```

### Auto-load Patterns

The demo demonstrates three auto-load patterns:

1. **Singleton** (GlobalMonitor)
   ```yaml
   - type: "GlobalMonitor"
     id: "GlobalMonitor"
     per_shard: false
   ```

2. **Per-shard** (ShardMonitor)
   ```yaml
   - type: "ShardMonitor"
     id: "ShardMonitor"
     per_shard: true
   ```

3. **Simulated per-node** (NodeMonitor)
   ```yaml
   - type: "NodeMonitor"
     id: "NodeMonitor-1"  # Each ID hashes to a different shard
   - type: "NodeMonitor"
     id: "NodeMonitor-2"
   - type: "NodeMonitor"
     id: "NodeMonitor-3"
   ```

### Shard Migration (Future Enhancement)

The `run-cluster.sh` script can be extended to demonstrate shard rebalancing:

1. Start with 2 nodes
2. Add a 3rd node
3. Watch shards migrate to balance the load
4. Shut down a node
5. Watch shards redistribute

## Testing

The demo includes integration tests that verify the functionality of all object types.

**Note**: Tests require etcd to be running on localhost:2379. If etcd is not available, tests will be skipped.

```bash
# Start etcd first
docker run -d --name etcd-test -p 2379:2379 quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379

# Run tests
cd examples/demo_server
go test -v -timeout 5m
```

Tests use 64 shards instead of 8192 for faster execution:

```go
NumShards: testutil.TestNumShards  // 64 shards in tests
```

## Cleanup

Press `Ctrl+C` to stop all processes when using `run-cluster.sh`. The script will:
1. Kill all node, gate, and inspector processes
2. Clean up etcd data (if started by the script)
3. Remove log files

Manual cleanup:
```bash
# Stop all demo processes
pkill -f demo-server
pkill -f goverse-gate
pkill -f goverse-inspector

# Clean up etcd data (if desired)
rm -rf /tmp/etcd-demo-data
rm /tmp/demo-*.log
```

## Implementation Notes

### Object Types

All objects extend `goverseapi.BaseObject` and implement the `Object` interface:

```go
type SimpleCounter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int64
}

func (c *SimpleCounter) OnCreated() {
    // Initialization logic
}
```

### Thread Safety

All objects use mutexes to protect shared state:

```go
func (c *SimpleCounter) Increment(ctx context.Context) (int64, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
    return c.value, nil
}
```

### Background Goroutines

NodeMonitor and GlobalMonitor use background goroutines with proper cleanup:

```go
func (n *NodeMonitor) OnCreated() {
    n.stopCh = make(chan struct{})
    go n.periodicCounterCreation()
}

func (n *NodeMonitor) periodicCounterCreation() {
    ticker := time.NewTicker(n.creationInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            n.createCounters()
        case <-n.stopCh:
            return
        }
    }
}
```

## Troubleshooting

### etcd Connection Issues

If nodes can't connect to etcd:
```bash
# Check if etcd is running
nc -z 127.0.0.1 2379

# Check etcd logs
docker logs etcd-demo  # if using Docker
```

### Port Conflicts

If ports are already in use, edit `demo-cluster.yml` to use different ports.

### Build Failures

Ensure proto files are compiled:
```bash
cd /path/to/goverse
./script/compile-proto.sh
go build ./...
```

## Next Steps

- Add metrics collection to ShardMonitor
- Implement counter aggregation in NodeMonitor
- Add REST API for querying counters
- Demonstrate shard migration by dynamically adding/removing nodes
- Add persistence for counter state
