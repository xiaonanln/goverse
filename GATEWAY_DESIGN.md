# Gateway Refactor Design

> **Status**: Draft / WIP  
> This document describes the planned gateway refactor for GoVerse, focusing on how client traffic enters the system and how it interacts with GoVerse objects (grains). Implementation details may change as the `gateway` branch evolves.

---

## 1. Goals

The gateway refactor aims to:

- Introduce a **clear separation** between:
  - **Nodes (GoVerse servers)** that host objects and participate in the cluster.
  - **Gateways** that handle client connections and protocols (gRPC, HTTP).
- Make **GoVerse objects** (grains) the primary abstraction:
  - No special client-specific objects in the cluster.
  - Per-user or per-session state can be modeled as grains when needed.
- Provide a **GoVerse service API** that both gateways and nodes use to:
  - Call objects.
  - Create objects.
- Keep the gateway as thin as possible:
  - Protocol handling, authentication, simple orchestration.
  - Core business logic lives in objects on nodes.

---

## 2. High-Level Architecture

### 2.1 Components

#### GoVerse server (node)

A GoVerse server (node) is a long-running process that:

- Joins the cluster and registers itself in etcd.
- Hosts GoVerse objects (e.g. `ChatRoom-{roomID}`, `Counter-{id}`, etc.).
- Receives object calls from gateways or other nodes and executes them locally.
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
  - Gateways via a client implementation (for external calls into the cluster).

All user-facing helpers like `goverseapi.CallObject` and `goverseapi.CreateObject` ultimately drive this service.

#### Gateway

The **gateway** is a separate process (planned `goverse-gateway` binary) responsible for client-facing concerns:

- gRPC `ClientService`.
- HTTP/JSON endpoints (future REST-style API).
- Optional long-lived streaming endpoints.
- Authentication and basic authorization / routing rules.

The gateway:

- Connects to nodes using a **GoVerse service client**.
- Uses etcd-backed cluster state (via `ConsensusManager` and sharding) to route `objectID` to the correct node.
- Does **not** host GoVerse objects; it only forwards calls and maintains connection-local state if needed.
- Does **not** implement domain logic (e.g. room membership, nicknames); that logic lives in grains.

#### Client proxies (gateway-local, optional)

Client proxies are gateway-local, per-connection controllers:

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

### 3.1 External client → gateway → node

#### gRPC example (generic CallObject)

1. Client opens a gRPC stream to the **gateway**.
2. Gateway creates a client proxy instance for that connection.
3. When the client wants to invoke an object method, it sends a generic call request, for example:

   ```protobuf
   message CallObjectRequest {
       string object_id = 1; // e.g. "ChatRoom-123"
       string method    = 2; // e.g. "Join"
       bytes  payload   = 3; // serialized domain-specific args (e.g. JoinArgs)
   }
   ```

   The client chooses `object_id` and `method` and encodes the domain arguments into `payload`. The gateway does not interpret domain fields (such as “room id”); it treats them as opaque.

4. Gateway receives `CallObjectRequest` and calls:

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

7. Gateway sends the result back to the client over gRPC.

#### HTTP example (chat)

For HTTP, a similar pattern can be exposed (shape TBD), where the client or HTTP handler supplies:

- `object_id`
- `method`
- JSON payload

and the gateway simply forwards this as a `CallObject` to the GoVerse service client.

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

Gateways are not involved in internal object→object calls.

---

## 4. Chat Sample Refactor

The chat sample will be refactored to follow this gateway architecture with minimal complexity.

### 4.1 Target shape for chat

For the chat sample:

- **Objects on nodes**:
  - `ChatRoom-{roomID}`: primary entrypoint for chat actions.
- **Gateway**:
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

### 4.3 Client proxy (gateway-local)

In the gateway process, a client proxy can be as thin as:

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

- `ClientProxy` is gateway-local only.
- It forwards `object_id`, `method`, and `payload` without interpreting them.

---

## 6. Access Rules and Safety (Future)

While the chat sample does not emphasize security, the architecture allows more control later:

- Gateway can enforce which objects and methods are callable from external clients:
  - For example, only allow calls to certain prefixes (`ChatRoom-*`, `User-*`, etc.) and whitelisted methods.
- For real applications:
  - Gateway authenticates the caller and attaches identity (e.g. user ID, roles) to the `context.Context` or request arguments.
  - Gateways do not trust identity fields provided directly by clients.

For now, the chat sample can remain simple:

- Let clients provide `object_id`, `method`, and arguments.
- Let the gateway forward directly to GoVerse objects via `CallObject`.

---

## 7. Summary

This gateway refactor design:

- Separates **nodes** (object hosts) from **gateways** (client-facing).
- Introduces a shared **GoverseService** abstraction used by both.
- Keeps **GoVerse objects** as the core programming model.
- Uses a generic `CallObject` request from clients, so the gateway stays free of domain logic and simply routes calls to objects based on `object_id`.
- Makes the chat sample simpler by letting clients call `ChatRoom` objects directly, without introducing extra user/session grains for now.

As the `gateway` branch evolves, this document should be kept in sync with the actual behavior of `goverse-server` and the future `goverse-gateway` binary.