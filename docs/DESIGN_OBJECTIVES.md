# Goverse Design and Objectives

## Overview

**Goverse** is a **distributed object runtime for Go** implementing the **virtual actor (grain) model**. It provides a comprehensive framework for building scalable, fault-tolerant distributed systems around stateful entities with identity and methods, while the runtime handles placement, routing, lifecycle, and observability.

## Core Design Principles

### 1. Simplicity First
Goverse aims to make distributed systems development accessible by:
- Providing intuitive APIs that hide complexity
- Using familiar programming patterns (methods on objects)
- Minimizing boilerplate code required
- Offering sensible defaults with opt-in customization

### 2. Scalability by Default
The architecture is designed to scale horizontally:
- Fixed sharding model (8192 shards) for predictable distribution
- Automatic shard-to-node mapping via consensus
- Efficient routing with O(1) shard lookups
- No single point of failure in runtime operations

### 3. Fault Tolerance
Built-in resilience mechanisms ensure system reliability:
- Leader election via etcd with lease-based fencing
- Epoch-based split-brain prevention
- Automatic recovery after node failures
- Idempotent operations where appropriate

### 4. Observable Systems
Comprehensive observability features enable production operations:
- Structured logging throughout the runtime
- Real-time cluster visualization (Inspector UI)
- Detailed operation logging with object type information
- Future: metrics and distributed tracing support

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Application Layer                        │
│  (Custom Objects: ChatRoom, UserProfile, GameSession, etc.)     │
└──────────────────────────┬──────────────────────────────────────┘
                           │
                           │ Extends BaseObject
                           │
┌──────────────────────────▼──────────────────────────────────────┐
│                    Goverse API (goverseapi)                      │
│  - CreateObject() / CallObject()                                │
│  - RegisterObjectType()                                         │
│  - PushMessageToClient()                                        │
└──────────────────────────┬──────────────────────────────────────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│   Server     │  │   Gate    │  │   Cluster    │
│   Runtime    │  │   System     │  │  Management  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                  │
       │                 │                  │
       ▼                 ▼                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│     Node     │  │    Gate      │  │   Sharding   │
│  Management  │  │  Registration│  │   System     │
└──────┬───────┘  └──────────────┘  └──────┬───────┘
       │                                    │
       │                                    │
       ▼                                    ▼
┌──────────────┐                    ┌──────────────┐
│   Object     │                    │ Consensus    │
│ Persistence  │                    │  Manager     │
└──────────────┘                    └──────┬───────┘
                                           │
                                           ▼
                                    ┌──────────────┐
                                    │     etcd     │
                                    │ Coordination │
                                    └──────────────┘
