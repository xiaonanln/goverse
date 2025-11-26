# HTTP Gate Design

> **Status**: Partially Implemented  
> This document describes the HTTP gate support for Goverse, enabling HTTP clients to interact with objects.

---

## Goals

Enable HTTP clients to:
- **Call object methods** via HTTP REST API ✅ Implemented
- **Create and delete objects** via HTTP REST API ✅ Implemented
- **Receive pushed messages** from objects in real-time via SSE ❌ Not yet implemented

All HTTP functionality integrates with the existing gate architecture without duplicating logic.

---

## Architecture Overview

The HTTP gate runs alongside the existing gRPC gate, sharing the same core `Gate` and `Cluster` components:

```
HTTP Client ──> HTTP Handler ──> GateServer ──> Cluster ──> Node ──> Objects
                                          ↓
gRPC Client ──> gRPC Handler ─────────────┘
```

**Key principle**: HTTP handlers are thin protocol adapters that translate HTTP requests/responses to the same internal gate operations used by gRPC.

---

## Current Implementation

### Configuration

HTTP is enabled by setting `HTTPListenAddress` in `GateServerConfig`:

```go
type GateServerConfig struct {
    ListenAddress     string        // gRPC address (e.g., ":49000")
    AdvertiseAddress  string        // Address advertised to cluster
    HTTPListenAddress string        // HTTP address for REST API (e.g., ":8080")
    EtcdAddress       string        // etcd address for cluster state
    EtcdPrefix        string        // etcd key prefix (default: "/goverse")
    NumShards         int           // Number of shards (default: 8192)

    DefaultCallTimeout   time.Duration // Default CallObject timeout (default: 30s)
    DefaultDeleteTimeout time.Duration // Default DeleteObject timeout (default: 30s)
    DefaultCreateTimeout time.Duration // Default CreateObject timeout (default: 30s)
}
```

### HTTP Server Settings

The HTTP server is configured with sensible defaults:
- Read timeout: 30 seconds
- Write timeout: 30 seconds
- Read header timeout: 10 seconds
- Idle timeout: 120 seconds

---

## HTTP REST API

### Base URL

```
http://gate-host:port/api/v1
```

### Encoding

All requests use **protobuf `Any` bytes** encoded as base64 JSON:

```json
{
  "request": "<base64-encoded protobuf Any bytes>"
}
```

This maintains type safety and consistency with the gRPC interface.

### Endpoints

#### 1. Call Object Method

**Endpoint:** `POST /api/v1/objects/call/{type}/{id}/{method}`

**Request:**
```json
{
  "request": "<base64-encoded Any bytes>"
}
```

**Headers:**
- `Content-Type: application/json`
- `X-Client-ID: <client-id>` (optional, for push messaging context)

**Response (success):**
```json
{
  "response": "<base64-encoded Any bytes>"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/objects/call/Counter/my-counter/Increment \
  -H "Content-Type: application/json" \
  -d '{"request":"CiZ0eXBlLmdvb2dsZWFwaXMuY29tL2dvb2dsZS5wcm90b2J1Zi5JbnQzMlZhbHVlEgIIBQ=="}'
```

#### 2. Create Object

**Endpoint:** `POST /api/v1/objects/create/{type}/{id}`

**Request:** Empty body or optional creation arguments

**Response (success):**
```json
{
  "id": "object-id"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/objects/create/Counter/my-counter
```

#### 3. Delete Object

**Endpoint:** `POST /api/v1/objects/delete/{id}`

**Request:** Empty body

**Response (success):**
```json
{
  "success": true
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/objects/delete/my-counter
```

#### 4. Prometheus Metrics

**Endpoint:** `GET /metrics`

Returns Prometheus-formatted metrics for monitoring.

### Error Handling

All errors return appropriate HTTP status codes with JSON error details:

```json
{
  "error": "error message",
  "code": "ERROR_CODE"
}
```

