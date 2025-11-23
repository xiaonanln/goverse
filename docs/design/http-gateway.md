# HTTP Gateway Design

> **Status**: Design Document  
> This document describes the HTTP gateway support for Goverse, enabling HTTP clients to interact with objects and receive push notifications.

---

## Goals

Enable HTTP clients to:
- **Call object methods** via HTTP REST API
- **Create and delete objects** via HTTP REST API
- **Receive pushed messages** from objects in real-time via HTTP mechanisms

All HTTP functionality should integrate seamlessly with the existing gateway architecture without duplicating logic.

---

## Architecture Overview

The HTTP gateway runs alongside the existing gRPC gateway, sharing the same core `Gateway` and `Cluster` components:

```
HTTP Client ──> HTTP Handler ──> Gateway ──> Cluster ──> Node ──> Objects
                                     ↓
gRPC Client ──> gRPC Handler ────────┘
```

**Key principle**: HTTP handlers are thin protocol adapters that translate HTTP requests/responses to the same internal gateway operations used by gRPC.

---

## HTTP REST API

### Base Design

All object operations use a consistent URL structure and JSON encoding:

```
Base URL: http://gateway-host:port/api/v1
```

### Endpoints

#### 1. Call Object Method

**Request:**
```
POST /api/v1/objects/{type}/{id}/call/{method}
Content-Type: application/json

{
  "args": { ... }  // Method-specific arguments as JSON
}
```

**Response:**
```json
{
  "result": { ... }  // Method-specific result as JSON
}
```

**Example:**
```
POST /api/v1/objects/ChatRoom/room-123/call/SendMessage
Content-Type: application/json

{
  "args": {
    "userName": "alice",
    "message": "Hello world"
  }
}
```

#### 2. Create Object

**Request:**
```
POST /api/v1/objects/{type}/{id}
Content-Type: application/json

{
  "args": { ... }  // Optional creation arguments
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
DELETE /api/v1/objects/{id}
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

## Push Messaging Options

HTTP push messaging requires special handling since HTTP is request-response based. Three approaches are supported:

### Option 1: Server-Sent Events (SSE) - Recommended

**Pros:**
- Native browser support
- Simple protocol built on HTTP
- Automatic reconnection
- Efficient one-way server→client streaming

**Cons:**
- One-way only (server→client)
- Limited browser connection pool (6 connections per domain)

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
   data: {"clientId": "gateway-addr/unique-id"}

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

### Option 2: WebSocket

**Pros:**
- Full bidirectional communication
- Efficient binary/text messaging
- Widely supported

**Cons:**
- More complex protocol
- Requires WebSocket infrastructure
- Some proxy/firewall issues

**Design:**

1. **Client connection:**
   ```
   WebSocket: ws://gateway-host:port/api/v1/ws
   ```

2. **Protocol:**
   - First message: `{"type": "register"}` → `{"type": "register_response", "clientId": "..."}`
   - Pushed messages: `{"type": "message", "payload": {...}}`
   - Keep-alive: `{"type": "ping"}` / `{"type": "pong"}`

3. **Advantages over SSE:**
   - Can send client→server messages over same connection
   - Better for bidirectional chat applications

### Option 3: Long Polling (Fallback)

**Pros:**
- Works everywhere (even through restrictive proxies)
- Simple to implement

**Cons:**
- Less efficient (frequent reconnections)
- Higher latency
- More server load

**Design:**

1. **Client registration:**
   ```
   POST /api/v1/events/register
   ```
   Response: `{"clientId": "gateway-addr/unique-id"}`

2. **Poll for messages:**
   ```
   GET /api/v1/events/poll?clientId={clientId}&timeout=30
   ```
   - Server holds connection open until message available or timeout
   - Returns: `{"messages": [...]}`
   - Client immediately reconnects after receiving response

---

## Implementation Strategy

### Phase 1: HTTP REST API (Core Operations)

Add HTTP handlers to `gate/gateserver/`:

```go
type HTTPGatewayServer struct {
    gatewayServer *GatewayServer
    httpServer    *http.Server
}

func (s *HTTPGatewayServer) handleCallObject(w http.ResponseWriter, r *http.Request) {
    // Extract type, id, method from URL
    // Parse JSON request body
    // Call s.gatewayServer.cluster.CallObject()
    // Return JSON response
}

