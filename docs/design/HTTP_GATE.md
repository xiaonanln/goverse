# HTTP Gate Design

> **Status**: Design Document  
> This document describes the HTTP gate support for Goverse, enabling HTTP clients to interact with objects and receive push notifications.

---

## Goals

Enable HTTP clients to:
- **Call object methods** via HTTP REST API
- **Create and delete objects** via HTTP REST API
- **Receive pushed messages** from objects in real-time via HTTP mechanisms

All HTTP functionality should integrate seamlessly with the existing gate architecture without duplicating logic.

---

## Architecture Overview

The HTTP gate runs alongside the existing gRPC gate, sharing the same core `Gate` and `Cluster` components:

```
HTTP Client ──> HTTP Handler ──> Gate ──> Cluster ──> Node ──> Objects
                                     ↓
gRPC Client ──> gRPC Handler ────────┘
```

**Key principle**: HTTP handlers are thin protocol adapters that translate HTTP requests/responses to the same internal gate operations used by gRPC.

---

## HTTP REST API

### Base Design

All object operations use a consistent URL structure with action prefixes. Requests use protobuf `Any` message encoding:

```
Base URL: http://gate-host:port/api/v1
```

**Encoding**: All requests must encode protobuf messages as `google.protobuf.Any`, marshal to bytes, and send as POST data in the format:
```json
{
  "request": "<base64-encoded Any bytes>"
}
```

### Endpoints

#### 1. Call Object Method

**Request:**
```
POST /api/v1/objects/call/{type}/{id}/{method}
Content-Type: application/json

{
  "request": "<base64-encoded Any bytes>"
}
```

**Response:**
```json
{
  "response": "<base64-encoded Any bytes>"
}
```

**Example:**
```
POST /api/v1/objects/call/ChatRoom/room-123/SendMessage
Content-Type: application/json

// Client must:
// 1. Create SendMessageArgs protobuf message
// 2. Wrap it in google.protobuf.Any
// 3. Marshal to bytes and base64 encode
// 4. Send as:
{
  "request": "ChtDaGF0Um9vbS5TZW5kTWVzc2FnZUFyZ3MS..."
}
```

#### 2. Create Object

**Request:**
```
POST /api/v1/objects/create/{type}/{id}
Content-Type: application/json

{
  "request": "<base64-encoded Any bytes>"  // Optional creation arguments
}
```

**Response:**
```json
{
  "id": "object-id"
}
```

#### 3. Delete Object

**Request:**
```
POST /api/v1/objects/delete/{id}
Content-Type: application/json
```

**Response:**
```json
{
  "success": true
}
```

### Error Handling

All errors return appropriate HTTP status codes with JSON error details:

```json
{
  "error": "error message",
  "code": "ERROR_CODE"
}
```

Common status codes:
- `200 OK`: Success
- `400 Bad Request`: Invalid request format
- `404 Not Found`: Object not found
- `500 Internal Server Error`: Server-side error

---

## Push Messaging

HTTP push messaging requires special handling since HTTP is request-response based. Goverse uses **Server-Sent Events (SSE)** for HTTP push messaging.

### Server-Sent Events (SSE)

**Why SSE:**
- Native browser support with `EventSource` API
- Simple protocol built on HTTP
- Automatic reconnection on connection loss
- Efficient one-way server→client streaming
- Sufficient for most push notification use cases

**Design:**

1. **Client registration:**
   ```
   GET /api/v1/events/stream
   Accept: text/event-stream
   ```

2. **Server response:**
   ```
   HTTP/1.1 200 OK
   Content-Type: text/event-stream
   Cache-Control: no-cache
   Connection: keep-alive

   event: register
   data: {"clientId": "gate-addr/unique-id"}

   event: message
   data: {"type": "ChatMessage", "payload": {...}}

   event: message
   data: {"type": "Notification", "payload": {...}}
   ```

3. **Message format:**
   - `event: register` - Initial registration response with client ID
   - `event: message` - Pushed messages from objects
   - `event: heartbeat` - Keep-alive signals (every 30s)

