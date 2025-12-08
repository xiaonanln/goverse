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

3. **NodeMonitor** - Per-node objects
   - One NodeMonitor per node (using `per_node: true` flag)
   - Aggregates stats from ShardMonitors on the node
   - Periodically scans the counter list and creates counters that hash to shards owned by its node
   - Object IDs are generated per node (e.g., `NodeMonitor-node-1`, `NodeMonitor-node-2`)

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

- 10 nodes (localhost:47001-47010)
- 5 gates (localhost:47101-47105)
- 1 inspector (localhost:48200)
- Auto-load objects:
  - 1 GlobalMonitor (singleton)
  - Per-shard ShardMonitors
  - 10 NodeMonitors (one per node)

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
1. Check if etcd is running (must be started separately)
2. Build the demo server, gate, and inspector
3. Start the inspector
4. Start 10 node servers
5. Start 5 gate servers (if goverse-gate is available)
6. Exit after all components are started

Use `./stop-cluster.sh` to stop all processes

### Manual Start (Alternative)

You can also start components manually (example with first few):

```bash
# Terminal 1: Start inspector
goverse-inspector --config demo-cluster.yml

# Terminal 2-11: Start nodes 1-10
go run main.go --config demo-cluster.yml --node-id node-1
go run main.go --config demo-cluster.yml --node-id node-2
# ... node-3 through node-10

# Terminal 12-16: Start gates 1-5 (if available)
goverse-gate --config demo-cluster.yml --node-id gate-1
goverse-gate --config demo-cluster.yml --node-id gate-2
# ... gate-3 through gate-5
```

## Observing the Demo

### Log Files

When using `run-cluster.sh`, logs are written to:
- `node.log` - All 10 node servers
- `gate.log` - All 5 gate servers
- `inspector.log` - Inspector

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

3. **Per-node** (NodeMonitor)
   ```yaml
   - type: "NodeMonitor"
     id: "NodeMonitor"
     per_node: true  # Creates one object per node
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

Use the stop script to stop all processes:

```bash
./stop-cluster.sh
```

This will:
1. Kill all node processes
2. Kill all gate processes
3. Kill the inspector process

Manual cleanup (alternative):
```bash
# Stop all demo processes
pkill -f demo-server
pkill -f goverse-gate
pkill -f goverse-inspector

# Clean up log files
rm -f node.log gate.log inspector.log
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

## Stress Testing

A comprehensive stress testing script is available to test the demo server under load:

```bash
cd examples/demo_server
python3 stress_test_demo.py --clients 10 --duration 300
```

The stress test:
- Starts a 3-node cluster with 2 gates and 1 inspector
- Runs configurable number of concurrent clients
- Performs random operations (create, increment, get counters)
- Tracks and reports statistics in real-time
- Handles graceful cleanup on exit

See [STRESS_TEST_README.md](STRESS_TEST_README.md) for detailed documentation.

## Next Steps

- Add metrics collection to ShardMonitor
- Implement counter aggregation in NodeMonitor
- Add REST API for querying counters
- Demonstrate shard migration by dynamically adding/removing nodes
- Add persistence for counter state
