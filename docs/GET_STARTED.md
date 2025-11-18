# Getting Started with GoVerse

**GoVerse** is a **distributed object runtime for Go**, implementing the **virtual actor (grain) model**.
It lets you build systems around **stateful entities with identity and methods**, while the runtime handles placement, routing, lifecycle, and fault-tolerance.

---

## Table of Contents

- [Installation](#installation)
- [Core Concepts](#core-concepts)
- [Project Structure](#project-structure)
- [Client Architecture](#client-architecture)
- [Quick Start Tutorial](#quick-start-tutorial)
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
- Fixed 8192-shard model provides consistent hashing
- Automatic shard-to-node mapping managed via etcd
- Objects placed on nodes based on their shard assignment
- Automatic rebalancing when nodes join or leave

### Fault-Tolerance
- Lease + epoch fencing prevent split-brain scenarios
- Safe recovery after node failures
- Automatic object migration during rebalancing

---

## Project Structure

- `cmd/inspector/` – Inspector web server for cluster visualization
- `server/` – Node server implementation with context-based shutdown
- `node/` – Core node logic and object management
- `object/` – Object base types and helpers (BaseObject)
- `client/` – Client service implementation and protocol definitions (BaseClient)
- `cluster/` – Cluster singleton management, leadership election, and automatic shard mapping
- `cluster/sharding/` – Shard-to-node mapping with 8192 fixed shards
- `cluster/etcdmanager/` – etcd connection management and node registry
- `util/postgres/` – PostgreSQL persistence utilities and JSONB storage
- `samples/chat/` – Sample distributed chat application with multiple chat rooms
- `examples/persistence/` – Example of using PostgreSQL persistence
- `examples/minquorum/` – Example demonstrating cluster quorum configuration
- `proto/` – GoVerse protocol definitions
- `util/` – Logging and utility helpers

---

## Client Architecture

GoVerse features a **Client Service** system that enables seamless communication between clients and distributed objects.

### Core Components

**1. Client Registration System**
- Clients register with the server via `Register()` streaming RPC on the `ClientService`
- Each client receives a unique ID and bidirectional message channel
- Persistent connections maintained for communication

**2. Connection Lifecycle Management**
```go
// Client-side registration
stream, err := client.Register(ctx, &client_pb.Empty{})

// Receive registration response with client ID
regResp, _ := stream.Recv()
clientID := regResp.(*client_pb.RegisterResponse).ClientId
```

**3. Server-Side Client Objects**
- Each connected client is represented by a `BaseClient` object on the server
- Clients can have custom methods that orchestrate calls to other objects
- Example: `ChatClient` manages chat room operations and message routing

**4. Method Call Interface**
- Clients call methods via the `Call()` RPC with client ID, method name, and request
- Server routes calls to the appropriate client object's methods
- Client objects can then call other distributed objects using `CallObject()`

**5. Push Messaging**
- Server can push messages to clients in real-time via the registration stream
- Use `PushMessageToClient()` to send messages from distributed objects to specific clients
- Enables real-time notifications, chat messages, and event delivery

### Client Connection Flow

1. **Registration**: Client connects and calls `Register()` streaming RPC
2. **ID Assignment**: Server creates a client object with unique ID
3. **Bidirectional Communication**: Client can call methods AND receive pushed messages
4. **Method Invocation**: Client calls methods through `Call()` RPC
5. **Object Orchestration**: Client object methods orchestrate calls to distributed objects
6. **Real-time Updates**: Server pushes messages to clients as events occur
7. **Graceful Cleanup**: Client objects cleaned up on disconnect

### Usage Example

```go
// Connect to server
conn, _ := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
client := client_pb.NewClientServiceClient(conn)

// Register and get client ID
stream, _ := client.Register(ctx, &client_pb.Empty{})
regResp, _ := stream.Recv()
clientID := regResp.(*client_pb.RegisterResponse).ClientId

// Call methods on the client object
anyReq, _ := anypb.New(&chat_pb.Client_JoinChatRoomRequest{
    RoomName: "General",
    UserName: "alice",
})
resp, _ := client.Call(ctx, &client_pb.CallRequest{
    ClientId: clientID,
    Method:   "Join",
    Request:  anyReq,
})
```

This architecture provides a clean separation between client connections and distributed object operations.

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
        ListenAddress:       "localhost:47000",
        AdvertiseAddress:    "localhost:47000",
        ClientListenAddress: "localhost:48000",
    }
    
    server, err := goverseapi.NewServer(config)
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

## Chat Application Example

The chat system demonstrates how distributed objects work together in a real application.

### ChatRoom Object (Distributed Object)

```go
type ChatRoom struct {
    goverseapi.BaseObject
    users    map[string]bool
    messages []*chat_pb.ChatMessage
    mu       sync.Mutex
}

func (room *ChatRoom) Join(ctx context.Context, req *chat_pb.ChatRoom_JoinRequest) (*chat_pb.ChatRoom_JoinResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    room.users[req.GetUserName()] = true
    
    return &chat_pb.ChatRoom_JoinResponse{
        RoomName: room.Name(),
        RecentMessages: []*chat_pb.ChatMessage{ /* recent messages */ },
    }, nil
}

func (room *ChatRoom) SendMessage(ctx context.Context, req *chat_pb.ChatRoom_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    room.messages = append(room.messages, &chat_pb.ChatMessage{
        UserName:  req.GetUserName(),
        Message:   req.GetMessage(),
        Timestamp: time.Now().UnixMicro(),
    })
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### ChatClient Object (Server-side Client Proxy)

```go
type ChatClient struct {
    goverseapi.BaseClient
    currentChatRoom string
}

func (cc *ChatClient) Join(ctx context.Context, req *chat_pb.Client_JoinChatRoomRequest) (*chat_pb.Client_JoinChatRoomResponse, error) {
    // Call the ChatRoom object
    resp, err := goverseapi.CallObject(ctx, "ChatRoom-"+req.RoomName, "Join", 
        &chat_pb.ChatRoom_JoinRequest{UserName: req.UserName})
    
    cc.currentChatRoom = req.RoomName
    return resp.(*chat_pb.Client_JoinChatRoomResponse), err
}

func (cc *ChatClient) SendMessage(ctx context.Context, req *chat_pb.Client_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
    // Forward to current chat room
    _, err := goverseapi.CallObject(ctx, "ChatRoom-"+cc.currentChatRoom, "SendMessage",
        &chat_pb.ChatRoom_SendChatMessageRequest{
            UserName: req.GetUserName(),
            Message:  req.GetMessage(),
        })
    return nil, err
}
```

### Server Setup

```go
func main() {
    config := &goverseapi.ServerConfig{
        ListenAddress:       "localhost:47000",
        AdvertiseAddress:    "localhost:47000",
        ClientListenAddress: "localhost:48000",
    }
    server, err := goverseapi.NewServer(config)
    if err != nil {
        panic(err)
    }
    
    // Register types and create initial objects
    goverseapi.RegisterClientType((*ChatClient)(nil))
    goverseapi.RegisterObjectType((*ChatRoom)(nil))
    goverseapi.CreateObject(ctx, "ChatRoom", "ChatRoom-General", nil)
    
    server.Run()
}
```

### Client Usage

```go
// Connect and register
client := client_pb.NewClientServiceClient(conn)
stream, _ := client.Register(ctx, &client_pb.Empty{})
regResp, _ := stream.Recv()
clientID := regResp.(*client_pb.RegisterResponse).ClientId

// Join a chat room
anyReq, _ := anypb.New(&chat_pb.Client_JoinChatRoomRequest{
    RoomName: "General",
    UserName: "alice",
})
client.Call(ctx, &client_pb.CallRequest{
    ClientId: clientID,
    Method:   "Join",
    Request:  anyReq,
})

// Send a message
anyReq, _ = anypb.New(&chat_pb.Client_SendChatMessageRequest{
    UserName: "alice",
    RoomName: "General",
    Message:  "Hello everyone!",
})
client.Call(ctx, &client_pb.CallRequest{
    ClientId: clientID,
    Method:   "SendMessage",
    Request:  anyReq,
})
```

### Chat Messaging Architecture

The chat system provides real-time message delivery through distributed ChatRoom objects:

- **Push-based Delivery**: Messages are pushed to clients in real-time via bidirectional gRPC streams
- **Polling Support**: Clients can also poll for recent messages using `GetRecentMessages()`
- **Timestamp Tracking**: Messages include microsecond timestamps for ordering and filtering
- **Message Persistence**: Chat history stored in distributed ChatRoom objects
- **Multi-room Support**: Each room is an independent distributed object with its own state
- **Client-side Orchestration**: ChatClient objects on the server coordinate operations

See [PUSH_MESSAGING.md](PUSH_MESSAGING.md) for implementation details.

### Running the Chat Application

1. **Start the inspector UI (optional):**
   ```bash
   go run ./cmd/inspector/
   # Open http://localhost:8080 to visualize the cluster
   ```

2. **Start the chat server:**
   ```bash
   go run ./samples/chat/server/
   # Starts server on port 47000 (node-to-node) and 48000 (client connections)
   ```

3. **Start multiple chat clients:**
   ```bash
   # Terminal 1
   go run ./samples/chat/client/ -server=localhost:48000 -user=alice

   # Terminal 2
   go run ./samples/chat/client/ -server=localhost:48000 -user=bob
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
    ListenAddress:       "localhost:7001",
    AdvertiseAddress:    "localhost:7001",
    ClientListenAddress: "localhost:8001",
    EtcdAddress:         "localhost:2379",
    EtcdPrefix:          "/goverse",
    MinQuorum:            3, // Require at least 3 nodes before cluster is ready
}

server, err := goverseapi.NewServer(config)
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
    ListenAddress:           "localhost:7001",
    AdvertiseAddress:        "localhost:7001",
    ClientListenAddress:     "localhost:8001",
    EtcdAddress:             "localhost:2379",
    EtcdPrefix:              "/goverse",
    MinQuorum:               1,
    NodeStabilityDuration:   5 * time.Second, // Wait 5s for stability (default: 10s)
}

server, err := goverseapi.NewServer(config)
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

**1. Set up PostgreSQL** (see [postgres-setup.md](postgres-setup.md)):

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
- **Setup Guide**: See `docs/postgres-setup.md` for detailed PostgreSQL setup instructions
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
python3 tests/integration/test_chat.py
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

- [Push Messaging](PUSH_MESSAGING.md): Real-time server-to-client message delivery
- [Prometheus Integration](PROMETHEUS_INTEGRATION.md): Metrics for monitoring and observability
- [PostgreSQL Setup](postgres-setup.md): Guide for setting up object persistence with PostgreSQL
- [Persistence Example](../examples/persistence/): Complete example of using PostgreSQL persistence
- [Inspector UI](../inspector/web/index.html): Visualize cluster state and objects
- [Chat Sample](../samples/chat/): Example distributed chat with multiple rooms and real-time messaging
- [Client Service](../client/): Client connection management and RPC routing
- [API Reference](../proto/): Protocol buffer definitions
- [Testing Guide](TESTING.md): Comprehensive testing documentation

---

**GoVerse** makes building distributed, stateful Go systems simple, scalable, and observable through its client service architecture and virtual actor model.
