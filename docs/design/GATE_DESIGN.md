# Gate Refactor Design

> **Status**: Draft / WIP  
> This document describes the planned gate refactor for GoVerse, focusing on how client traffic enters the system and how it interacts with GoVerse objects (grains). Implementation details may change as the `gate` branch evolves.

---

## 1. Goals

The gate refactor aims to:

- Introduce a **clear separation** between:
  - **Nodes (GoVerse servers)** that host objects and participate in the cluster.
  - **Gates** that handle client connections and protocols (gRPC, HTTP).
- Make **GoVerse objects** (grains) the primary abstraction:
  - No special client-specific objects in the cluster.
  - Per-user or per-session state can be modeled as grains when needed.
- Provide a **GoVerse service API** that both gates and nodes use to:
  - Call objects.
  - Create objects.
- Keep the gate as thin as possible:
  - Protocol handling, authentication, simple orchestration.
  - Core business logic lives in objects on nodes.

---

## 2. High-Level Architecture

### 2.1 Components

#### GoVerse server (node)

A GoVerse server (node) is a long-running process that:

- Joins the cluster and registers itself in etcd.
- Hosts GoVerse objects (e.g. `ChatRoom-{roomID}`, `Counter-{id}`, etc.).
- Receives object calls from gates or other nodes and executes them locally.
- Uses node-to-node RPC for:
  - Object→object calls across nodes.
  - Activation / creation of remote objects.

#### GoVerse service

The **GoVerse service** is the internal API that represents “the cluster’s ability to operate on objects”:

```go
type GoverseService interface {
    CallObject(ctx context.Context, objectID, method string, arg any) (any, error)
    CreateObject(ctx context.Context, typeName, objectID string, arg any) (any, error)
}
```

- Server-side implementation lives inside the node.
- Used by:
  - Nodes themselves (for internal object→object calls).
  - Gates via a client implementation (for external calls into the cluster).

All user-facing helpers like `goverseapi.CallObject` and `goverseapi.CreateObject` ultimately drive this service.

#### Gate

The **gate** is a separate process (planned `goverse-gate` binary) responsible for client-facing concerns:

- gRPC `ClientService`.
- HTTP/JSON endpoints (future REST-style API).
- Optional long-lived streaming endpoints.
- Authentication and basic authorization / routing rules.

The gate:

- Connects to nodes using a **GoVerse service client**.
- Uses etcd-backed cluster state (via `ConsensusManager` and sharding) to route `objectID` to the correct node.
- Does **not** host GoVerse objects; it only forwards calls and maintains connection-local state if needed.
- Does **not** implement domain logic (e.g. room membership, nicknames); that logic lives in grains.

#### Client proxies (gate-local, optional)

Client proxies are gate-local, per-connection controllers:

- Created per incoming client connection (e.g., per gRPC stream).
- Hold only minimal connection-local state needed for transport:
  - Underlying gRPC stream / socket or equivalent.
  - A generated connection ID or client proxy ID.
  - Lightweight bookkeeping to correlate responses to the client.
- Use the GoVerse service client to call objects on nodes.

Client proxies **do not** own application-level or domain-specific state such as room membership, nicknames, or user profiles. All such state should be modeled as GoVerse objects on nodes. The proxy’s job is purely to receive client messages, translate them into generic object calls via the GoVerse service, and send responses back over the connection.

For simple flows (including the chat sample), client proxies can be very thin or even skipped in favor of direct HTTP/gRPC → grain calls.

---

## 3. Call Flows

### 3.1 External client → gate → node

#### gRPC example (generic CallObject)

1. Client opens a gRPC stream to the **gate**.
2. Gate creates a client proxy instance for that connection.
3. When the client wants to invoke an object method, it sends a generic call request, for example:

   ```protobuf
   message CallObjectRequest {
       string object_id = 1; // e.g. "ChatRoom-123"
       string method    = 2; // e.g. "Join"
       bytes  payload   = 3; // serialized domain-specific args (e.g. JoinArgs)
   }
   ```

   The client chooses `object_id` and `method` and encodes the domain arguments into `payload`. The gate does not interpret domain fields (such as “room id”); it treats them as opaque.