func (s *HTTPGatewayServer) handleCreateObject(w http.ResponseWriter, r *http.Request) {
    // Extract type, id from URL
    // Call s.gatewayServer.cluster.CreateObject()
    // Return JSON response
}

func (s *HTTPGatewayServer) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
    // Extract id from URL
    // Call s.gatewayServer.cluster.DeleteObject()
    // Return JSON response
}
```

### Phase 2: Server-Sent Events (Push Messaging)

Implement SSE streaming handler:

```go
func (s *HTTPGatewayServer) handleEventStream(w http.ResponseWriter, r *http.Request) {
    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    
    // Register client with gateway
    clientProxy := s.gatewayServer.gate.Register(r.Context())
    defer s.gatewayServer.gate.Unregister(clientProxy.GetID())
    
    // Send registration event
    fmt.Fprintf(w, "event: register\ndata: {\"clientId\":\"%s\"}\n\n", clientProxy.GetID())
    w.(http.Flusher).Flush()
    
    // Stream messages
    for {
        select {
        case <-r.Context().Done():
            return
        case msg := <-clientProxy.MessageChan():
            // Convert protobuf message to JSON
            // Write as SSE event
            fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonMsg)
            w.(http.Flusher).Flush()
        }
    }
}
```

### Phase 3: WebSocket (Optional)

Add WebSocket support using `gorilla/websocket` or similar library.

---

## Key Design Choices

### 1. JSON vs Protobuf

**Choice**: Use JSON for HTTP API, convert to/from protobuf internally.

**Rationale**:
- JSON is HTTP-native and developer-friendly
- Protobuf is used internally for efficient node-to-node communication
- Gateway performs transparent conversion

### 2. URL Structure

**Choice**: RESTful URLs with type and ID in path: `/objects/{type}/{id}/call/{method}`

**Rationale**:
- Clear, intuitive structure
- Type-safety through explicit type parameter
- Consistent with object model (`Type-ID` format)

### 3. Push Mechanism Priority

**Choice**: Prioritize SSE > WebSocket > Long Polling

**Rationale**:
- SSE is simplest and sufficient for most use cases (server→client push)
- WebSocket adds complexity but enables bidirectional communication
- Long polling is fallback for restrictive environments

### 4. Authentication & Authorization

**Approach**: HTTP middleware at gateway level

**Design considerations**:
- HTTP gateway can add auth middleware (JWT, API keys, etc.)
- Auth context passed to objects via request context or arguments
- Objects should not trust client-provided identity fields
- Future: Per-object/per-method access control policies

### 5. HTTP Server Integration

**Choice**: Run HTTP server alongside gRPC server in same process.

**Rationale**:
- Share same gateway and cluster instances
- Simpler deployment (one binary)
- Option to run on different ports for separation

---

## Configuration Example

```go
type GatewayServerConfig struct {
    // gRPC
    GRPCListenAddress    string // e.g., ":49000"
    
    // HTTP
    HTTPListenAddress    string // e.g., ":8080"
    HTTPEnabled          bool   // Enable HTTP gateway
    
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
// Join room
await fetch('/api/v1/objects/ChatRoom/room-1/call/Join', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    args: { userName: 'alice', clientId: clientId }
  })
});

// Send message
await fetch('/api/v1/objects/ChatRoom/room-1/call/SendMessage', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    args: { userName: 'alice', message: 'Hello!' }
  })
});
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

1. **Phase 1**: Deploy HTTP gateway with REST API support
2. **Phase 2**: Add SSE streaming for push notifications
3. **Phase 3**: (Optional) Add WebSocket for bidirectional use cases
4. **Phase 4**: Build example web UI using HTTP + SSE

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

- **REST API** provides simple, familiar HTTP/JSON interface for object operations
- **Server-Sent Events** enable efficient real-time push notifications
- **Thin protocol layer** maintains separation between transport and business logic
- **Shared infrastructure** ensures consistency between HTTP and gRPC clients
- **Incremental implementation** allows phased rollout with backward compatibility

The HTTP gateway preserves Goverse's core architectural principles while making the platform accessible to web browsers and HTTP-native tooling.