```

### Core Components

#### 1. Server Runtime (`server/`)
- Node server implementation with context-based shutdown
- gRPC server for node-to-node and client-to-node communication
- Graceful startup and shutdown with proper cleanup
- Configuration management (listen addresses, etcd settings, quorum)

#### 2. Node Management (`node/`)
- Core node logic and object lifecycle management
- Sophisticated multi-level locking strategy:
  - **stopMu (RWMutex)**: Protects node lifecycle operations
  - **keyLock (per-object locks)**: Serializes operations on individual objects
  - **objectsMu (RWMutex)**: Protects the objects map
- Object creation, deletion, and method invocation
- Periodic persistence for durable objects
- Thread-safe operations with deadlock prevention

#### 3. Object Framework (`object/`)
- BaseObject implementation for distributed objects
- Persistence framework with PersistenceProvider interface
- Optional PostgreSQL persistence with JSONB storage
- Thread-safe ToData()/FromData() serialization hooks
- Concurrency modes: Sequential, Concurrent, Read-only

#### 4. Gate System (`gate/`)
- Gate servers handle client connections separately from nodes
- Gate registration with nodes via bidirectional gRPC streams (`RegisterGate`)
- Client registration with gates for unique client ID assignment
- Generic object call routing from clients to distributed objects
- Push-based messaging from nodes through gates to clients
- Graceful cleanup on client disconnect and gate shutdown
- Legacy `client/` package retained for backwards compatibility (BaseClient)

#### 5. Cluster Management (`cluster/`)
- Cluster singleton for distributed coordination
- Leadership election (lexicographically smallest node)
- Automatic shard mapping management
- Node registration and discovery via etcd
- Cluster quorum configuration (MinQuorum)
- Node stability duration for rebalancing control

#### 6. Sharding System (`cluster/sharding/`)
- Fixed shard model: 8192 shards for predictable distribution
- FNV-1a hash-based object-to-shard mapping
- Round-robin shard-to-node distribution
- Shard migration support (CurrentNode vs TargetNode)
- Per-shard keys in etcd for granular updates

#### 7. Consensus Manager (`cluster/consensusmanager/`)
- Centralizes all etcd interactions
- Single watch on `/goverse` prefix for efficiency
- In-memory state: node list, shard mapping, timestamps
- Real-time notifications to state change listeners
- Thread-safe state queries (no etcd round-trips)

#### 8. etcd Manager (`cluster/etcdmanager/`)
- etcd connection management and pooling
- Node registration with automatic lease renewal
- Keep-alive monitoring and reconnection logic
- Prefix-based key isolation for multi-cluster support

#### 9. Inspector UI (`inspector/`)
- Real-time cluster visualization web interface
- Interactive graph view using Sigma.js
- Node and object relationship visualization
- Sidebar with node details and metadata
- WebSocket-based updates for live monitoring

#### 10. Persistence (`util/postgres/`)
- PostgreSQL database utilities and connection pooling
- JSONB-based flexible schema storage
- Automatic timestamp tracking (created_at, updated_at)
- Type-based indexing for efficient queries
- Support for custom persistence providers

## Key Concepts

### Virtual Actor Model

Goverse implements the virtual actor pattern where:

1. **Unique Identity**: Each object has a globally unique ID
2. **Activation on Demand**: Objects are created when first accessed
3. **Transparent Location**: Clients don't need to know which node hosts an object
4. **Automatic Lifecycle**: Runtime handles activation, deactivation, and migration
5. **State Persistence**: Objects can optionally persist state across restarts

### Distributed Objects (Grains)

Objects in Goverse are:
- **Stateful**: Maintain internal state between method calls
- **Addressable**: Can be called by ID from anywhere in the cluster
- **Type-Safe**: Strongly typed with protocol buffer messages
- **Concurrent**: Support various concurrency modes (sequential, concurrent, read-only)
- **Persistent**: Optionally durable via PostgreSQL or custom providers

Example distributed object:
```go
type ChatRoom struct {
    goverseapi.BaseObject
    mu       sync.Mutex
    users    map[string]bool
    messages []*chat_pb.ChatMessage
}

func (room *ChatRoom) Join(ctx context.Context, req *chat_pb.JoinRequest) (*chat_pb.JoinResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    room.users[req.GetUserName()] = true
    // ... implementation
}
```

### Gate Architecture

The gate system provides a clean separation between client connections and distributed objects:

1. **Gate Registration**: Gates register with nodes via `RegisterGate` streaming RPC
2. **Client Registration**: Clients connect to gates and receive a unique ID in format `gateAddress/uniqueId`
3. **Generic Object Calls**: Clients call distributed objects directly via `CallObject()` RPC
4. **Message Routing**: Gates route calls to the appropriate nodes based on shard mapping
5. **Push Messaging**: Nodes push messages to clients via gate registration streams

Benefits:
- Clean separation between client-facing gates and object-hosting nodes
- Distributed objects are the primary abstraction (no client-specific objects)
- Efficient resource management and connection pooling
- Support for long-lived connections with push messaging
- Scalable gate layer independent of object hosts

### Sharding and Distribution

Goverse uses a **fixed sharding model** with 8192 shards:

**Object Routing**:
1. Hash object ID using FNV-1a → shard ID (0-8191)
2. Look up shard → node mapping in consensus manager
3. Route request to the appropriate node
4. Node creates/activates object and executes method

**Shard Distribution**:
- Round-robin assignment: `node = sortedNodes[shardID % nodeCount]`
- Even distribution across all nodes
- Deterministic mapping based on sorted node list
- Per-shard etcd keys for granular updates

**Rebalancing**:
- Leader monitors node list changes via consensus manager
- Waits for NodeStabilityDuration (default: 10s) after last change
- Creates new mapping, preserving existing assignments where possible
- Only shards on removed nodes are reassigned
- Updates propagate via etcd watch mechanism

### Fault Tolerance

Multiple mechanisms ensure system reliability:

1. **Leader Election**:
   - Lexicographically smallest node becomes leader
   - Leader manages shard mapping and cluster coordination
   - Automatic failover when leader leaves

2. **Epoch Fencing**:
   - Each shard mapping has an epoch number
   - Prevents split-brain scenarios
   - Ensures only one valid mapping at a time

3. **Lease-Based Registration**:
   - Nodes register with etcd using leases
   - Automatic deregistration on failure/disconnect
   - Keep-alive monitoring with reconnection

4. **Idempotent Operations**:
   - DeleteObject succeeds if object already deleted
   - Safe to retry operations after failures
   - At-least-once delivery with idempotency hooks

5. **Graceful Degradation**:
   - Cluster continues operating if non-leader nodes fail
   - Objects on failed nodes recreated on healthy nodes
   - Client connections automatically cleaned up

### Concurrency Control

Goverse provides fine-grained concurrency control:

**Node-Level Locking**:
- Per-object key-based locks prevent contention
- Multiple objects can be accessed in parallel
- Lock hierarchy prevents deadlocks

**Object-Level Locking**:
- Objects use mutexes to protect internal state
- Sequential mode: one method at a time per object
- Concurrent mode: multiple methods can run in parallel
- Read-only mode: optimized for reads

**Thread-Safety Requirements**:
- All object methods must be thread-safe
- ToData()/FromData() must use mutex protection
- Periodic persistence runs in background goroutines

## Observability Features

### 1. Structured Logging

**Logger Package** (`util/logger/`):
- Consistent logging interface across all components
- Configurable log levels (Debug, Info, Error)
- Context-aware logging with operation tracking
- Object type logging for better debugging

Example usage:
```go
import "github.com/xiaonanln/goverse/util/logger"