4. **Client usage (JavaScript):**
   ```javascript
   const eventSource = new EventSource('/api/v1/events/stream');
   
   eventSource.addEventListener('register', (e) => {
     const { clientId } = JSON.parse(e.data);
     // Use clientId in subsequent API calls
   });
   
   eventSource.addEventListener('message', (e) => {
     const msg = JSON.parse(e.data);
     // Handle pushed message
   });
   ```

---

## Implementation Strategy

### Phase 1: HTTP REST API (Core Operations)

Add HTTP handlers to `gate/gateserver/`:

```go
type HTTPGateServer struct {
    gateServer *GateServer
    httpServer    *http.Server
}

func (s *HTTPGateServer) handleCallObject(w http.ResponseWriter, r *http.Request) {
    // Extract type, id, method from URL path
    // Parse request body: {"request": "<base64-encoded Any bytes>"}
    // Decode base64 and unmarshal to google.protobuf.Any
    // Call s.gateServer.cluster.CallObjectAnyRequest()
    // Marshal response Any to base64 and return
}

func (s *HTTPGateServer) handleCreateObject(w http.ResponseWriter, r *http.Request) {
    // Extract type, id from URL path
    // Parse optional request body with Any bytes
    // Call s.gateServer.cluster.CreateObject()
    // Return object ID
}

func (s *HTTPGateServer) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
    // Extract id from URL path
    // Call s.gateServer.cluster.DeleteObject()
    // Return success response
}
```

### Phase 2: Server-Sent Events (Push Messaging)

Implement SSE streaming handler:

```go
func (s *HTTPGateServer) handleEventStream(w http.ResponseWriter, r *http.Request) {
    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    // Register client with gate
    clientProxy := s.gateServer.gate.Register(r.Context())
    defer s.gateServer.gate.Unregister(clientProxy.GetID())
    
    // Send registration event
    fmt.Fprintf(w, "event: register\ndata: {\"clientId\":\"%s\"}\n\n", clientProxy.GetID())
    w.(http.Flusher).Flush()
    
    // Stream messages
    for {
        select {
        case <-r.Context().Done():
            return
        case msg := <-clientProxy.MessageChan():
            // Convert protobuf message to JSON for SSE transport
            // Write as SSE event
            fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonMsg)
            w.(http.Flusher).Flush()
        }
    }
}
```

---

## Key Design Choices

### 1. Protobuf Encoding

**Choice**: Use protobuf `Any` bytes for HTTP API requests, not JSON.

**Rationale**:
- Goverse is highly protobuf-centric
- Maintains type safety and consistency with gRPC interface
- Clients must encode proto messages into `google.protobuf.Any`, marshal to bytes, and base64-encode for HTTP transport
- Gate can forward Any bytes directly to cluster without re-marshaling
- No lossy JSON conversion or schema ambiguity

### 2. URL Structure

**Choice**: Action-based URLs with operation prefix: `/objects/{action}/{type}/{id}/{method}`

**Rationale**:
- Explicit action naming (call, create, delete) for clarity
- All operations use POST for consistency and to avoid HTTP method limitations
- Type and ID clearly separated in path
- Method name at end for call operations

### 3. Push Mechanism

**Choice**: Use Server-Sent Events (SSE) exclusively for HTTP push messaging.

**Rationale**:
- SSE is sufficient for server→client push notifications
- Native browser support with simple API
- HTTP-based, works through most proxies
- Automatic reconnection handling
- For bidirectional needs, clients should use gRPC gate

### 4. Authentication & Authorization

**Approach**: HTTP middleware at gate level

**Design considerations**:
- HTTP gate can add auth middleware (JWT, API keys, etc.)
- Auth context passed to objects via request context or arguments
- Objects should not trust client-provided identity fields
- Future: Per-object/per-method access control policies

### 5. HTTP Server Integration

**Choice**: Run HTTP server alongside gRPC server in same process.

**Rationale**:
- Share same gate and cluster instances
- Simpler deployment (one binary)
- Option to run on different ports for separation

---

