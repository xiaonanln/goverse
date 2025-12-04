# Goverse Inspector

A powerful web-based visualization and management tool for monitoring and managing your Goverse distributed cluster in real-time.

## Overview

The Inspector provides a comprehensive dashboard for observing and managing your Goverse cluster. It offers real-time visualization of nodes, gates, objects, and shard distribution across your distributed system. Whether you're debugging issues, monitoring cluster health, or performing shard management operations, the Inspector gives you complete visibility and control.

## Features

- **Real-time Cluster Monitoring**: Live updates via Server-Sent Events (SSE)
- **Interactive Visualizations**: Force-directed graphs and charts for cluster topology
- **Shard Management**: Drag-and-drop shard migration between nodes
- **Multi-View Dashboard**: Four specialized views for different aspects of your cluster
- **Node & Gate Tracking**: Monitor connections and health of all cluster components

## Quick Start

### Running the Demo

The easiest way to see the Inspector in action is to run the demo:

```bash
# Start etcd (required for shard management features)
docker run -d --name etcd-demo -p 2379:2379 -p 2380:2380 \
  gcr.io/etcd-development/etcd:v3.5.10 \
  /usr/local/bin/etcd \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379

# Run the inspector demo (automatically opens browser)
cd cmd/inspector/demo
go run . --nodes 3 --gates 2 --objects 50 --shards 64
```

This will start the Inspector with sample data at http://localhost:8080

### Running with Your Cluster

To connect the Inspector to your actual Goverse cluster:

```bash
# From the project root
go run ./cmd/inspector \
  --http-addr :8080 \
  --grpc-addr :8081 \
  --etcd-addr localhost:2379 \
  --etcd-prefix /goverse
```

Or using a config file:

```bash
go run ./cmd/inspector --config /path/to/config.yaml
```

## Views

The Inspector provides four specialized views accessible via tabs at the top of the interface:

### 1. Objects View

Visualizes the relationship between nodes, gates, and objects in your cluster using an interactive force-directed graph.

