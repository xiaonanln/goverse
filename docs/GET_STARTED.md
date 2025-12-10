# Getting Started with GoVerse

**GoVerse** is a **distributed object runtime for Go**, implementing the **virtual actor (grain) model**.
It lets you build systems around **stateful entities with identity and methods**, while the runtime handles placement, routing, lifecycle, and fault-tolerance.

---

## Table of Contents

- [Installation](#installation)
- [Core Concepts](#core-concepts)
- [Project Structure](#project-structure)
- [Gate Architecture](#gate-architecture)
- [Quick Start Tutorial](#quick-start-tutorial)
- [HTTP API](#http-api)
- [Object Access Control](#object-access-control)
- [Chat Application Example](#chat-application-example)
- [Cluster Configuration](#cluster-configuration)
- [Object Persistence](#object-persistence)
- [Monitoring with Prometheus](#monitoring-with-prometheus)
- [Testing](#testing)

---

## Installation

Install GoVerse using Go modules:

```bash
go get github.com/xiaonanln/goverse
```

---

## Core Concepts

### Distributed Objects (Grains)
- Uniquely addressable, stateful entities with custom methods
- Each object has a unique ID and type
- Objects can call methods on other objects using `CallObject()`

### Virtual Actor Lifecycle
- Objects are activated on demand when first accessed
- Deactivated automatically when idle
- Reactivated seamlessly when needed again
- The runtime manages all lifecycle transitions

### Sharding & Placement
- Configurable shard count (default 8192) for flexible scaling
- Automatic shard-to-node mapping managed via etcd
- Objects placed on nodes based on their shard assignment
- Automatic rebalancing when nodes join or leave

### Fault-Tolerance
- Lease + epoch fencing prevent split-brain scenarios
- Safe recovery after node failures
- Automatic object migration during rebalancing

### Concurrency Model

**Important**: GoVerse's concurrency model differs from traditional actor frameworks:

- **Traditional Actors (Orleans, Akka)**: Serialize method calls per actor. Only one method runs at a time per actor (turn-based concurrency). Objects don't need internal locks.

- **GoVerse Objects**: Allow **concurrent method execution** on the same object. Multiple method calls can run simultaneously on the same object instance.

**Why this design?**
- Higher throughput for read-heavy workloads
- Better CPU utilization
- More flexibility for object developers
- Natural fit for Go's concurrency model

**Implications for developers:**
1. **Must protect shared state** using `sync.Mutex`, `sync.RWMutex`, or atomic operations
2. **Must consider race conditions** when accessing object fields
3. **Can leverage Go's concurrency** primitives (goroutines, channels) within objects
4. **Can optimize** with read/write locks for read-heavy patterns

**Example:**
```go
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex  // Required: protects value
    value int
}

func (c *Counter) Add(ctx context.Context, req *pb.AddRequest) (*pb.CounterResponse, error) {
    c.mu.Lock()  // Protect from concurrent Add/Get calls
    defer c.mu.Unlock()
    c.value += int(req.Amount)
    return &pb.CounterResponse{Value: int32(c.value)}, nil
}
```

**Alternative - Lock-free with atomics:**
```go
type AtomicCounter struct {
    goverseapi.BaseObject
    value atomic.Int32  // Lock-free operations
}

func (c *AtomicCounter) Add(ctx context.Context, req *pb.AddRequest) (*pb.CounterResponse, error) {
    newValue := c.value.Add(req.Amount)
    return &pb.CounterResponse{Value: newValue}, nil
}
```

---

## Project Structure

- `cmd/inspector/` – Inspector web server for cluster visualization
- `cmd/gate/` – Gate server binary for client connections (gRPC + HTTP)
- `server/` – Node server implementation with context-based shutdown
- `node/` – Core node logic and object management
- `object/` – Object base types and helpers (BaseObject)
- `object_access/` – Object access control validation
- `gate/` – Gate implementation for handling client connections
- `goverseapi/` – High-level API for applications
- `client/` – Client protocol definitions
- `cluster/` – Cluster singleton management, leadership election, and automatic shard mapping
- `cluster/sharding/` – Shard-to-node mapping with configurable shard count
- `cluster/etcdmanager/` – etcd connection management and node registry
- `config/` – YAML configuration parsing and access rules
- `util/postgres/` – PostgreSQL persistence utilities and JSONB storage
- `samples/counter/` – Simple counter service demonstrating basic operations
- `samples/tictactoe/` – Web-based game with HTTP Gate API
- `samples/chat/` – Distributed chat with real-time push messaging and web client
- `examples/persistence/` – Example of using PostgreSQL persistence
- `examples/minquorum/` – Example demonstrating cluster quorum configuration
- `proto/` – GoVerse protocol definitions
- `util/` – Logging and utility helpers

---

## Gate Architecture

GoVerse features a **Gate** system that enables seamless communication between clients and distributed objects.

### Core Components

**1. Gate Server**
- Separate `cmd/gate/` binary that handles client connections
- Gates connect to GoVerse nodes to route calls to distributed objects
- Provides `GateService` for client-facing RPCs

**2. Client Registration System**
- Clients register with the gate via `Register()` streaming RPC on the `GateService`
- Each client receives a unique ID in format `gateAddress/uniqueId` (e.g., `localhost:49000/AAZEDvtPr4JHP6WtybiD`)
- Persistent connections maintained for bidirectional communication

**3. Connection Lifecycle Management**
```go
// Client-side registration with gate
stream, err := client.Register(ctx, &gate_pb.Empty{})

// Receive registration response with client ID
regResp, _ := stream.Recv()
clientID := regResp.(*gate_pb.RegisterResponse).ClientId
```

**4. Generic Object Call Interface**
- Clients call distributed objects via the `CallObject()` RPC with object type, ID, method name, and request
- Gate routes calls to the appropriate node based on object ID shard mapping
- No client-specific objects on the server; all logic lives in distributed objects

**5. Push Messaging**
- Gates register with each node via `RegisterGate()` streaming RPC
- Nodes can push messages to clients via the gate using `PushMessageToClient()`
- Enables real-time notifications, chat messages, and event delivery

### Gate Connection Flow

1. **Gate Startup**: Gate connects to etcd and discovers nodes in the cluster
2. **Gate Registration**: Gate registers with each node via `RegisterGate()` streaming RPC
3. **Client Registration**: Client connects to gate and calls `Register()` to get a unique client ID
4. **Bidirectional Communication**: Client can call objects AND receive pushed messages
5. **Object Invocation**: Client calls distributed objects through `CallObject()` RPC
6. **Node Routing**: Gate routes calls to the appropriate node based on object shard
7. **Real-time Updates**: Nodes push messages to clients via the gate's registration stream
8. **Graceful Cleanup**: Client connections and gate registrations cleaned up on disconnect

### Usage Example

```go
// Connect to gate
conn, _ := grpc.Dial(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
client := gate_pb.NewGateServiceClient(conn)

// Register and get client ID
stream, _ := client.Register(ctx, &gate_pb.Empty{})
regResp, _ := stream.Recv()
clientID := regResp.(*gate_pb.RegisterResponse).ClientId

// Call distributed objects directly
anyReq, _ := anypb.New(&chat_pb.ChatRoom_JoinRequest{
    UserName: "alice",
    ClientId: clientID,
})
resp, _ := client.CallObject(ctx, &gate_pb.CallObjectRequest{
    ClientId: clientID,
    Type:     "ChatRoom",
    Id:       "ChatRoom-General",
    Method:   "Join",
    Request:  anyReq,
})
```

This design cleanly separates client connectivity from object management, with distributed objects serving as the core abstraction.

### HTTP API

The gate also provides an HTTP/JSON API for clients that prefer REST-style access:

```bash
# Call an object method via HTTP
curl -X POST http://localhost:8080/api/v1/objects/call/Counter/my-counter/Add \
  -H "Content-Type: application/json" \
  -d '{"request":"<base64-encoded-protobuf>"}'
```

Start the gate with HTTP enabled:

```bash
go run ./cmd/gate/ -http-listen=:8080
```

See [HTTP_GATE.md](design/HTTP_GATE.md) for full HTTP API documentation.

---

## Quick Start Tutorial

### Step 1: Define Your Distributed Object

```go
package main

import (
    "context"
    "sync"
    "github.com/xiaonanln/goverse/goverseapi"
)

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

func (c *Counter) Get(ctx context.Context) (int, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    return c.value, nil
}
```

### Step 2: Set Up the Server

```go
func main() {
    config := &goverseapi.ServerConfig{
        ListenAddress:    "localhost:47000",
        AdvertiseAddress: "localhost:47000",
    }
    
    server, err := goverseapi.NewServerWithConfig(config)
    if err != nil {
        panic(err)
    }
    
    // Register object types
    goverseapi.RegisterObjectType((*Counter)(nil))
    
    // Start server (blocks until shutdown)
    server.Run()
}
```

### Step 3: Create and Use Objects

```go
ctx := context.Background()

// Create a counter object
_, err := goverseapi.CreateObject(ctx, "Counter", "Counter-1", nil)
if err != nil {
    panic(err)
}

// Call methods on the object
resp, err := goverseapi.CallObject(ctx, "Counter-1", "Add", 5)
if err != nil {
    panic(err)
}

value := resp.(int)
fmt.Printf("Counter value: %d\n", value)
```

---

## Object Access Control

GoVerse supports config-based access control to restrict which clients and nodes can access specific object types and methods.

### Configuration

Add `object_access_rules` to your YAML config:

```yaml
# config.yaml
object_access_rules:
  # Allow clients to call specific ChatRoom methods
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    method: /(Join|Leave|SendMessage|GetMessages)/
    access: ALLOW
  
  # Internal methods only accessible by other objects
  - type: ChatRoom
    access: INTERNAL

  # Counter: clients can increment/decrement/get
  - type: Counter
    method: /(Increment|Decrement|Get)/
    access: ALLOW

  # Default: deny everything else
  - type: /.*/
    access: REJECT
```

### Access Levels

| Level | Client (via Gate) | Node (object-to-object) |
|-------|-------------------|-------------------------|
| `REJECT` | ✗ Denied | ✗ Denied |
| `INTERNAL` | ✗ Denied | ✓ Allowed |
| `EXTERNAL` | ✓ Allowed | ✗ Denied |
| `ALLOW` | ✓ Allowed | ✓ Allowed |

### Pattern Matching

- **Literal strings**: Exact match (e.g., `ChatRoom`)
- **Regex patterns**: Use `/pattern/` syntax (auto-anchored, e.g., `/[a-zA-Z0-9_-]+/`)
- **Optional fields**: Omit `id` or `method` to match all

See [OBJECT_ACCESS_CONTROL.md](design/OBJECT_ACCESS_CONTROL.md) for full documentation.

---

## Chat Application Example

The chat system demonstrates how distributed objects work together in a real application with the gate architecture.

### ChatRoom Object (Distributed Object)

```go
type ChatRoom struct {
    goverseapi.BaseObject
    users     map[string]bool          // userName -> bool
    clientIDs map[string]string        // userName -> clientID for push notifications
    messages  []*chat_pb.ChatMessage
    mu        sync.Mutex
}

func (room *ChatRoom) Join(ctx context.Context, req *chat_pb.ChatRoom_JoinRequest) (*chat_pb.ChatRoom_JoinResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    userName := req.GetUserName()
    clientID := req.GetClientId()
    
    room.users[userName] = true
    if clientID != "" {
        room.clientIDs[userName] = clientID  // Store for push messaging
    }
    
    return &chat_pb.ChatRoom_JoinResponse{
        RoomName: room.Name(),
        RecentMessages: []*chat_pb.ChatMessage{ /* recent messages */ },
    }, nil
}

func (room *ChatRoom) SendMessage(ctx context.Context, req *chat_pb.ChatRoom_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    chatMsg := &chat_pb.ChatMessage{
        UserName:  req.GetUserName(),
        Message:   req.GetMessage(),
        Timestamp: time.Now().UnixMicro(),
    }
    room.messages = append(room.messages, chatMsg)
    
    // Push message to all connected clients in the room
    notification := &chat_pb.Client_NewMessageNotification{
        Message: chatMsg,
    }
    for userName, clientID := range room.clientIDs {
        if userName == req.GetUserName() {
            continue  // Don't send to sender
        }
        goverseapi.PushMessageToClient(ctx, clientID, notification)
    }
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### Server Setup

```go
func main() {
    config := &goverseapi.ServerConfig{
        ListenAddress:    "localhost:47000",
        AdvertiseAddress: "localhost:47000",
    }
    server, err := goverseapi.NewServerWithConfig(config)
    if err != nil {
        panic(err)
    }
    
    // Register object types
    goverseapi.RegisterObjectType((*ChatRoom)(nil))
    goverseapi.RegisterObjectType((*ChatRoomMgr)(nil))
    
    // Create ChatRoomMgr when cluster is ready
    go func() {
        <-goverseapi.ClusterReady()
        goverseapi.CreateObject(context.Background(), "ChatRoomMgr", "ChatRoomMgr0")
    }()
    
    server.Run()
}
```

### Client Usage

```go
// Connect to gate
client := gate_pb.NewGateServiceClient(conn)
stream, _ := client.Register(ctx, &gate_pb.Empty{})
regResp, _ := stream.Recv()
clientID := regResp.(*gate_pb.RegisterResponse).ClientId

// Join a chat room
anyReq, _ := anypb.New(&chat_pb.ChatRoom_JoinRequest{
    UserName: "alice",
    ClientId: clientID,
})
client.CallObject(ctx, &gate_pb.CallObjectRequest{
    ClientId: clientID,
    Type:     "ChatRoom",
    Id:       "ChatRoom-General",
    Method:   "Join",
    Request:  anyReq,
})

// Send a message
anyReq, _ = anypb.New(&chat_pb.ChatRoom_SendChatMessageRequest{
    UserName: "alice",
    Message:  "Hello everyone!",
})
client.CallObject(ctx, &gate_pb.CallObjectRequest{
    ClientId: clientID,
    Type:     "ChatRoom",
    Id:       "ChatRoom-General",
    Method:   "SendMessage",
    Request:  anyReq,
})
```

### Chat Messaging Architecture

The chat system provides real-time message delivery through distributed ChatRoom objects:

- **Push-based Delivery**: Messages are pushed to clients in real-time via gate streams
- **Gate Routing**: Gates register with nodes and route push messages to connected clients
- **Polling Support**: Clients can also poll for recent messages using `GetRecentMessages()`
- **Timestamp Tracking**: Messages include microsecond timestamps for ordering and filtering
- **Message Persistence**: Chat history stored in distributed ChatRoom objects
- **Multi-room Support**: Each room is an independent distributed object with its own state
- **Client ID Format**: Client IDs are in format `gateAddress/uniqueId` for efficient routing

See [PUSH_MESSAGING.md](PUSH_MESSAGING.md) for implementation details.

### Running the Chat Application

1. **Start the inspector UI (optional):**
   ```bash
   go run ./cmd/inspector/
   # Open http://localhost:8080 to visualize the cluster
   ```

2. **Start the chat server (node):**
   ```bash
   go run ./samples/chat/server/
   # Starts node server on port 47000 (node-to-node communication)
   ```

3. **Start the gate:**
   ```bash
   go run ./cmd/gate/
   # Starts gate on port 49000 (client connections)
   ```

4. **Start multiple chat clients:**
   ```bash
   # Terminal 1
   go run ./samples/chat/client/ -server=localhost:49000 -user=alice

   # Terminal 2
   go run ./samples/chat/client/ -server=localhost:49000 -user=bob
   ```

### Client Commands

- `/list` - List available chat rooms
- `/join <room>` - Join a chat room
- `/messages` - View recent messages in current room
- `/help` - Show available commands
- `/quit` - Exit the client
- Type any message to send to current room

---

## Cluster Configuration

### Minimum Node Requirement (Quorum)

GoVerse allows you to configure a minimum number of nodes required for the cluster to be considered stable and ready. This is useful for ensuring high availability and preventing operations on incomplete clusters.

```go
config := &goverseapi.ServerConfig{
    ListenAddress:    "localhost:7001",
    AdvertiseAddress: "localhost:7001",
    EtcdAddress:      "localhost:2379",
    EtcdPrefix:       "/goverse",
    MinQuorum:        3, // Require at least 3 nodes before cluster is ready
}

server, err := goverseapi.NewServerWithConfig(config)
```

**Key Points:**
- **Default**: If not set, `MinQuorum` defaults to 1
- **Cluster Ready**: The cluster is marked as ready only when:
  - Number of registered nodes >= `MinQuorum`
  - Node list has been stable for the configured duration (10 seconds)
  - Shard mapping has been successfully created
- **Leader Behavior**: The leader node will wait for `MinQuorum` before creating shard mapping
- **Scaling**: When the cluster has fewer nodes than `MinQuorum`, it waits for more nodes to join

**Example Use Cases:**
- **Production Deployments**: Set `MinQuorum=3` for a 3-node cluster to ensure redundancy
- **High Availability**: Prevent operations until sufficient nodes are available
- **Rolling Updates**: Coordinate cluster startup during deployments

See the [minquorum example](../examples/minquorum/) for a complete demonstration.

### Node Stability Duration

GoVerse allows you to configure how long the cluster waits for the node list to stabilize before updating shard mapping. This is useful for environments with varying levels of node churn.

```go
config := &goverseapi.ServerConfig{
    ListenAddress:         "localhost:7001",
    AdvertiseAddress:      "localhost:7001",
    EtcdAddress:           "localhost:2379",
    EtcdPrefix:            "/goverse",
    MinQuorum:             1,
    NodeStabilityDuration: 5 * time.Second, // Wait 5s for stability (default: 10s)
}

server, err := goverseapi.NewServerWithConfig(config)
```

**Key Points:**
- **Default**: If not set, `NodeStabilityDuration` defaults to 10 seconds
- **Purpose**: Prevents frequent shard reassignments during node churn
- **Leader Behavior**: The leader waits for this duration after the last node change before updating shard mapping
- **Trade-offs**: 
  - Shorter duration (e.g., 3s): Faster cluster convergence, but may cause more frequent rebalancing
  - Longer duration (e.g., 30s): More stable in high-churn environments, but slower to react to changes

**Example Use Cases:**
- **Development/Testing**: Set to 2-3 seconds for faster iteration
- **Production with stable nodes**: Use default 10 seconds for balanced behavior
- **High-churn environments**: Set to 20-30 seconds to avoid premature rebalancing
- **Cloud deployments**: Adjust based on typical VM startup/shutdown patterns

---

## Object Persistence

GoVerse supports optional PostgreSQL persistence for distributed objects, enabling durable state management across server restarts.

### Features

- **JSONB Storage**: Objects are serialized to JSONB for flexible schema and efficient queries
- **Automatic Timestamps**: Created and updated timestamps tracked automatically
- **Type-based Indexing**: Fast lookups by object type
- **Flexible Interface**: Easy to implement custom persistence providers

### Quick Start

**1. Set up PostgreSQL** (see [POSTGRES_SETUP.md](POSTGRES_SETUP.md)):

```bash
# Create database and user
sudo -u postgres psql
CREATE DATABASE goverse;
CREATE USER goverse WITH PASSWORD 'goverse';
GRANT ALL PRIVILEGES ON DATABASE goverse TO goverse;
```

**2. Create a Persistent Object** ⚠️ **Thread-Safe Implementation Required**:

```go
import (
    "sync"
    "github.com/xiaonanln/goverse/object"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/structpb"
)

type UserProfile struct {
    object.BaseObject
    mu       sync.Mutex  // REQUIRED: Protects concurrent access
    Username string
    Email    string
    Score    int
}

// ToData must be thread-safe - called during periodic persistence
func (u *UserProfile) ToData() (proto.Message, error) {
    u.mu.Lock()
    defer u.mu.Unlock()
    
    return structpb.NewStruct(map[string]interface{}{
        "username": u.Username,
        "email":    u.Email,
        "score":    u.Score,
    })
}

// FromData must be thread-safe - called during initialization
func (u *UserProfile) FromData(data proto.Message) error {
    structData, ok := data.(*structpb.Struct)
    if !ok {
        return nil
    }
    
    u.mu.Lock()
    defer u.mu.Unlock()
    
    if username, ok := structData.Fields["username"]; ok {
        u.Username = username.GetStringValue()
    }
    // ... load other fields
    return nil
}
```

**Important**: `ToData()` and `FromData()` MUST use mutex protection because:
- Periodic persistence runs in background goroutines
- Objects may process requests while being persisted
- Race conditions can cause data corruption without proper locking

**3. Save and Load**:

```go
import (
    "github.com/xiaonanln/goverse/object"
    "github.com/xiaonanln/goverse/util/postgres"
)

// Connect to database
config := postgres.DefaultConfig()
db, _ := postgres.NewDB(config)
db.Ping(ctx)  // Verify connection
db.InitSchema(ctx)

// Create provider
provider := postgres.NewPostgresPersistenceProvider(db)

// Save object
user := &UserProfile{}
user.OnInit(user, "user-123", nil)
user.Username = "alice"
object.SaveObject(ctx, provider, user)

// Load object
loadedUser := &UserProfile{}
loadedUser.OnInit(loadedUser, "user-123", nil)
object.LoadObject(ctx, provider, loadedUser, "user-123")
```

### Examples and Documentation

- **Full Example**: See `examples/persistence/main.go` for a complete working example
- **Setup Guide**: See `docs/POSTGRES_SETUP.md` for detailed PostgreSQL setup instructions
- **Production Guide**: Learn about SSL, connection pooling, and performance tuning

---

## Monitoring with Prometheus

GoVerse includes built-in Prometheus metrics for comprehensive observability.

### Available Metrics

- **`goverse_objects_total`**: Total number of distributed objects by node, type, and shard
- **`goverse_method_calls_total`**: Method call count by node, object type, method, and status
- **`goverse_method_call_duration`**: Method call duration histogram with configurable buckets
- **`goverse_shards_total`**: Number of shards assigned to each node
- **`goverse_shards_migrating`**: Number of shards currently being migrated
- **`goverse_shard_claims_total`**: Counter for shard ownership claims
- **`goverse_shard_releases_total`**: Counter for shard ownership releases
- **`goverse_shard_migrations_total`**: Counter for completed shard migrations between nodes
- **`goverse_clients_total`**: Number of connected clients per node

### Usage

Metrics are automatically tracked and exposed on the standard Prometheus endpoint. Configure your Prometheus server to scrape the GoVerse nodes:

```yaml
scrape_configs:
  - job_name: 'goverse'
    static_configs:
      - targets: ['localhost:47000', 'localhost:47001', 'localhost:47002']
```

See [PROMETHEUS_INTEGRATION.md](PROMETHEUS_INTEGRATION.md) for detailed metric descriptions and usage examples.

---

## Testing

You can test the chat client/server functionality using the automated test script:

```bash
# Run the test script (requires protoc and Go protobuf plugins)
python3 tests/samples/chat/test_chat.py
```

The test script will:
1. Start the inspector
2. Start the chat server
3. Build and run the chat client with automated test input
4. Verify all chat functionality (listing rooms, joining, sending messages)
5. Clean up all processes

This script is used in CI/CD but can also be run locally for testing.

---

## Additional Resources

- [GoVerse API Reference](GOVERSEAPI.md): Complete API documentation for the goverseapi package
- [HTTP Gate API](design/HTTP_GATE.md): REST/HTTP endpoints for client access
- [Object Access Control](design/OBJECT_ACCESS_CONTROL.md): Security rules and access patterns
- [Push Messaging](PUSH_MESSAGING.md): Real-time server-to-client message delivery
- [Configuration](CONFIGURATION.md): YAML config file format and options
- [Prometheus Integration](PROMETHEUS_INTEGRATION.md): Metrics for monitoring and observability
- [PostgreSQL Setup](POSTGRES_SETUP.md): Guide for setting up object persistence with PostgreSQL
- [Persistence Example](../examples/persistence/): Complete example of using PostgreSQL persistence
- [Inspector UI](../cmd/inspector/): Visualize cluster state and objects
- [Counter Sample](../samples/counter/): Simple counter service demonstrating basic operations
- [Tic Tac Toe Sample](../samples/tictactoe/): Web-based game with HTTP Gate API
- [Chat Sample](../samples/chat/): Distributed chat with real-time messaging and web client
- [Proto Reference](../proto/): Protocol buffer definitions
- [Testing Guide](TESTING.md): Comprehensive testing documentation

---

**GoVerse** makes building distributed, stateful Go systems simple, scalable, and observable through its client service architecture and virtual actor model.
