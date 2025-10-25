# GoVerse

> ⚠️ **EARLY DEVELOPMENT STAGE** - This project is in very early development. APIs are unstable and may change significantly. Not recommended for production use.

[![Go Tests](https://github.com/xiaonanln/goverse/actions/workflows/test.yml/badge.svg)](https://github.com/xiaonanln/goverse/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/xiaonanln/goverse/branch/main/graph/badge.svg)](https://codecov.io/gh/xiaonanln/goverse)

**GoVerse** is a **distributed object runtime for Go**, implementing the **virtual actor (grain) model**.
It lets you build systems around **stateful entities with identity and methods**, while the runtime handles placement, routing, lifecycle, and fault-tolerance.

---

## ✨ Features

- **Distributed Objects (Grains):** Uniquely addressable, stateful entities with custom methods.
- **Virtual Actor Lifecycle:** Objects are activated on demand, deactivated when idle, and reactivated seamlessly.
- **Client Service:** Client connection management and method routing through server-side client objects.
- **Sharding & Rebalancing:** Fixed shard model with automatic remapping via etcd.
- **Fault-Tolerance:** Lease + epoch fencing prevent split-brain; safe recovery after node failures.
- **Call Semantics:** At-least-once delivery with idempotency hooks; optional at-most-once.
- **Concurrency Modes:** Sequential, concurrent, or read-only execution strategies.
- **gRPC Transport:** Efficient remote calls, client proxies, and bidirectional streaming.
- **Inspector UI:** Visualize nodes, objects, and their relationships in real time.
- **Sample Apps:** Includes a distributed chat system with multiple chat rooms.

---

## 📦 Project Structure

- `cmd/inspector/` – Inspector web server for cluster visualization.
- `server/` – Node server implementation.
- `node/` – Core node logic and object management.
- `object/` – Object base types and helpers.
- `client/` – Client service implementation and protocol definitions.
- `samples/chat/` – Sample distributed chat application:
  - `server/ChatRoom.go` – Chat room distributed object.
  - `server/ChatClient.go` – Server-side client object for orchestrating operations.
  - `server/ChatRoomMgr.go` – Manager object for chat room lifecycle.
  - `server/chat_server.go` – Chat server main entry point.
  - `client/client.go` – Interactive chat client application.
  - `proto/chat.proto` – Chat protocol definitions.
- `proto/` – GoVerse protocol definitions.
- `util/` – Logging and utility helpers.

---

## 🧩 Client Architecture & Connection Management

GoVerse features a **Client Service** system that enables seamless communication between clients and distributed objects:

### Core Components:

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

### Client Connection Flow:

1. **Registration**: Client connects and calls `Register()` streaming RPC
2. **ID Assignment**: Server creates a client object with unique ID
3. **Method Invocation**: Client calls methods through `Call()` RPC
4. **Object Orchestration**: Client object methods orchestrate calls to distributed objects
5. **Graceful Cleanup**: Client objects cleaned up on disconnect

### Usage Example:
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

## 🚀 Example: Distributed Chat

The chat system consists of multiple distributed objects working together:

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
    server := goverseapi.NewServer(config)
    
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

---

## 💬 Chat Message Retrieval

The chat system provides message management through distributed ChatRoom objects:

- **Polling-based Updates**: Clients can poll for recent messages using `GetRecentMessages()`
- **Timestamp Tracking**: Messages include microsecond timestamps for ordering and filtering
- **Message Persistence**: Chat history stored in distributed ChatRoom objects
- **Multi-room Support**: Each room is an independent distributed object with its own state
- **Client-side Orchestration**: ChatClient objects on the server coordinate operations

### Architecture Benefits:
- **Distributed State**: Each ChatRoom maintains its own message history
- **Client Abstraction**: ChatClient objects provide a clean interface for clients
- **Object Isolation**: Chat rooms are independent distributed objects
- **Simple Protocol**: Standard RPC calls using protobuf messages

---

## 🛠 Getting Started

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

    # Both clients register with the server and can chat
    ```

### Client Commands:
- `/list` - List available chat rooms
- `/join <room>` - Join a chat room
- `/messages` - View recent messages in current room
- `/help` - Show available commands
- `/quit` - Exit the client
- Type any message to send to current room

---

## 🧪 Testing

You can test the chat client/server functionality using the automated test script:

```bash
# Run the test script (requires protoc and Go protobuf plugins)
python3 script/test_chat.py
```

The test script will:
1. Start the inspector
2. Start the chat server
3. Build and run the chat client with automated test input
4. Verify all chat functionality (listing rooms, joining, sending messages)
5. Clean up all processes

This script is used in CI/CD but can also be run locally for testing.

---

## 📚 Documentation

- [Inspector UI](inspector/web/index.html): Visualize cluster state and objects.
- [Chat Sample](samples/chat/): Example distributed chat with multiple rooms.
- [Client Service](client/): Client connection management and RPC routing.
- [API Reference](proto/): Protocol buffer definitions.

---

## ⚠️ Project Status

**This project is in very early development.**

- APIs and interfaces are subject to breaking changes
- Features are actively being developed and refined
- Documentation may be incomplete or outdated
- Not suitable for production environments
- Experimental features may be added or removed

**Use at your own risk.** Contributions and feedback are welcome!

---

## 📝 License

MIT License

---

**GoVerse** makes building distributed, stateful Go systems simple, scalable, and observable through its client service architecture and virtual actor model.
