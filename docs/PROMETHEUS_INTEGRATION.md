# Prometheus Metrics Integration

This document describes the Prometheus metrics integration added to GoVerse.

## Overview

GoVerse now exposes Prometheus metrics for monitoring distributed objects. The metrics are automatically tracked and can be scraped by Prometheus for observability.

## Available Metrics

### Object Metrics

**`goverse_objects_total{node, type, shard}`** - Gauge
- Description: Total number of distributed objects in the cluster
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `type`: The object type (e.g., "ChatRoom", "ChatClient")
  - `shard`: The shard number (e.g., "100", "200")
  - `shard`: The shard number (e.g., "0", "100")
- Updated: Automatically incremented on object creation, decremented on deletion

### Method Call Metrics

**`goverse_method_calls_total{node, object_type, method_name, status}`** - Counter
- Description: Total number of method calls on distributed objects
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `object_type`: The object type (e.g., "ChatRoom", "ChatClient")
  - `method_name`: The method being called (e.g., "SendMessage", "JoinRoom")
  - `status`: The call status - "success" or "failure"
- Updated: Automatically incremented on each method call

**`goverse_method_call_duration{node, object_type, method_name, status}`** - Histogram
- Description: Duration of method calls on distributed objects in seconds
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `object_type`: The object type (e.g., "ChatRoom", "ChatClient")
  - `method_name`: The method being called (e.g., "SendMessage", "JoinRoom")
  - `status`: The call status - "success" or "failure"
- Buckets: [0.001, 0.01, 0.1, 1, 10] seconds
- Updated: Automatically recorded for each method call

### Shard Metrics

**`goverse_shards_total{node}`** - Gauge
- Description: Total number of shards assigned to each node
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically updated when shard mapping changes in the ConsensusManager
- Note: Counts shards based on CurrentNode (actual ownership), not TargetNode (planned assignment)

### Shard Migration Metrics

**`goverse_shard_claims_total{node}`** - Counter
- Description: Total number of shard ownership claims by node
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically incremented when a node successfully claims ownership of shards via ClaimShardsForNode

**`goverse_shard_releases_total{node}`** - Counter
- Description: Total number of shard ownership releases by node
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically incremented when a node successfully releases ownership of shards via ReleaseShardsForNode

**`goverse_shard_migrations_total{from_node, to_node}`** - Counter
- Description: Total number of completed shard migrations (ownership transfers between nodes)
- Labels:
  - `from_node`: The node that previously owned the shard (e.g., "localhost:47000")
  - `to_node`: The node that now owns the shard (e.g., "localhost:47001")
- Updated: Automatically incremented when a shard's CurrentNode changes from one node to another

**`goverse_shards_migrating`** - Gauge
- Description: Number of shards currently in migration state (TargetNode != CurrentNode)
- Updated: Automatically updated when shard metrics are refreshed via UpdateShardMetrics
- Note: A shard is considered migrating when its TargetNode differs from its CurrentNode

### Client Connection Metrics

**`goverse_clients_connected{node, client_type}`** - Gauge
- Description: Number of active client connections in the cluster
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `client_type`: The type of client connection (e.g., "grpc", defaults to "grpc" if not specified)
- Updated: Automatically incremented on client connection, decremented on disconnection

## Metrics Endpoint

The metrics are exposed via HTTP by each GoVerse server/node. Configure the metrics endpoint address using the `MetricsListenAddress` field in `ServerConfig`:

```go
config := &goverseapi.ServerConfig{
    ListenAddress:        "localhost:47000",
    AdvertiseAddress:     "localhost:47000",
    MetricsListenAddress: ":9090",  // Metrics HTTP endpoint
    EtcdAddress:          "localhost:2379",
}
```

The endpoint returns metrics in Prometheus text format at:
```
http://<node-address>:9090/metrics
```

## Example Output