4. Gate receives `CallObjectRequest` and calls:

   ```go
   svc.CallObject(ctx, req.ObjectId, req.Method, req.Payload)
   ```

5. GoVerse service client:
   - Computes the shard for `req.ObjectId`.
   - Looks up the node that owns that shard.
   - Sends a `CallObject` request to that node.

6. Node:
   - Activates or finds the object identified by `req.ObjectId` locally (e.g. `ChatRoom-123`).
   - Invokes the method named by `req.Method` (e.g. `Join`) with the decoded payload.
   - Returns success or error.

7. Gate sends the result back to the client over gRPC.

#### HTTP example (chat)

For HTTP, a similar pattern can be exposed (shape TBD), where the client or HTTP handler supplies:

- `object_id`
- `method`
- JSON payload

and the gate simply forwards this as a `CallObject` to the GoVerse service client.

### 3.2 Object → object (inside the cluster)

From user code inside a GoVerse object:

```go
resp, err := goverseapi.CallObject(ctx, "OtherObject-123", "SomeMethod", arg)
```

Execution:

1. The node hosting the caller object uses:
   - `ConsensusManager` + shard mapping to determine which node owns `"OtherObject-123"`.
2. If the owner is **local**:
   - Call the method on the local object instance.
3. If the owner is **remote**:
   - Use node→node RPC to forward the call to the owning node’s GoVerse service implementation.
4. Remote node activates/invokes the object and returns the result.

Gates are not involved in internal object→object calls.

### 3.3 Gate → Node Registration Flow

To enable objects on nodes to push messages to clients connected to gates, gates must register with nodes:

1. **Gate startup**:
   - Gate starts and connects to etcd to discover nodes in the cluster.
   - Gate discovers all nodes via the consensus/shard mapping.

2. **Gate registers with each node**:
   - For each discovered node, the gate calls the node's `RegisterGate` RPC with its advertise address:
     ```protobuf
     message RegisterGateRequest {
       string gate_addr = 1;  // e.g., "localhost:49000"
     }
     ```
   - `RegisterGate` returns a **streaming response** (`stream GateMessage`).

3. **Node acknowledges registration**:
   - Node sends `RegisterGateResponse` as the first message on the stream.
   - Node creates a buffered channel for this gate and tracks it in memory.

4. **Node streams messages to gate**:
   - When an object on the node calls `PushMessageToClient(clientID, message)`, the node:
     - Parses the `clientID` to extract the gate address (format: `gateAddress/uniqueId`).
     - Looks up the gate's message channel.
     - Sends a `ClientMessageEnvelope` wrapped in a `GateMessage` to the gate's channel:
       ```protobuf
       message ClientMessageEnvelope {
         string client_id = 1;  // Full client ID
         google.protobuf.Any message = 2;  // The message to push
       }
       
       message GateMessage {
         oneof message {
           RegisterGateResponse register_gate_response = 1;
           ClientMessageEnvelope client_message = 2;
         }
       }
       ```
   - The `RegisterGate` stream handler forwards the envelope to the gate.

5. **Gate receives and routes messages**:
   - Gate receives `GateMessage` on the stream.
   - Gate extracts the `client_id` from `ClientMessageEnvelope`.
   - Gate looks up the corresponding `ClientProxy` by `client_id`.
   - Gate forwards the message to the client's message channel.
   - Client receives the message via its `Register` stream.

6. **Stream lifecycle**:
   - The `RegisterGate` stream stays open as long as the gate is connected.
   - If the stream closes (due to network issues, gate shutdown, etc.):
     - Node detects the closure and unregisters the gate connection.
     - Node cleans up the gate's message channel.
   - Gate can re-register with the node when the connection is re-established.

