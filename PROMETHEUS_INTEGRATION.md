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
  - `shard`: The shard number (e.g., "0", "100")
- Updated: Automatically incremented on object creation, decremented on deletion

### Shard Metrics

**`goverse_shards_total{node}`** - Gauge
- Description: Total number of shards assigned to each node
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically updated when shard mapping changes in the ConsensusManager
- Note: Counts shards based on CurrentNode (actual ownership), not TargetNode (planned assignment)
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
    ClientListenAddress:  "localhost:48000",
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
goverse_objects_total{node="localhost:47000",type="ChatRoom"} 3
goverse_objects_total{node="localhost:47000",type="ChatClient"} 5
goverse_objects_total{node="localhost:47001",type="ChatRoom"} 2

# HELP goverse_shards_total Total number of shards assigned to each node
# TYPE goverse_shards_total gauge
goverse_shards_total{node="localhost:47000"} 2048
goverse_shards_total{node="localhost:47001"} 2048
goverse_shards_total{node="localhost:47002"} 2048
goverse_shards_total{node="localhost:47003"} 2048
goverse_objects_total{node="localhost:47000",type="ChatRoom",shard="0"} 3
goverse_objects_total{node="localhost:47000",type="ChatClient",shard="1"} 5
goverse_objects_total{node="localhost:47001",type="ChatRoom",shard="2"} 2

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

## Testing

The metrics package includes comprehensive unit tests:

```bash
go test -v ./util/metrics/...
```

To verify metrics are exposed:

```bash
# Start inspector
cd cmd/inspector
go run .

# Check metrics endpoint
curl http://localhost:8080/metrics | grep goverse
```

## Dependencies

The following Prometheus client library dependencies were added:

- `github.com/prometheus/client_golang v1.23.2`
- `github.com/prometheus/client_model v0.6.2`
- `github.com/prometheus/common v0.66.1`
- `github.com/prometheus/procfs v0.16.1`

All dependencies have been checked for security vulnerabilities and are clean.

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
```

## Future Enhancements

Potential future metrics to add:

- Node connection counts
- Object lifecycle duration histograms
- RPC error rates
- Persistence operation metrics
- Shard mapping change frequency
- Client connection counts
- Shard migration duration and frequency
- Shard ownership transition tracking (TargetNode vs CurrentNode)
- Client request rates and latencies
