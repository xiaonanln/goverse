# Goverse pprof Example

This example demonstrates how to enable pprof profiling in a Goverse node server.

## Overview

pprof is Go's built-in profiling tool that helps you:
- Identify CPU bottlenecks
- Find memory leaks
- Analyze goroutine usage
- Track mutex contention
- Generate execution traces

## Running the Example

1. Make sure etcd is running:
```bash
# Install etcd if needed
docker run -d --name etcd -p 2379:2379 quay.io/coreos/etcd:latest \
  /usr/local/bin/etcd --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://localhost:2379
```

2. Start the node with pprof enabled:
```bash
go run main.go
```

The node will start with:
- gRPC server on `localhost:47000`
- Metrics and pprof on `http://localhost:9090`

## Using pprof

### Available Endpoints

- `http://localhost:9090/debug/pprof/` - Index of available profiles
- `http://localhost:9090/debug/pprof/heap` - Heap memory profile
- `http://localhost:9090/debug/pprof/goroutine` - Goroutine profile  
- `http://localhost:9090/debug/pprof/profile` - CPU profile
- `http://localhost:9090/debug/pprof/trace` - Execution trace
- `http://localhost:9090/metrics` - Prometheus metrics (always available)

### Example Commands

**View heap memory profile:**
```bash
go tool pprof http://localhost:9090/debug/pprof/heap
```

**Profile CPU for 30 seconds:**
```bash
go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30
```

**View goroutines:**
```bash
go tool pprof http://localhost:9090/debug/pprof/goroutine
```

**Generate execution trace:**
```bash
curl http://localhost:9090/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```

**Interactive pprof web UI:**
```bash
go tool pprof -http=:8081 http://localhost:9090/debug/pprof/heap
# Opens browser at http://localhost:8081
```

## Configuration Options

### Via Code

```go
config := &goverseapi.ServerConfig{
    ListenAddress:        "localhost:47000",
    AdvertiseAddress:     "localhost:47000",
    MetricsListenAddress: "localhost:9090", // Required for pprof
    EnablePprof:          true,              // Enable pprof endpoints
}
```

### Via CLI Flag

```bash
go run main.go \
  --listen localhost:47000 \
  --advertise localhost:47000 \
  --http-listen localhost:9090 \
  --enable-pprof
```

### Via Config File

```yaml
version: 1
cluster:
  shards: 8192
  provider: etcd
  etcd:
    endpoints: ["localhost:2379"]
    prefix: /goverse

nodes:
  - id: node1
    grpc_addr: localhost:47000
    advertise_addr: localhost:47000
    http_addr: localhost:9090
    enable_pprof: true  # Enable pprof
```

## Security Note

**Important:** pprof endpoints can expose sensitive information about your application's internals and performance characteristics. 

- pprof is **disabled by default** for security
- Only enable it in development or when actively debugging
- In production, consider:
  - Restricting access to the metrics port with firewall rules
  - Using authentication/authorization middleware
  - Only enabling temporarily when needed for troubleshooting