![Objects View](https://github.com/user-attachments/assets/1bcbf878-d557-4a00-9494-db8b33235136)

**Features:**
- **Green squares**: Goverse nodes
- **Blue diamonds**: Gate servers
- **Colored circles**: Objects (colored by type)
- **Gray lines**: Connections between components
- **Interactive**: Click nodes to see details, zoom and pan

**Use Cases:**
- Understanding object distribution across nodes
- Visualizing gate connections to nodes
- Identifying connection patterns and potential issues
- Exploring object relationships and clustering

### 2. Nodes View

Shows the cluster topology with focus on node-to-node and gate-to-node connections, displaying object counts per component.

![Nodes View](https://github.com/user-attachments/assets/4ac6a4b1-df89-4585-bf96-e6077270e67f)

**Features:**
- **Green boxes with counts**: Nodes showing number of hosted objects
- **Blue diamonds with counts**: Gates showing number of hosted objects
- **Connection lines**: Node interconnections and gate connections
- **Object counts**: See how many objects each component manages

**Use Cases:**
- Monitoring cluster connectivity health
- Identifying nodes with unbalanced object loads
- Verifying gate connections to all nodes
- Detecting disconnected or isolated nodes

### 3. Shard Distribution

Provides statistical visualizations and distribution analysis of your cluster's shards and objects.

![Shard Distribution](https://github.com/user-attachments/assets/f3183f36-7d17-45ae-ba0c-f1f1834e01db)

**Features:**
- **Cluster Summary**: Node count, gate count, object count, active shard count
- **Objects by Type**: Pie chart showing distribution of object types
- **Objects by Node**: Bar chart showing object count per node
- **Shard Distribution**: Histogram showing objects per shard

**Use Cases:**
- Monitoring cluster capacity and utilization
- Identifying object type distribution patterns
- Detecting load imbalance across nodes
- Understanding shard utilization patterns

### 4. Shard Management

Interactive interface for viewing and managing shard assignments across nodes.

![Shard Management](https://github.com/user-attachments/assets/0075638d-b6bd-4475-96a7-6dcaacf00526)

**Features:**
- **Node Sections**: Each node shows its assigned shards with object counts
- **Shard Pills**: Color-coded badges showing shard ID and object count
  - **Bright green**: Shards with objects
  - **Muted green**: Empty shards
- **Drag & Drop**: Move shards between nodes by dragging shard pills
- **Real-time Updates**: See migrations in progress and completion

**Use Cases:**
- Rebalancing shards across nodes for better load distribution
- Moving shards to prepare for node maintenance
- Consolidating shards to optimize resource usage
- Managing cluster capacity by redistributing workload

## Configuration

### Command-Line Options

```bash
  --http-addr string      HTTP server address (default ":8080")
  --grpc-addr string      gRPC server address (default ":8081")
  --etcd-addr string      etcd server address (optional)
  --etcd-prefix string    etcd key prefix (default "/goverse")
  --static-dir string     Static files directory (default "cmd/inspector/web")
  --config string         Path to YAML config file
```

### Configuration File

When using `--config`, create a YAML file with your cluster configuration:

```yaml
etcd:
  address: "localhost:2379"
  prefix: "/goverse"

inspector:
  grpc_addr: ":8081"
  http_addr: ":8080"

num_shards: 8192  # Production default
```

**Note**: When using `--config`, other command-line flags cannot be used.

## Architecture

The Inspector consists of several components:

- **HTTP Server**: Serves the web interface and API endpoints
- **gRPC Server**: Receives updates from Goverse nodes and gates
- **Graph Engine**: Maintains in-memory cluster state graph
- **SSE Handler**: Pushes real-time updates to connected web clients
- **Consensus Manager**: (Optional) Watches etcd for shard state changes

### How It Works

1. **Registration**: Nodes and gates register with the Inspector via gRPC
2. **Updates**: Components send periodic updates about their state
3. **Graph Updates**: Inspector updates its internal graph representation
4. **Client Notification**: Changes are pushed to web clients via SSE
5. **Shard Operations**: Management operations are written to etcd and executed by nodes

## Integration with Goverse Cluster

Nodes and gates automatically connect to the Inspector when configured:

```yaml
# In your node/gate config
inspector:
  address: "localhost:8081"  # Inspector's gRPC address
```

The Inspector receives:
- Node registrations and heartbeats
- Gate registrations and heartbeats
- Object creation and deletion events
- Connection status updates

## Development

### Running from Source

```bash
# Install dependencies
go mod download

# Compile proto files
./script/compile-proto.sh

# Run inspector
go run ./cmd/inspector
```

### Demo Options

The demo supports several flags for customization:

```bash
go run ./cmd/inspector/demo \
  --nodes 5 \          # Number of demo nodes
  --gates 3 \          # Number of demo gates
  --objects 100 \      # Number of demo objects
  --shards 64 \        # Number of shards
  --no-browser        # Don't open browser automatically
```

### Directory Structure

```
cmd/inspector/
├── main.go                           # Inspector entry point
├── demo/                             # Demo application
│   └── main.go
├── graph/                            # Graph data structure
│   ├── graph.go
│   └── graph_test.go
├── inspector/                        # Core inspector logic
│   └── inspector.go
├── inspectorconfig/                  # Configuration handling
│   └── inspectorconfig.go
├── inspectserver/                    # gRPC and HTTP servers
│   ├── inspectserver.go
│   └── inspectserver_test.go
├── models/                           # Data models
│   └── models.go
├── proto/                            # Protocol definitions
│   └── inspector.proto
└── web/                              # Web UI (HTML/CSS/JS)
    ├── index.html
    ├── styles.css
    ├── app.js
    ├── graph-view.js
    ├── nodes-view.js
    ├── shard-view.js
    ├── shardmgmt-view.js
    └── ...
```

## Troubleshooting

### Inspector won't start

**Problem**: `Failed to start gRPC server` or `Failed to start HTTP server`

**Solution**: Check if ports are already in use. Change ports with `--grpc-addr` and `--http-addr` flags.

### No data showing

**Problem**: Inspector UI loads but shows no nodes/objects

**Solution**: 
1. Verify nodes and gates are configured with the correct Inspector gRPC address
2. Check that nodes are running and able to connect to the Inspector
3. Check Inspector logs for connection errors

### Shard Management not working

**Problem**: Can't drag shards or changes don't persist

**Solution**:
1. Ensure etcd is running and accessible
2. Verify `--etcd-addr` matches your etcd server address
3. Check that nodes have write access to etcd with the same prefix

### Demo etcd connection fails

**Problem**: `Failed to connect to etcd`

**Solution**: Start etcd before running the demo (see Quick Start section)

## Profiling with pprof

The Inspector includes built-in support for Go's pprof profiler, which allows you to analyze performance and diagnose issues.

### Accessing pprof Endpoints

Once the Inspector is running, pprof endpoints are available at:

- **Index**: `http://localhost:8080/debug/pprof/` - Overview of all available profiles
- **Heap**: `http://localhost:8080/debug/pprof/heap` - Memory allocation profile
- **CPU**: `http://localhost:8080/debug/pprof/profile` - CPU profile (30s sample)
- **Goroutine**: `http://localhost:8080/debug/pprof/goroutine` - Stack traces of all goroutines
- **Trace**: `http://localhost:8080/debug/pprof/trace` - Execution trace
- **Other profiles**: block, mutex, threadcreate, etc.

### Using pprof with the go tool

```bash
# View heap profile in browser
go tool pprof -http=:8090 http://localhost:8080/debug/pprof/heap

# Capture 30-second CPU profile and analyze
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30

# View all goroutines
curl http://localhost:8080/debug/pprof/goroutine?debug=1

# Save profile for later analysis
curl -o inspector-heap.prof http://localhost:8080/debug/pprof/heap
go tool pprof inspector-heap.prof
```

### Common Use Cases

- **Memory leaks**: Use heap profile to identify growing allocations
- **High CPU usage**: CPU profile shows which functions are consuming time
- **Too many goroutines**: Goroutine profile helps find goroutine leaks
- **Lock contention**: Mutex and block profiles show synchronization issues

## Performance Considerations

- The Inspector keeps full cluster state in memory for fast access
- SSE connections push updates in real-time to all connected clients
- For large clusters (1000+ nodes), consider running Inspector on a dedicated server
- The demo mode is for testing only; use actual etcd for production
- Use pprof endpoints to monitor Inspector resource usage and performance

## License

This is part of the Goverse project. See the main repository LICENSE file for details.

## See Also

- [Goverse Main Documentation](../README.md)
- [Shard Drag & Drop Implementation](../cmd/inspector/SHARD_DRAG_DROP_IMPLEMENTATION.md)
- [Testing Shard Drag & Drop](../cmd/inspector/TESTING_SHARD_DRAG_DROP.md)