logger.Info("Starting operation")
logger.Error("Operation failed: %v", err)
logger.Debug("Debug information: %s", details)
```

**Key Logging Points**:
- Object creation and deletion
- Method invocation with object type
- Cluster state changes (nodes joining/leaving)
- Shard mapping updates
- Client connections and disconnections
- Persistence operations (save/load)
- Error conditions with full context

### 2. Inspector UI

**Real-Time Visualization** (`inspector/`):
- Web-based interface at http://localhost:8080
- Interactive graph visualization using Sigma.js and Graphology
- Node sidebar with detailed information
- Live updates via periodic polling or WebSocket

**What You Can See**:
- All registered cluster nodes
- Node relationships and connections
- Object distribution across nodes
- Shard assignments (future)
- Health status (future)

**Inspector Service**:
- gRPC service for cluster introspection
- Queries consensus manager for current state
- Returns node list, object counts, and metadata
- Extensible for additional metrics

### 3. Operation Logging

**Enhanced CallObject Logging**:
- Every CallObject operation logs the object type
- Provides clear audit trail of operations
- Enables debugging of routing issues
- Helps identify performance bottlenecks

Example log output:
```
INFO: CallObject objType=ChatRoom id=ChatRoom-general method=SendMessage
INFO: CreateObject objType=UserProfile id=user-123
INFO: DeleteObject objType=GameSession id=game-456
```

### 4. Future Observability Enhancements

**Planned Features**:
- Prometheus metrics export (operation counts, latencies, error rates)
- Distributed tracing with OpenTelemetry
- Health check endpoints
- Performance profiling integration
- State update frequency metrics
- Query latency histograms
- Shard migration tracking

## System Objectives

### Primary Objectives

1. **Developer Productivity**
   - Reduce distributed systems complexity to simple method calls
   - Hide infrastructure concerns (placement, routing, recovery)
   - Provide clear, type-safe APIs
   - Minimize boilerplate and configuration

2. **Horizontal Scalability**
   - Support clusters from 1 to 100+ nodes
   - Linear scaling of object capacity
   - Efficient shard distribution
   - No bottlenecks in routing or coordination

3. **High Availability**
   - Continue operations during node failures
   - Automatic object migration and recovery
   - Leader failover in seconds
   - No data loss with persistence enabled

4. **Operational Visibility**
   - Real-time cluster state visualization
   - Comprehensive operation logging
   - Clear error messages and diagnostics
   - Future: metrics and distributed tracing

5. **Flexibility**
   - Support various concurrency models
   - Optional persistence with pluggable providers
   - Configurable cluster behavior (quorum, stability duration)
   - Extensible architecture for custom features

### Non-Functional Requirements

1. **Performance**
   - Sub-millisecond local object calls
   - Low-latency remote calls via gRPC
   - Efficient serialization with protobuf
   - Minimal overhead from coordination

2. **Reliability**
   - Graceful handling of network partitions
   - Recovery from etcd disconnections
   - Protection against split-brain scenarios
   - Safe concurrent access to shared state

3. **Maintainability**
   - Clear separation of concerns
   - Comprehensive test coverage
   - Well-documented interfaces
   - Consistent coding patterns

4. **Security** (Future)
   - TLS for all gRPC connections
   - Authentication and authorization
   - Encryption at rest for persistence
   - Audit logging for sensitive operations

## Use Cases

### 1. Real-Time Chat Systems
- Distributed chat rooms as objects
- Push-based message delivery through gates
- Multi-room support with independent state
- Direct object calls from clients via gate

**Example**: `samples/chat/`

### 2. Gaming Backends
- Game sessions as distributed objects
- Player state management
- Matchmaking and lobby systems
- Leaderboards with persistent storage

### 3. IoT Device Management
- Each device as a distributed object
- Command routing to devices
- State synchronization
- Event aggregation and analytics

### 4. Collaborative Applications
- Document editing with CRDT-based objects
- Real-time presence tracking
- Collaborative whiteboards
- Session management

### 5. Workflow Orchestration
- Workflow instances as objects
- State machine persistence
- Task distribution across nodes
- Long-running process management

## Configuration and Deployment

### Server Configuration

```go
config := &goverseapi.ServerConfig{
    // Node communication
    ListenAddress:    "localhost:47000",  // Internal gRPC port
    AdvertiseAddress: "localhost:47000",  // Address other nodes use
    
    // Cluster coordination
    EtcdAddress: "localhost:2379",  // etcd endpoint
    EtcdPrefix:  "/goverse",        // Key prefix for isolation
    
    // Cluster behavior
    MinQuorum:              3,                  // Minimum nodes required
    NodeStabilityDuration:  10 * time.Second,   // Rebalancing delay
}
```

### Gate Configuration

```go
config := &gateserver.GateServerConfig{
    ListenAddress:    ":49000",              // Client connection port
    AdvertiseAddress: "localhost:49000",     // Address clients use
    EtcdAddress:      "localhost:2379",      // etcd endpoint
    EtcdPrefix:       "/goverse",            // Key prefix for isolation
}
```

### Cluster Quorum

**Purpose**: Ensure cluster has sufficient nodes before accepting traffic

**Behavior**:
- Default: MinQuorum = 1 (single node development)
- Production: MinQuorum = 3 (high availability)
- Cluster marked ready when: nodes >= MinQuorum and stable

**Use Cases**:
- Prevent operations on incomplete clusters
- Coordinate rolling updates
- Ensure redundancy before traffic

### Node Stability Duration

**Purpose**: Prevent frequent rebalancing during node churn

**Behavior**:
- Default: 10 seconds
- Leader waits this duration after last node change
- Trade-off: faster convergence vs. stability

**Tuning Guidelines**:
- Development: 2-3 seconds for fast iteration
- Production stable: 10 seconds (default)
- High churn: 20-30 seconds
- Cloud deployments: adjust for VM startup patterns

### Persistence Configuration

```go
import (
    "github.com/xiaonanln/goverse/util/postgres"
    "github.com/xiaonanln/goverse/object"
)