---

## 4. Chat Sample Refactor

The chat sample will be refactored to follow this gate architecture with minimal complexity.

### 4.1 Target shape for chat

For the chat sample:

- **Objects on nodes**:
  - `ChatRoom-{roomID}`: primary entrypoint for chat actions.
- **Gate**:
  - gRPC `ClientService` with a client proxy per connection (thin).
  - (Later) HTTP endpoints that map to generic `CallObject` requests.
- **No `ChatUser`/session grains** for this sample; the client calls `ChatRoom` directly via `CallObject`.

### 4.2 ChatRoom grain

Example API (simplified):

```go
type ChatRoom struct {
    goverseapi.BaseObject
    mu       sync.Mutex
    members  map[string]bool   // nick/user IDs
    messages []ChatMessage
}

type JoinArgs struct {
    UserID string
}

func (r *ChatRoom) Join(ctx context.Context, args *JoinArgs) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.members == nil {
        r.members = make(map[string]bool)
    }
    r.members[args.UserID] = true
    return nil
}

type SendMessageArgs struct {
    UserID  string
    Content string
}

func (r *ChatRoom) SendMessage(ctx context.Context, args *SendMessageArgs) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    // append to messages and fan out to subscribers (implementation-specific)
    return nil
}
```

The client encodes `JoinArgs` / `SendMessageArgs` into the `payload` field of `CallObjectRequest` and uses `object_id = "ChatRoom-{roomID}"`, `method = "Join"` / `"SendMessage"`.

### 4.3 Client proxy (gate-local)

In the gate process, a client proxy can be as thin as:

```go
type ClientProxy struct {
    id  string
    svc GoverseService // client-side implementation
}

func (p *ClientProxy) HandleCall(ctx context.Context, req *CallObjectRequest) (*CallObjectResponse, error) {
    // No domain logic here: just forward
    result, err := p.svc.CallObject(ctx, req.ObjectId, req.Method, req.Payload)
    if err != nil {
        return nil, err
    }
    // Wrap result back into CallObjectResponse (encoding TBD)
    return encodeResponse(result), nil
}
```

Notes:

- `ClientProxy` is gate-local only.
- It forwards `object_id`, `method`, and `payload` without interpreting them.

---

## 5. Push Messaging from Objects to Clients

GoVerse supports push messaging, allowing objects on nodes to send messages directly to clients connected to gates, even when the client is not actively making a request.

### 5.1 Use Cases

Push messaging is useful for:

- **Chat notifications**: When a user sends a message to a chat room, all members receive the message immediately.
- **Real-time updates**: Objects can notify clients of state changes (e.g., game events, stock price updates).
- **Asynchronous responses**: Long-running operations can notify clients when complete.

### 5.2 Client Registration and Identification

1. **Client connects to gate**:
   - Client calls the gate's `Register` RPC.
   - Gate generates a unique `client_id` with format: `gateAddress/uniqueId` (e.g., `"localhost:49000/AAZEDvtPr4JHP6WtybiD"`).
   - Gate creates a `ClientProxy` to manage the client's connection.
   - Gate returns the `client_id` to the client in a `RegisterResponse`.

2. **Client keeps the stream open**:
   - The `Register` RPC returns a stream (`stream google.protobuf.Any`).
   - Client receives pushed messages on this stream.
   - The stream remains open for the lifetime of the client connection.

### 5.3 Pushing Messages from Objects

From within a GoVerse object method, you can push a message to a client:

```go
func (obj *MyObject) SomeMethod(ctx context.Context, args *SomeArgs) error {
    // ... do some work ...
    
    // Push a notification to a specific client
    notification := &MyNotification{
        Text: "Something happened!",
        Timestamp: time.Now().Unix(),
    }
    
    err := goverseapi.PushMessageToClient(ctx, args.ClientID, notification)
    if err != nil {
        // Client not found or gate not connected
        return err
    }
    
    return nil
}
```