```
# HELP goverse_objects_total Total number of distributed objects in the cluster
# TYPE goverse_objects_total gauge
goverse_objects_total{node="localhost:47000",type="ChatRoom",shard="100"} 3
goverse_objects_total{node="localhost:47000",type="ChatClient",shard="200"} 5
goverse_objects_total{node="localhost:47001",type="ChatRoom",shard="150"} 2

# HELP goverse_method_calls_total Total number of method calls on distributed objects
# TYPE goverse_method_calls_total counter
goverse_method_calls_total{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success"} 1523
goverse_method_calls_total{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="failure"} 12
goverse_method_calls_total{node="localhost:47000",object_type="ChatRoom",method_name="JoinRoom",status="success"} 245
goverse_method_calls_total{node="localhost:47001",object_type="ChatClient",method_name="Connect",status="success"} 89

# HELP goverse_method_call_duration Duration of method calls on distributed objects in seconds
# TYPE goverse_method_call_duration histogram
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="0.001"} 450
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="0.01"} 1200
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="0.1"} 1500
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="1"} 1520
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="10"} 1523
goverse_method_call_duration_bucket{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success",le="+Inf"} 1523
goverse_method_call_duration_sum{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success"} 45.2
goverse_method_call_duration_count{node="localhost:47000",object_type="ChatRoom",method_name="SendMessage",status="success"} 1523

# HELP goverse_shards_total Total number of shards assigned to each node
# TYPE goverse_shards_total gauge
goverse_shards_total{node="localhost:47000"} 2048
goverse_shards_total{node="localhost:47001"} 2048
goverse_shards_total{node="localhost:47002"} 2048
goverse_shards_total{node="localhost:47003"} 2048
goverse_objects_total{node="localhost:47000",type="ChatRoom",shard="0"} 3
goverse_objects_total{node="localhost:47000",type="ChatClient",shard="1"} 5
goverse_objects_total{node="localhost:47001",type="ChatRoom",shard="2"} 2

# HELP goverse_shard_claims_total Total number of shard ownership claims by node
# TYPE goverse_shard_claims_total counter
goverse_shard_claims_total{node="localhost:47000"} 2048
goverse_shard_claims_total{node="localhost:47001"} 2048
goverse_shard_claims_total{node="localhost:47002"} 2048

# HELP goverse_shard_releases_total Total number of shard ownership releases by node
# TYPE goverse_shard_releases_total counter
goverse_shard_releases_total{node="localhost:47000"} 15
goverse_shard_releases_total{node="localhost:47001"} 12

# HELP goverse_shard_migrations_total Total number of completed shard migrations (ownership transfers between nodes)
# TYPE goverse_shard_migrations_total counter
goverse_shard_migrations_total{from_node="localhost:47000",to_node="localhost:47001"} 8
goverse_shard_migrations_total{from_node="localhost:47000",to_node="localhost:47002"} 7
goverse_shard_migrations_total{from_node="localhost:47001",to_node="localhost:47002"} 5

# HELP goverse_shards_migrating Number of shards currently in migration state (TargetNode != CurrentNode)
# TYPE goverse_shards_migrating gauge
goverse_shards_migrating 3

# HELP goverse_clients_connected Number of active client connections in the cluster
# TYPE goverse_clients_connected gauge
goverse_clients_connected{node="localhost:47000",client_type="grpc"} 12
goverse_clients_connected{node="localhost:47001",client_type="grpc"} 8
```

## Prometheus Configuration

Add the following to your `prometheus.yml` to scrape GoVerse metrics from all nodes:

```yaml
scrape_configs:
  - job_name: 'goverse'
    static_configs:
      - targets:
          - 'localhost:9090'    # Node 1
          - 'localhost:9091'    # Node 2
          - 'localhost:9092'    # Node 3
    metrics_path: '/metrics'
```

For dynamic discovery, you can use Prometheus service discovery mechanisms based on your infrastructure (e.g., DNS, Consul, Kubernetes).

## Implementation Details

### Architecture

The metrics integration is implemented across several packages:

1. **`util/metrics`** - Core metrics package
   - Defines and registers Prometheus metrics
   - Provides helper functions for updating metrics

2. **`node`** - Node package integration
   - Tracks object creation in `createObject()`
   - Tracks object deletion in `destroyObject()`

3. **`cluster/consensusmanager`** - ConsensusManager integration
   - Tracks shard assignments in `handleShardEvent()`
   - Updates metrics when shard mapping changes
   - Counts shards per node based on CurrentNode (actual ownership)

4. **`server`** - Server package
   - Exposes `/metrics` HTTP endpoint using `promhttp.Handler()`
   - Configurable via `MetricsListenAddress` in `ServerConfig`
   - Tracks client connections/disconnections in `Register()` method