// PostgreSQL connection
config := postgres.DefaultConfig()
config.Host = "localhost"
config.Database = "goverse"
config.User = "goverse"
config.Password = "goverse"

db, err := postgres.NewDB(config)
db.InitSchema(ctx)

// Set as node's persistence provider
provider := postgres.NewPostgresPersistenceProvider(db)
node.SetPersistenceProvider(provider)
```

## Development Workflow

### Prerequisites

```bash
# Install protoc compiler
sudo apt-get install -y protobuf-compiler  # Linux
brew install protobuf                      # macOS

# Install Go protobuf plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

### Building

```bash
# CRITICAL: Always compile proto files first
./script/compile-proto.sh

# Tidy dependencies
go mod tidy

# Build all packages
go build ./...

# Run go vet
go vet ./...
```

### Testing

```bash
# Compile proto files first
./script/compile-proto.sh

# Run all tests
go test -v -p 1 -coverprofile=coverage.out ./...

# Run specific package tests
go test -v ./server/
go test -v ./cluster/

# View coverage
go tool cover -html=coverage.out
```

### Protocol Buffer Changes

When modifying `.proto` files:
1. Edit the proto file
2. Run `./script/compile-proto.sh`
3. Update corresponding Go implementations
4. Run tests to verify changes

## Project Status and Roadmap

