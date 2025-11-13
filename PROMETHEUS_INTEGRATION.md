# Prometheus Metrics Integration

This document describes the Prometheus metrics integration added to GoVerse.

## Overview

GoVerse now exposes Prometheus metrics for monitoring distributed objects and node connections. The metrics are automatically tracked and can be scraped by Prometheus for observability.

## Available Metrics

### Object Metrics

**`goverse_objects_total{node, type}`** - Gauge
- Description: Total number of distributed objects in the cluster
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
  - `type`: The object type (e.g., "ChatRoom", "ChatClient")
- Updated: Automatically incremented on object creation, decremented on deletion

### Node Connection Metrics

**`goverse_node_connections_total{node}`** - Gauge
- Description: Total number of connections between nodes in the cluster
- Labels:
  - `node`: The node address (e.g., "localhost:47000")
- Updated: Automatically updated when connections are established or closed

## Metrics Endpoint

The metrics are exposed via HTTP at:

```
http://<inspector-address>:8080/metrics
```

By default, the inspector runs on port 8080. The endpoint returns metrics in Prometheus text format.

## Example Output

```
# HELP goverse_objects_total Total number of distributed objects in the cluster
# TYPE goverse_objects_total gauge
goverse_objects_total{node="localhost:47000",type="ChatRoom"} 3
goverse_objects_total{node="localhost:47000",type="ChatClient"} 5
goverse_objects_total{node="localhost:47001",type="ChatRoom"} 2

# HELP goverse_node_connections_total Total number of connections between nodes in the cluster
# TYPE goverse_node_connections_total gauge
goverse_node_connections_total{node="localhost:47000"} 2
goverse_node_connections_total{node="localhost:47001"} 2
```

## Prometheus Configuration

Add the following to your `prometheus.yml` to scrape GoVerse metrics:

```yaml
scrape_configs:
  - job_name: 'goverse'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/metrics'
```

## Implementation Details

### Architecture

The metrics integration is implemented across several packages:

1. **`util/metrics`** - Core metrics package
   - Defines and registers Prometheus metrics
   - Provides helper functions for updating metrics

2. **`node`** - Node package integration
   - Tracks object creation in `createObject()`
   - Tracks object deletion in `destroyObject()`

3. **`cluster/nodeconnections`** - Node connections integration
   - Tracks connection establishment in `connectToNode()`
   - Tracks connection closure in `disconnectFromNode()`

4. **`cmd/inspector`** - Inspector HTTP server
   - Exposes `/metrics` endpoint using `promhttp.Handler()`

### Automatic Tracking

Metrics are automatically updated without requiring manual intervention:

- **Object Creation**: When `node.createObject()` successfully creates an object, the object count is incremented
- **Object Deletion**: When `node.destroyObject()` removes an object, the object count is decremented
- **Connection Established**: When a node connection is established, the connection count is updated
- **Connection Closed**: When a node connection is closed, the connection count is updated

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

Total connections across all nodes:
```promql
sum(goverse_node_connections_total)
```

### Alerting Examples

Alert when a node loses all connections:
```yaml
- alert: NodeDisconnected
  expr: goverse_node_connections_total == 0
  for: 1m
  annotations:
    summary: "Node {{ $labels.node }} has no connections"
```

Alert when object count is too high:
```yaml
- alert: HighObjectCount
  expr: goverse_objects_total{node=~".+"} > 10000
  for: 5m
  annotations:
    summary: "Node {{ $labels.node }} has high object count"
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

- Object method call latency histograms
- Object lifecycle duration histograms
- RPC error rates
- Persistence operation metrics
- Shard mapping change frequency
- Client connection counts