### Automatic Tracking

Metrics are automatically updated without requiring manual intervention:

- **Object Creation**: When `node.createObject()` successfully creates an object, the object count is incremented
- **Object Deletion**: When `node.destroyObject()` removes an object, the object count is decremented
- **Method Calls**: When `node.CallObject()` executes a method, both the counter and histogram are updated with the call status and duration
- **Shard Mapping Changes**: When `ConsensusManager` handles shard events or initializes, the shard count per node is updated
- **Client Connection**: When a client connects via `server.Register()`, the client connection count is incremented
- **Client Disconnection**: When a client disconnects, the client connection count is decremented

### Thread Safety

All metrics operations are thread-safe and use Prometheus client library's built-in synchronization.

## Usage Examples

### Querying Metrics with PromQL

#### Object Metrics

Total objects across all nodes:
```promql
sum(goverse_objects_total)
```

Objects by type:
```promql
sum by (type) (goverse_objects_total)
```

Objects on a specific node:
```promql
goverse_objects_total{node="localhost:47000"}
```

#### Method Call Metrics

Total method calls across all nodes:
```promql
sum(goverse_method_calls_total)
```

Method calls by status (success vs failure):
```promql
sum by (status) (goverse_method_calls_total)
```

Success rate for a specific method:
```promql
sum(rate(goverse_method_calls_total{method_name="SendMessage",status="success"}[5m]))
/
sum(rate(goverse_method_calls_total{method_name="SendMessage"}[5m]))
```

Error rate (failures per second) for a specific object type:
```promql
sum(rate(goverse_method_calls_total{object_type="ChatRoom",status="failure"}[5m]))
```

#### Method Call Duration Metrics

Average method call duration (seconds):
```promql
rate(goverse_method_call_duration_sum[5m])
/
rate(goverse_method_call_duration_count[5m])
```

95th percentile of method call duration:
```promql
histogram_quantile(0.95, rate(goverse_method_call_duration_bucket[5m]))
```

99th percentile for a specific method:
```promql
histogram_quantile(0.99, rate(goverse_method_call_duration_bucket{method_name="SendMessage"}[5m]))
```

Methods taking longer than 100ms (p95):
```promql
histogram_quantile(0.95, rate(goverse_method_call_duration_bucket[5m])) > 0.1
```

Shard distribution across all nodes:
```promql
sum(goverse_shards_total) by (node)
```

Total shards in the cluster:
```promql
sum(goverse_shards_total)
```

Check for unbalanced shard distribution (find max - min):
```promql
max(goverse_shards_total) - min(goverse_shards_total)
Total connected clients across all nodes:
```promql
sum(goverse_clients_connected)
```

Connected clients by node:
```promql
goverse_clients_connected{node="localhost:47000"}
```

Connected clients by type:
```promql
sum by (client_type) (goverse_clients_connected)
```

#### Shard Migration Metrics

Total shard claims by node:
```promql
sum by (node) (goverse_shard_claims_total)
```

Total shard releases by node:
```promql
sum by (node) (goverse_shard_releases_total)
```

Shard claim rate (claims per second over 5 minutes):
```promql
rate(goverse_shard_claims_total[5m])
```

Shard release rate (releases per second over 5 minutes):
```promql
rate(goverse_shard_releases_total[5m])
```

Total completed migrations across all nodes:
```promql
sum(goverse_shard_migrations_total)
```

Migration rate (migrations per second over 5 minutes):
```promql
sum(rate(goverse_shard_migrations_total[5m]))
```

Migrations by direction (from_node to to_node):
```promql
goverse_shard_migrations_total{from_node="localhost:47000",to_node="localhost:47001"}
```

Most active migration paths:
```promql
topk(5, sum by (from_node, to_node) (goverse_shard_migrations_total))
```

Current number of shards in migration:
```promql
goverse_shards_migrating
```

Percentage of shards currently migrating:
```promql
goverse_shards_migrating / 8192 * 100
```

Net shard churn per node (claims - releases):
```promql
sum by (node) (goverse_shard_claims_total) - sum by (node) (goverse_shard_releases_total)
```

### Alerting Examples

Alert when object count is too high:
```yaml
- alert: HighObjectCount
  expr: goverse_objects_total{node=~".+"} > 10000
  for: 5m
  annotations:
    summary: "Node {{ $labels.node }} has high object count"