## Configuration Example

```go
type GateServerConfig struct {
    // gRPC
    GRPCListenAddress    string // e.g., ":49000"
    
    // HTTP
    HTTPListenAddress    string // e.g., ":8080"
    HTTPEnabled          bool   // Enable HTTP gate
    
    // Common
    AdvertiseAddress string
    EtcdAddress      string
    EtcdPrefix       string
}
```

---

## Testing Strategy

### Unit Tests
- HTTP handler request/response parsing
- JSON <-> Protobuf conversion
- Error handling and status codes

### Integration Tests
- End-to-end HTTP CallObject/CreateObject/DeleteObject
- SSE stream lifecycle (connect, receive, disconnect)
- HTTP + gRPC clients interacting with same objects
- Push message delivery to both HTTP and gRPC clients

### Manual Testing
- Browser-based HTTP client (fetch API + EventSource)
- curl commands for REST API
- Multi-client chat scenario (HTTP + gRPC clients)

---

## Example Usage

### Chat via HTTP REST API

```javascript
// Helper function to encode protobuf message for HTTP
async function callObject(type, id, method, protoMessage) {
  // 1. Wrap proto message in google.protobuf.Any
  const anyMessage = new google.protobuf.Any();
  anyMessage.pack(protoMessage);
  
  // 2. Marshal to bytes and base64 encode
  const anyBytes = anyMessage.serializeBinary();
  const base64Request = btoa(String.fromCharCode(...anyBytes));
  
  // 3. Send HTTP request
  const response = await fetch(`/api/v1/objects/call/${type}/${id}/${method}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ request: base64Request })
  });
  
  return response.json();
}

// Join room
const joinArgs = new chat.JoinArgs();
joinArgs.setUserName('alice');
joinArgs.setClientId(clientId);
await callObject('ChatRoom', 'room-1', 'Join', joinArgs);

// Send message
const sendArgs = new chat.SendMessageArgs();
sendArgs.setUserName('alice');
sendArgs.setMessage('Hello!');
await callObject('ChatRoom', 'room-1', 'SendMessage', sendArgs);
```

### Chat with SSE Push

```javascript
// Connect to event stream
const eventSource = new EventSource('/api/v1/events/stream');
let clientId;

eventSource.addEventListener('register', (e) => {
  const data = JSON.parse(e.data);
  clientId = data.clientId;
  
  // Now join room with clientId
  joinRoom(clientId);
});

eventSource.addEventListener('message', (e) => {
  const msg = JSON.parse(e.data);
  if (msg.type === 'ChatMessage') {
    displayMessage(msg.payload);
  }
});
```

---

## Migration Path

For existing gRPC clients, no changes required. HTTP support is additive:

1. **Phase 1**: Deploy HTTP gate with REST API support (call, create, delete)
2. **Phase 2**: Add SSE streaming for push notifications
3. **Phase 3**: Build example web UI using HTTP + SSE

HTTP and gRPC clients can coexist and interact seamlessly through the same object layer.

---

## Future Enhancements

- **GraphQL API**: Alternative to REST for complex queries
- **WebRTC**: For peer-to-peer media streaming scenarios
- **HTTP/2 Server Push**: Alternative to SSE (less browser support)
- **Rate limiting**: Per-client request throttling
- **CORS configuration**: For browser-based clients
- **API documentation**: OpenAPI/Swagger spec generation
- **SDK generation**: Auto-generated client libraries for popular languages

---

## Summary

This design enables HTTP clients to fully participate in the Goverse ecosystem:

- **HTTP REST API** with action-based URLs for object operations (call, create, delete)
- **Protobuf encoding** maintains type safety and consistency with gRPC
- **Server-Sent Events** enable efficient real-time push notifications
- **Thin protocol layer** maintains separation between transport and business logic
- **Shared infrastructure** ensures consistency between HTTP and gRPC clients
- **Incremental implementation** allows phased rollout with backward compatibility

The HTTP gate preserves Goverse's core architectural principles while making the platform accessible to web browsers and HTTP-native tooling.