**Requirements:**

- The message must be a `proto.Message` (protobuf message).
- The `clientID` must be in the format `gateAddress/uniqueId`.
- The object calling `PushMessageToClient` must pass the client's ID, which it receives as part of the method arguments from the client.

### 5.4 Push Message Flow

When `goverseapi.PushMessageToClient(ctx, clientID, message)` is called:

1. **API call**:
   - `goverseapi.PushMessageToClient` delegates to `cluster.PushMessageToClient`.

2. **Cluster routing**:
   - Cluster parses `clientID` to extract the gate address.
   - Cluster looks up the gate's registered channel (from `RegisterGate` connection).
   - Cluster wraps the message in a `ClientMessageEnvelope`:
     ```protobuf
     message ClientMessageEnvelope {
       string client_id = 1;  // Full client ID
       google.protobuf.Any message = 2;  // The pushed message
     }
     ```

3. **Send to gate**:
   - Cluster sends the envelope to the gate's channel (buffered, non-blocking).
   - If the gate is not connected to this node, the call returns an error.

4. **Gate receives and routes**:
   - Gate's `RegisterGate` stream handler receives the `ClientMessageEnvelope`.
   - Gate extracts `client_id` and looks up the corresponding `ClientProxy`.
   - Gate forwards the message to the client proxy's message channel.

5. **Client receives**:
   - Client proxy sends the message to the client via the `Register` stream.
   - Client receives the message as a `google.protobuf.Any` and unmarshals it.

### 5.5 Error Handling

`PushMessageToClient` returns an error if:

- The `clientID` format is invalid (missing gate address or unique ID).
- The gate specified in `clientID` is not connected to the node.
- The client is not registered with the gate (client disconnected).
- The gate's message channel is full (backpressure).

Objects should handle these errors gracefully, as clients may disconnect at any time.

### 5.6 Design Considerations

- **Client ID format**: The `gateAddress/uniqueId` format allows the node to route messages to the correct gate without a centralized client registry.
- **Per-gate channels**: Each gate has a dedicated message channel on each node, enabling concurrent message delivery to multiple gates.
- **Buffered channels**: Channels are buffered (default 1024 messages) to prevent blocking object methods during transient network delays.
- **No guaranteed delivery**: If a client disconnects or a gate fails, messages are dropped. Applications requiring guaranteed delivery should implement acknowledgment and retry logic at the application level.

---

## 6. Access Rules and Safety (Future)

While the chat sample does not emphasize security, the architecture allows more control later:

- Gate can enforce which objects and methods are callable from external clients:
  - For example, only allow calls to certain prefixes (`ChatRoom-*`, `User-*`, etc.) and whitelisted methods.
- For real applications:
  - Gate authenticates the caller and attaches identity (e.g. user ID, roles) to the `context.Context` or request arguments.
  - Gates do not trust identity fields provided directly by clients.

For now, the chat sample can remain simple:

- Let clients provide `object_id`, `method`, and arguments.
- Let the gate forward directly to GoVerse objects via `CallObject`.

---

## 7. Summary

This gate refactor design:

- Separates **nodes** (object hosts) from **gates** (client-facing).
- Introduces a shared **GoverseService** abstraction used by both.
- Keeps **GoVerse objects** as the core programming model.
- Uses a generic `CallObject` request from clients, so the gate stays free of domain logic and simply routes calls to objects based on `object_id`.
- Supports **push messaging** from objects to clients via `PushMessageToClient`, enabling real-time notifications and updates.
- Implements **gate registration** via the `RegisterGate` streaming RPC, allowing nodes to push messages to clients through gates.
- Makes the chat sample simpler by letting clients call `ChatRoom` objects directly, without introducing extra user/session grains for now.

As the `gate` branch evolves, this document should be kept in sync with the actual behavior of `goverse-server` and the future `goverse-gate` binary.