```

Alert when method call error rate is too high:
```yaml
- alert: HighMethodCallErrorRate
  expr: |
    sum by (node, object_type, method_name) (rate(goverse_method_calls_total{status="failure"}[5m]))
    /
    sum by (node, object_type, method_name) (rate(goverse_method_calls_total[5m]))
    > 0.05
  for: 5m
  annotations:
    summary: "High error rate for {{ $labels.method_name }} on {{ $labels.object_type }} at {{ $labels.node }}"
    description: "Error rate is {{ $value | humanizePercentage }}"
```

Alert when method call duration is too high:
```yaml
- alert: SlowMethodCalls
  expr: |
    histogram_quantile(0.95,
      rate(goverse_method_call_duration_bucket[5m])
    ) > 1.0
  for: 5m
  annotations:
    summary: "Slow method calls detected on {{ $labels.node }}"
    description: "P95 latency is {{ $value }}s for {{ $labels.method_name }}"
```

Alert when shard distribution is unbalanced:
```yaml
- alert: UnbalancedShardDistribution
  expr: (max(goverse_shards_total) - min(goverse_shards_total)) > 500
  for: 10m
  annotations:
    summary: "Shard distribution is unbalanced (difference > 500)"
    description: "Max shards: {{ $value }}, consider rebalancing"
```

Alert when a node has no shards but should:
```yaml
- alert: NodeWithoutShards
  expr: goverse_shards_total{node=~".+"} == 0
  for: 5m
  annotations:
    summary: "Node {{ $labels.node }} has no shards assigned"
Alert when client count is too high:
```yaml
- alert: HighClientCount
  expr: goverse_clients_connected{node=~".+"} > 1000
  for: 5m
  annotations:
    summary: "Node {{ $labels.node }} has high client connection count"
```

Alert when no clients are connected:
```yaml
- alert: NoClientsConnected
  expr: sum(goverse_clients_connected) == 0
  for: 10m
  annotations:
    summary: "No clients connected to any node in the cluster"
```

Alert when too many shards are migrating:
```yaml
- alert: HighShardMigrationActivity
  expr: goverse_shards_migrating > 100
  for: 15m
  annotations:
    summary: "High number of shards in migration state"
    description: "{{ $value }} shards are currently migrating (TargetNode != CurrentNode)"
```

Alert when shard migration rate is too high:
```yaml
- alert: HighShardMigrationRate
  expr: sum(rate(goverse_shard_migrations_total[5m])) > 10
  for: 10m
  annotations:
    summary: "High shard migration rate detected"
    description: "Shard migrations are happening at {{ $value }} per second"
```

Alert when shard migrations are stuck:
```yaml
- alert: StuckShardMigrations
  expr: goverse_shards_migrating > 0 and rate(goverse_shard_migrations_total[15m]) == 0
  for: 30m
  annotations:
    summary: "Shard migrations appear to be stuck"
    description: "{{ $value }} shards have been in migration state for over 30 minutes with no completions"
```

Alert when shard churn is imbalanced (one node claiming more than releasing):
```yaml
- alert: ImbalancedShardChurn
  expr: |
    abs(
      sum by (node) (increase(goverse_shard_claims_total[1h])) - 
      sum by (node) (increase(goverse_shard_releases_total[1h]))
    ) > 200
  for: 15m
  annotations:
    summary: "Imbalanced shard churn on {{ $labels.node }}"
    description: "Node has a net difference of {{ $value }} shards claimed vs released in the last hour"
```

Alert when a node is constantly releasing shards (possible node degradation):
```yaml
- alert: NodeLosingShards
  expr: rate(goverse_shard_releases_total[10m]) > rate(goverse_shard_claims_total[10m])
  for: 30m
  annotations:
    summary: "Node {{ $labels.node }} is consistently losing shards"
    description: "Node may be degraded - releasing more shards than claiming"
```

## Profiling with pprof

In addition to Prometheus metrics, GoVerse gates expose Go's built-in profiling endpoints via pprof when HTTP is enabled.

### Enabling pprof