### Current Status

⚠️ **Early Development Stage**
- APIs are subject to breaking changes
- Features actively being developed
- Not recommended for production use
- Experimental features may change

### Recent Enhancements

✅ **Completed**:
- CallObject API with explicit object type parameter
- Improved logging for observability
- Client service with push messaging
- PostgreSQL persistence framework
- Cluster quorum configuration
- Node stability duration tuning
- Consensus manager refactoring
- Inspector UI visualization

### Roadmap

🚧 **In Progress**:
- Enhanced test coverage across all packages
- Documentation improvements
- Performance optimization

📋 **Planned**:
- Prometheus metrics export
- Distributed tracing with OpenTelemetry
- TLS support for all connections
- Authentication and authorization
- Shard migration monitoring
- Health check endpoints
- Object placement policies
- Enhanced persistence providers (Redis, MongoDB)

## Best Practices

### Object Design

1. **Thread Safety**: Always use mutexes to protect shared state
2. **Idempotency**: Design operations to be safely retryable
3. **Small State**: Keep object state manageable in size
4. **Clear Interfaces**: Use protocol buffers for type-safe communication
5. **Logging**: Log important operations for debugging

### Client Object Patterns

1. **Orchestration**: Use client objects to coordinate multiple object calls
2. **State Management**: Keep minimal client-specific state
3. **Error Handling**: Return clear errors to clients
4. **Cleanup**: Implement proper cleanup in OnDestroy()

### Cluster Operations

1. **Start Inspector**: Always run inspector for visibility
2. **Monitor Logs**: Watch for errors and warnings
3. **Test Failover**: Regularly test node failure scenarios
4. **Plan Capacity**: Size cluster based on object count and load
5. **Gradual Rollout**: Use MinQuorum for controlled deployments

### Testing

1. **Isolation**: Use unique etcd prefixes for test isolation
2. **Cleanup**: Always use t.Cleanup() for resource cleanup
3. **Parallel Tests**: Only for tests without shared resources
4. **Integration Tests**: Test real distributed scenarios
5. **Mock Servers**: Use TestServerHelper for inter-node testing

## Architecture Decisions

### Why Fixed Sharding?

**Benefits**:
- Predictable distribution across nodes
- O(1) object lookup via hash
- Simple rebalancing logic
- No coordination required for routing

**Trade-offs**:
- Fixed at 8192 shards (not configurable)
- Minimum effective cluster size (prefer 3+ nodes)

### Why etcd for Coordination?

**Benefits**:
- Proven reliability in production
- Strong consistency guarantees
- Efficient watch mechanism
- Lease-based failure detection

**Trade-offs**:
- External dependency
- Network overhead for coordination
- Limited by etcd's performance

### Why Protocol Buffers?

**Benefits**:
- Efficient binary serialization
- Strong typing with code generation
- Version compatibility (field numbers)
- Wide language support

**Trade-offs**:
- Build-time code generation required
- Learning curve for developers

### Why gRPC?

**Benefits**:
- High-performance RPC framework
- Bidirectional streaming support
- Protocol buffer integration
- Load balancing and middleware

**Trade-offs**:
- HTTP/2 complexity
- Debugging can be harder than REST

## Conclusion

Goverse provides a comprehensive, observable, and scalable framework for building distributed systems in Go. By implementing the virtual actor model with built-in observability features, it enables developers to focus on business logic while the runtime handles the complexities of distribution, routing, fault tolerance, and monitoring.

The project is designed with clear architectural principles, comprehensive logging and visualization, and a roadmap toward production-grade observability with metrics and distributed tracing. Whether building real-time chat systems, gaming backends, IoT platforms, or workflow orchestrators, Goverse provides the foundation for scalable distributed applications.

---

**License**: MIT

**Contributing**: Contributions welcome! See repository for guidelines.

**Support**: File issues on GitHub for bugs or feature requests.
