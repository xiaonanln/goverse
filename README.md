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
- Sharded placement using etcd
- Gate architecture with streaming gRPC and HTTP REST API
- Automatic rebalancing & fault recovery
- PostgreSQL persistence with JSONB storage
- Built-in Prometheus metrics
- Inspector UI for visualizing object topology

---

## Why GoVerse?

- High-level programming model without giving up Go's performance
- Scales horizontally with minimal coordination
- Built for trading infra, backend automation, and real-time apps

---

## Quick Start

```go
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int
}

func (c *Counter) Add(ctx context.Context, n int) (int, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value += n
    return c.value, nil
}

func main() {
    config := &goverseapi.ServerConfig{
        ListenAddress:    "localhost:47000",
        AdvertiseAddress: "localhost:47000",
    }
    server, _ := goverseapi.NewServer(config)
    goverseapi.RegisterObjectType((*Counter)(nil))
    
    // Create and call object
    goverseapi.CreateObject(ctx, "Counter", "Counter-1", nil)
    resp, _ := goverseapi.CallObject(ctx, "Counter-1", "Add", 5)
    fmt.Println("Counter value:", resp.(int))
    
    server.Run()
}
```

---

## Installation

```bash
go get github.com/xiaonanln/goverse
```

---

## Documentation

Full documentation:
- [Getting Started](docs/GET_STARTED.md) - Complete guide to building with GoVerse
- [Object Model & Architecture](docs/GET_STARTED.md#core-concepts) - Understanding virtual actors
- [Cluster Configuration](docs/GET_STARTED.md#cluster-configuration) - Quorum & stability settings
- [Object Persistence](docs/GET_STARTED.md#object-persistence) - PostgreSQL integration
- [Push Messaging](docs/PUSH_MESSAGING.md) - Real-time server-to-client delivery
- [Prometheus Metrics](docs/PROMETHEUS_INTEGRATION.md) - Monitoring & observability
- [Inspector UI](cmd/inspector/) - Cluster visualization
- [Chat Sample](samples/chat/) - Complete distributed chat example
- [API Reference](proto/) - Protocol buffer definitions

---

## Status & Roadmap

Current status: **Alpha**

Near-term goals: stability improvements, enhanced benchmarks, Inspector UI v2

See [Getting Started](docs/GET_STARTED.md) for detailed roadmap and contribution guidelines.

---

## TODO

High-level objectives for future development:

### Core System Improvements
- **Better leader election strategy** - Improve stability and reduce failover time during node transitions
- **Shard rebalancing based on actual node load** - Dynamic rebalancing that considers CPU, memory, and object count
- **Support different object call semantics** - Currently only best-effort; add at-least-once, exactly-once, and idempotent patterns
- **RPC timeout configuration** - Configurable timeouts for CallObject/CreateObject with proper propagation across node boundaries (see [TIMEOUT_DESIGN.md](docs/TIMEOUT_DESIGN.md))

### Gate & Client Features
- **Gate HTTP client support** - REST/HTTP endpoints alongside gRPC for broader client compatibility (see [HTTP_GATE.md](docs/design/HTTP_GATE.md))
- **Gate authorization mechanism** - Fine-grained access control and authentication for client connections
- **Gate object call filtering** - Whitelist/blacklist patterns for security and API governance
- **Gate rate limiting** - Per-client and per-object throttling to prevent abuse
- **Client reconnection & backoff** - Automatic retry logic with exponential backoff

### Performance & Scalability
- **Configurable shard count** - Move beyond fixed 8192 shards for extreme scale

### Observability & Operations
- **Enhanced metrics & alerting** - More granular Prometheus metrics, SLO tracking
- **Chaos engineering tools** - Inject failures for resilience testing

---

## License

MIT License