The pprof profiling endpoints are automatically available when you configure the gate with an HTTP listen address:

```bash
# Start gate with HTTP enabled
go run ./cmd/gate/ -http-listen=:8080

# Or using config file
go run ./cmd/gate/ -config=config.yaml -gate-id=gate1
```

### Available Profiling Endpoints

Once the gate HTTP server is running, the following pprof endpoints are available:

- `http://localhost:8080/debug/pprof/` - Index page listing available profiles
- `http://localhost:8080/debug/pprof/heap` - Heap memory profile
- `http://localhost:8080/debug/pprof/goroutine` - Goroutine profile
- `http://localhost:8080/debug/pprof/threadcreate` - Thread creation profile
- `http://localhost:8080/debug/pprof/block` - Block profile
- `http://localhost:8080/debug/pprof/mutex` - Mutex contention profile
- `http://localhost:8080/debug/pprof/profile` - CPU profile (30-second sample by default)
- `http://localhost:8080/debug/pprof/trace` - Execution trace
- `http://localhost:8080/debug/pprof/cmdline` - Command-line invocation
- `http://localhost:8080/debug/pprof/symbol` - Symbol lookup

### Using pprof

#### Interactive Analysis

Use the `go tool pprof` command to analyze profiles interactively:

```bash
# CPU profiling (captures 30 seconds of CPU activity)
go tool pprof http://localhost:8080/debug/pprof/profile

# Heap memory profile
go tool pprof http://localhost:8080/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:8080/debug/pprof/goroutine
```

#### Generating Visualizations

Generate SVG graphs:

```bash
# CPU profile graph
go tool pprof -svg http://localhost:8080/debug/pprof/profile > cpu.svg

# Heap allocation graph
go tool pprof -svg http://localhost:8080/debug/pprof/heap > heap.svg

# Goroutine call graph
go tool pprof -svg http://localhost:8080/debug/pprof/goroutine > goroutine.svg
```

#### Web UI

Launch the interactive web UI:

```bash
go tool pprof -http=:6060 http://localhost:8080/debug/pprof/heap
```

This opens a browser with an interactive flame graph and call graph visualization.

#### Common Profiling Scenarios

**Memory Leak Investigation:**
```bash
# Take multiple heap snapshots over time
go tool pprof -base http://localhost:8080/debug/pprof/heap http://localhost:8080/debug/pprof/heap
```

**High CPU Usage:**
```bash
# Capture 60 seconds of CPU profile
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=60
```

**Goroutine Leak:**
```bash
# Check number of goroutines and their stacks
go tool pprof http://localhost:8080/debug/pprof/goroutine
```

**Lock Contention:**
```bash
# Analyze mutex contention
go tool pprof http://localhost:8080/debug/pprof/mutex
```

### Security Considerations

⚠️ **Important**: The pprof endpoints expose detailed runtime information about your application. In production:

- Use firewall rules to restrict access to the HTTP port
- Consider using authentication middleware if publicly exposed
- Monitor access to profiling endpoints
- Only enable HTTP/pprof when needed for debugging

## Testing

The metrics package includes comprehensive unit tests:

```bash
go test -v -p 1 ./util/metrics/...
```

To verify metrics are exposed:

```bash
# Start inspector
cd cmd/inspector
go run .

# Check metrics endpoint
curl http://localhost:8080/metrics | grep goverse
```

To verify pprof endpoints:

```bash
# Start gate with HTTP enabled
go run ./cmd/gate/ -http-listen=:8080

# Check pprof index
curl http://localhost:8080/debug/pprof/

# Check specific profile
curl http://localhost:8080/debug/pprof/cmdline
```

## Dependencies

The following Prometheus client library dependencies were added:

- `github.com/prometheus/client_golang v1.23.2`
- `github.com/prometheus/client_model v0.6.2`
- `github.com/prometheus/common v0.66.1`
- `github.com/prometheus/procfs v0.16.1`

All dependencies have been checked for security vulnerabilities and are clean.

## Future Enhancements

Potential future metrics to add:

- Node connection counts
- Object lifecycle duration histograms
- RPC error rates
- Persistence operation metrics
- Shard migration duration histograms
- Client request rates and latencies
- Object creation/deletion rates per shard
- gRPC connection pool metrics