**Error codes:**
| Code | HTTP Status | Description |
|------|-------------|-------------|
| `METHOD_NOT_ALLOWED` | 405 | Only POST is allowed |
| `INVALID_PATH` | 400 | URL path format is incorrect |
| `INVALID_PARAMETERS` | 400 | Required parameters are empty |
| `INVALID_BODY` | 400 | Failed to read request body |
| `INVALID_JSON` | 400 | Failed to parse JSON |
| `INVALID_BASE64` | 400 | Failed to decode base64 |
| `INVALID_PROTOBUF` | 400 | Failed to unmarshal protobuf |
| `OBJECT_NOT_FOUND` | 404 | Object does not exist |
| `CALL_FAILED` | 500 | Internal server error |
| `CREATE_FAILED` | 500 | Object creation failed |
| `DELETE_FAILED` | 500 | Object deletion failed |
| `MARSHAL_ERROR` | 500 | Failed to marshal response |

---

## Client Usage Example

### Encoding Protobuf Messages for HTTP

```go
package main

import (
    "encoding/base64"
    "fmt"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/anypb"
    "google.golang.org/protobuf/types/known/wrapperspb"
)

func encodeRequest(msg proto.Message) (string, error) {
    // Step 1: Wrap message in google.protobuf.Any
    anyReq, err := anypb.New(msg)
    if err != nil {
        return "", err
    }

    // Step 2: Marshal to bytes
    reqBytes, err := proto.Marshal(anyReq)
    if err != nil {
        return "", err
    }

    // Step 3: Base64 encode
    return base64.StdEncoding.EncodeToString(reqBytes), nil
}

func main() {
    // Create an Int32 value to increment counter by 5
    msg := &wrapperspb.Int32Value{Value: 5}
    encoded, _ := encodeRequest(msg)
    
    fmt.Printf(`curl -X POST http://localhost:8080/api/v1/objects/call/Counter/my-counter/Increment \
  -H "Content-Type: application/json" \
  -d '{"request":"%s"}'`, encoded)
}
```

### JavaScript Example

```javascript
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
```

---

## Implementation Details

### Source Files

- `gate/gateserver/http_handler.go` - HTTP handler implementation
- `gate/gateserver/http_handler_test.go` - Unit tests
- `examples/httpgate/main.go` - Example usage

### Key Functions

| Function | Description |
|----------|-------------|
| `setupHTTPRoutes()` | Configures HTTP routes |
| `handleCallObject()` | Handles `/api/v1/objects/call/{type}/{id}/{method}` |
| `handleCreateObject()` | Handles `/api/v1/objects/create/{type}/{id}` |
| `handleDeleteObject()` | Handles `/api/v1/objects/delete/{id}` |
| `startHTTPServer()` | Starts the HTTP server |
| `stopHTTPServer()` | Gracefully stops the HTTP server |

---

## Future Work: Push Messaging via SSE

> **Status**: Not yet implemented

HTTP push messaging will use **Server-Sent Events (SSE)** to deliver messages from objects to clients.

### Planned Design

**Endpoint:** `GET /api/v1/events/stream`

**Response:**
```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

event: register
data: {"clientId": "gate-addr/unique-id"}

event: message
data: {"type": "ChatMessage", "payload": {...}}

event: heartbeat
data: {}
```

**Why SSE:**
- Native browser support with `EventSource` API
- Simple protocol built on HTTP
- Automatic reconnection on connection loss
- Efficient one-way server→client streaming
- For bidirectional needs, clients should use gRPC gate

### Planned Client Usage

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

## Future Enhancements

- **Server-Sent Events (SSE)** - Real-time push messaging
- **Authentication middleware** - JWT, API keys
- **Rate limiting** - Per-client request throttling
- **CORS configuration** - For browser-based clients
- **OpenAPI/Swagger spec** - API documentation generation
- **GraphQL API** - Alternative to REST for complex queries

---

## Summary

The HTTP gate provides a REST API for Goverse object operations:

| Feature | Status |
|---------|--------|
| Call object methods | ✅ Implemented |
| Create objects | ✅ Implemented |
| Delete objects | ✅ Implemented |
| Prometheus metrics | ✅ Implemented |
| Push messaging (SSE) | ❌ Not yet implemented |

The implementation uses protobuf `Any` encoding for type safety and shares the same cluster infrastructure as the gRPC gate, ensuring consistency between HTTP and gRPC clients.
