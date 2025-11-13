# Prometheus Metrics Integration

This document describes the Prometheus metrics integration added to GoVerse.

## Overview

GoVerse now exposes Prometheus metrics for monitoring distributed objects. The metrics are automatically tracked and can be scraped by Prometheus for observability.

## Available Metrics

### Object Metrics

**`goverse_objects_total{node, type}`** - Gauge
- Description: Total number of distributed objects in the cluster
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `type`: The object type (e.g., "ChatRoom", "ChatClient")
- Updated: Automatically incremented on object creation, decremented on deletion

### Shard Metrics

**`goverse_shards_total{node}`** - Gauge
- Description: Total number of shards assigned to each node
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically updated when shard mapping changes in the ConsensusManager
- Note: Counts shards based on CurrentNode (actual ownership), not TargetNode (planned assignment)

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

### Automatic Tracking

Metrics are automatically updated without requiring manual intervention:

- **Object Creation**: When `node.createObject()` successfully creates an object, the object count is incremented
- **Object Deletion**: When `node.destroyObject()` removes an object, the object count is decremented
- **Shard Mapping Changes**: When `ConsensusManager` handles shard events or initializes, the shard count per node is updated

### Thread Safety

All metrics operations are thread-safe and use Prometheus client library's built-in synchronization.

## Usage Examples

### Querying Metrics with PromQL

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

## Future Enhancements

Potential future metrics to add:

- Node connection counts
- Object method call latency histograms
- Object lifecycle duration histograms
- RPC error rates
- Persistence operation metrics
- Shard mapping change frequency
- Client connection counts
- Shard migration duration and frequency
- Shard ownership transition tracking (TargetNode vs CurrentNode)
