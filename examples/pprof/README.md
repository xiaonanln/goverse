# Goverse pprof Example

This example demonstrates how to use pprof profiling in a Goverse node server. pprof is automatically enabled when the HTTP server is configured.

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

2. Start the node:
```bash
go run main.go
```

The node will start with:
- gRPC server on `localhost:47000`
- Metrics and pprof on `http://localhost:9090` (pprof is automatically enabled)

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

## Configuration

pprof is automatically enabled when you configure an HTTP server. Simply set the `MetricsListenAddress`:

### Via Code

```go
config := &goverseapi.ServerConfig{
    ListenAddress:        "localhost:47000",
    AdvertiseAddress:     "localhost:47000",
    MetricsListenAddress: "localhost:9090", // pprof automatically enabled
}
```

### Via CLI Flag

```bash
go run main.go \
  --listen localhost:47000 \
  --advertise localhost:47000 \
  --http-listen localhost:9090
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
    http_addr: localhost:9090  # pprof automatically enabled
```

## Security Note

**⚠️ CRITICAL: pprof endpoints expose sensitive runtime information that could be exploited by attackers:**

- **Memory dumps** - Full heap snapshots can reveal secrets, keys, tokens, and business logic
- **Stack traces** - Expose code paths, function names, and internal architecture
- **Goroutine details** - Reveal concurrent operations and timing information
- **CPU profiles** - Show performance characteristics and hot code paths

### Recommendations

- **Development**: Safe to use pprof for debugging and profiling
- **Production**: 
  - ⛔ **Do NOT expose** the HTTP server to public networks
  - ✅ Use firewall rules to restrict access to trusted IPs only
  - ✅ Deploy authentication/authorization middleware if HTTP server is needed
  - ✅ Only enable HTTP server temporarily when troubleshooting issues
  - ✅ Consider using a VPN or bastion host for accessing diagnostics

pprof is automatically enabled when you configure `MetricsListenAddress`. Control access by controlling who can reach the HTTP port.
