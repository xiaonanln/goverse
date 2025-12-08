# GoVerse

> ⚠️ **EARLY DEVELOPMENT STAGE** - This project is in very early development. APIs are unstable and may change significantly. Not recommended for production use.

[![Go Tests](https://github.com/xiaonanln/goverse/actions/workflows/test.yml/badge.svg)](https://github.com/xiaonanln/goverse/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/xiaonanln/goverse/branch/main/graph/badge.svg)](https://codecov.io/gh/xiaonanln/goverse)

GoVerse is a distributed object runtime for Go, inspired by Orleans.  
It provides virtual actors, automatic placement, lifecycle management, and streaming RPCs.  
Designed for building fault-tolerant backend services and large-scale real-time systems.

---

## Key Features

- Virtual objects with automatic lifecycle & activation
- Sharded placement using etcd with dynamic object & shard rebalancing across nodes
- Gate architecture with streaming gRPC
- Automatic failover & fault recovery
- Sharded placement using etcd
- Gate architecture with streaming gRPC and HTTP REST API
- Automatic rebalancing & fault recovery
- PostgreSQL persistence with JSONB storage
- Built-in Prometheus metrics and pprof profiling
- Inspector UI for visualizing object topology

---

## Why GoVerse?

- High-level programming model without giving up Go's performance
- Scales horizontally with minimal coordination
- Built for trading infra, backend automation, and real-time apps

---

## Quick Start

GoVerse uses a **Node + Gate** architecture:
- **Node** hosts distributed objects and handles object lifecycle
- **Gate** accepts client connections (gRPC and HTTP) and routes calls to nodes

### 1. Define a Distributed Object

```go
// counter.go
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int
}

// Object methods use protobuf types for HTTP/gRPC compatibility
func (c *Counter) Add(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value += int(req.GetValue())
    return &wrapperspb.Int32Value{Value: int32(c.value)}, nil
}
```

### 2. Start a Node (Object Server)

```go
// node/main.go
func main() {
    config := &goverseapi.ServerConfig{
        ListenAddress:    "localhost:47000",
        AdvertiseAddress: "localhost:47000",
    }
    server, err := goverseapi.NewServerWithConfig(config)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    goverseapi.RegisterObjectType((*Counter)(nil))
    
    // Create Counter object when cluster is ready
    go func() {
        <-goverseapi.ClusterReady()
        _, err := goverseapi.CreateObject(context.Background(), "Counter", "my-counter")
        if err != nil {
            log.Printf("Failed to create counter: %v", err)
        }
    }()
    
    server.Run(context.Background())
}
```

### 3. Start a Gate (Client-Facing Server)

```bash
# Start the gate with HTTP API enabled
go run ./cmd/gate/ -http-listen=:8080
```

### 4. Access via HTTP

Once the node creates the `my-counter` object, you can call its methods via the gate's HTTP API:

```bash
# Call object method (increment by 5)
# The request body is a base64-encoded protobuf Any containing Int32Value{Value: 5}
curl -X POST http://localhost:8080/api/v1/objects/call/Counter/my-counter/Add \
  -H "Content-Type: application/json" \
  -d '{"request":"Ci50eXBlLmdvb2dsZWFwaXMuY29tL2dvb2dsZS5wcm90b2J1Zi5JbnQzMlZhbHVlEgIIBQ=="}'
```

> See [HTTP_GATE.md](docs/design/HTTP_GATE.md) for details on encoding protobuf messages and the [httpgate example](examples/httpgate/) for a helper to generate these values.

---

## Installation

```bash
go get github.com/xiaonanln/goverse
```

---

## Documentation

Full documentation:
- [Getting Started](docs/GET_STARTED.md) - Complete guide to building with GoVerse
- [GoVerse API](docs/GOVERSEAPI.md) - API reference for the goverseapi package
- [Object Model & Architecture](docs/GET_STARTED.md#core-concepts) - Understanding virtual actors
- [Cluster Configuration](docs/CONFIGURATION.md) - YAML config and cluster settings
- [Object Persistence](docs/PERSISTENCE_IMPLEMENTATION.md) - PostgreSQL integration
- [Push Messaging](docs/PUSH_MESSAGING.md) - Real-time server-to-client delivery
- [HTTP Gate API](docs/design/HTTP_GATE.md) - REST/HTTP endpoints for clients
- [Object Access Control](docs/design/OBJECT_ACCESS_CONTROL.md) - Access rules and security
- [Prometheus Metrics](docs/PROMETHEUS_INTEGRATION.md) - Monitoring & observability
- [pprof Profiling](examples/pprof/) - Performance profiling and debugging
- [Inspector UI](docs/INSPECTOR.md) - Cluster visualization
- [Proto Reference](proto/) - Protocol buffer definitions

---

## Samples

| Sample | Description |
|--------|-------------|
| [Counter](samples/counter/) | Simple counter service demonstrating basic Goverse object operations |
| [Tic Tac Toe](samples/tictactoe/) | Web-based game demonstrating HTTP Gate with REST API |
| [Chat](samples/chat/) | Distributed chat application with real-time push messaging and web client |
| [Sharding Demo](samples/sharding_demo/) | Comprehensive demo showcasing sharding, auto-load objects, and distributed object management |

---

## Status & Roadmap

Current status: **Alpha**

Near-term goals: stability improvements, enhanced benchmarks, Inspector UI v2

See [Getting Started](docs/GET_STARTED.md) for detailed roadmap and contribution guidelines.

---

## TODO

High-level objectives for future development:

### Core System Improvements
- **Shard rebalancing based on actual node load** - Dynamic rebalancing that considers CPU, memory, and object count
- **Support different object call semantics** - Currently only best-effort; add at-least-once, exactly-once, and idempotent patterns
- **Configuration hot reload** - Support runtime configuration updates without cluster restart for access control, lifecycle rules, and other policies

### Gate & Client Features
- **Gate authorization mechanism** - Fine-grained access control and authentication for client connections
- **Gate rate limiting** - Per-client and per-object throttling to prevent abuse
- **Client reconnection & backoff** - Automatic retry logic with exponential backoff

### Performance & Scalability
- **Runtime shard count reconfiguration** - Allow dynamic shard count changes without cluster restart

### Observability & Operations
- **Enhanced metrics & alerting** - More granular Prometheus metrics, SLO tracking
- **Inspector UI enhancements** - Real-time object visualization, shard distribution graphs, and cluster health dashboard

---

## License

Apache-2.0 License
