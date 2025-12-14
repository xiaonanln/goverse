# GoVerse API Reference

This document describes the `goverseapi` package, which provides the high-level API for building applications with GoVerse.

## Overview

The `goverseapi` package is the primary interface for GoVerse applications. It provides:

- Server creation and configuration
- Object registration and lifecycle management
- Object method invocation (CallObject)
- Push messaging to clients
- Context utilities for identifying callers

## Quick Start

```go
package main

import (
    "context"
    "sync"

    "github.com/xiaonanln/goverse/goverseapi"
    "google.golang.org/protobuf/types/known/wrapperspb"
)

// Define your object
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int
}

func (c *Counter) Add(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value += int(req.GetValue())
    return wrapperspb.Int32(int32(c.value)), nil
}

func main() {
    // Create server from command-line flags
    server := goverseapi.NewServer()
    
    // Register object types
    goverseapi.RegisterObjectType((*Counter)(nil))
    
    // Wait for cluster and create objects
    go func() {
        <-goverseapi.ClusterReady()
        goverseapi.CreateObject(context.Background(), "Counter", "my-counter")
    }()
    
    // Run the server
    server.Run(context.Background())
}
```

---

## Server Creation

### NewServer

```go
func NewServer() *Server
```

Creates a server using command-line flags. This is the recommended way to create a server.

**CLI Mode:**
```bash
go run . --listen :47000 --advertise localhost:47000 --etcd localhost:2379
```

**Config File Mode:**
```bash
go run . --config config.yaml --node-id node1
```

**Available Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:48000` | Node listen address |
| `--advertise` | `localhost:48000` | Node advertise address |
| `--http-listen` | | HTTP listen address for metrics |
| `--etcd` | `localhost:2379` | Etcd address |
| `--etcd-prefix` | `/goverse` | Etcd key prefix |
| `--config` | | Path to YAML config file |
| `--node-id` | | Node ID (required with `--config`) |

### NewServerWithConfig

```go
func NewServerWithConfig(config *ServerConfig) (*Server, error)
```

Creates a server with explicit configuration. Use this when you need programmatic control over configuration.

```go
config := &goverseapi.ServerConfig{
    ListenAddress:    "localhost:47000",
    AdvertiseAddress: "localhost:47000",
    EtcdAddress:      "localhost:2379",
    EtcdPrefix:       "/myapp",
}
server, err := goverseapi.NewServerWithConfig(config)
```

---

## Object Registration

### RegisterObjectType

```go
func RegisterObjectType(obj Object)
```

Registers an object type with the cluster. Must be called before `server.Run()`.

```go
goverseapi.RegisterObjectType((*Counter)(nil))
goverseapi.RegisterObjectType((*ChatRoom)(nil))
goverseapi.RegisterObjectType((*Player)(nil))
```

---

## Object Lifecycle

### Creating Object IDs

GoVerse provides three methods for creating object IDs, each with different routing behaviors:

#### CreateObjectID

```go
func CreateObjectID() string
```

Creates a normal object ID using hash-based sharding. This is the default choice for most use cases.

```go
objID := goverseapi.CreateObjectID()
goverseapi.CreateObject(ctx, "Counter", objID)
```

**Use when:** You want automatic load balancing and fault tolerance across nodes.

#### CreateObjectIDOnShard

```go
func CreateObjectIDOnShard(shardID int) string
```

Creates an object ID that will be placed on a specific shard. Valid shard IDs range from 0 to 8191 (production) or 0 to 63 (tests).

```go
objID := goverseapi.CreateObjectIDOnShard(5)
goverseapi.CreateObject(ctx, "Counter", objID)
```

**Use when:** You need objects to be co-located on the same node (e.g., objects that frequently interact).

**Format:** `shard#<shardID>/<uniqueID>` (e.g., `shard#5/AAZFAQkHBWFxJkjOKHgz`)

#### CreateObjectIDOnNode

```go
func CreateObjectIDOnNode(nodeAddress string) string
```

Creates an object ID that will be placed on a specific node. The node address should be in the format "host:port".

```go
objID := goverseapi.CreateObjectIDOnNode("localhost:7001")
goverseapi.CreateObject(ctx, "Counter", objID)
```

**Use when:** You need precise control over object placement (e.g., node-local resources).

**Format:** `<nodeAddress>/<uniqueID>` (e.g., `localhost:7001/AAZFAQkHBYPMjtdAA2_4`)

### CreateObject

```go
func CreateObject(ctx context.Context, objType, objID string) (string, error)
```

Creates a new object instance. The object will be placed on the appropriate node based on the object ID format.

```go
// Using a normal ID
objID := goverseapi.CreateObjectID()
id, err := goverseapi.CreateObject(ctx, "Counter", objID)

// Using a fixed-shard ID
objID := goverseapi.CreateObjectIDOnShard(5)
id, err := goverseapi.CreateObject(ctx, "Counter", objID)

// Using a fixed-node ID
objID := goverseapi.CreateObjectIDOnNode("localhost:7001")
id, err := goverseapi.CreateObject(ctx, "Counter", objID)
```

**Parameters:**
- `objType`: The registered object type name (e.g., `"Counter"`)
- `objID`: Unique identifier for the object (use CreateObjectID* methods)

**Returns:**
- The object ID (same as input `objID`)
- Error if creation fails

### DeleteObject

```go
func DeleteObject(ctx context.Context, objID string) error
```

Deletes an object from the cluster.

```go
err := goverseapi.DeleteObject(ctx, "Counter-my-counter")
```

---

## Object Method Invocation

### CallObject

```go
func CallObject(ctx context.Context, objType, id string, method string, request proto.Message) (proto.Message, error)
```

Invokes a method on an object. The call is routed to the node hosting the object.

```go
req := wrapperspb.Int32(5)
resp, err := goverseapi.CallObject(ctx, "Counter", "my-counter", "Add", req)
if err != nil {
    log.Printf("Call failed: %v", err)
    return
}
result := resp.(*wrapperspb.Int32Value)
log.Printf("New value: %d", result.GetValue())
```

**Parameters:**
- `objType`: The object type name
- `id`: The object ID
- `method`: Method name to invoke
- `request`: Protobuf message as method argument

**Returns:**
- Response as `proto.Message`
- Error if call fails

---

## Push Messaging

### PushMessageToClient

```go
func PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error
```

Sends a message to a connected client via the gate. Use this for real-time notifications and updates.

```go
notification := &myproto.ChatMessage{
    Sender:  "system",
    Content: "Welcome!",
}
err := goverseapi.PushMessageToClient(ctx, clientID, notification)
```

**Client ID Format:** `gateAddress/uniqueId` (e.g., `"localhost:7001/abc123"`)

See [Push Messaging](PUSH_MESSAGING.md) for detailed usage.

---

## Cluster Status

### ClusterReady

```go
func ClusterReady() <-chan bool
```

Returns a channel that closes when the cluster is ready. The cluster is ready when:
- Nodes are connected
- Shard mapping is complete

```go
// Block until ready
<-goverseapi.ClusterReady()

// Or with timeout
select {
case <-goverseapi.ClusterReady():
    log.Println("Cluster is ready")
case <-time.After(30 * time.Second):
    log.Fatal("Cluster not ready in time")
}
```

---

## Context Utilities

### CallerClientID

```go
func CallerClientID(ctx context.Context) string
```

Gets the client ID from the call context. Returns empty string if the call didn't come from a client.

```go
func (obj *MyObject) MyMethod(ctx context.Context, req *MyRequest) (*MyResponse, error) {
    clientID := goverseapi.CallerClientID(ctx)
    if clientID != "" {
        // Call came from a client via gate
        log.Printf("Request from client: %s", clientID)
    } else {
        // Call came from another object
    }
    // ...
}
```

### CallerIsClient

```go
func CallerIsClient(ctx context.Context) bool
```

Checks if the call originated from a client via the gate.

```go
func (obj *MyObject) InternalMethod(ctx context.Context, req *Req) (*Resp, error) {
    if goverseapi.CallerIsClient(ctx) {
        return nil, errors.New("this method is internal only")
    }
    // Process internal call...
}
```

---

## Defining Objects

Objects must embed `goverseapi.BaseObject` and implement methods with the signature:

```go
func (obj *MyObject) MethodName(ctx context.Context, req *RequestType) (*ResponseType, error)
```

### Basic Object

```go
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int
}

func (c *Counter) Get(ctx context.Context, req *emptypb.Empty) (*wrapperspb.Int32Value, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    return wrapperspb.Int32(int32(c.value)), nil
}

func (c *Counter) Add(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value += int(req.GetValue())
    return wrapperspb.Int32(int32(c.value)), nil
}
```

### Persistent Object

Override `ToData` and `FromData` to enable persistence:

```go
type PersistentCounter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int
}

func (c *PersistentCounter) ToData() (proto.Message, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    return &myproto.CounterData{Value: int32(c.value)}, nil
}

func (c *PersistentCounter) FromData(data proto.Message) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    if data != nil {
        c.value = int(data.(*myproto.CounterData).Value)
    }
    return nil
}
```

See [Persistence](PERSISTENCE_IMPLEMENTATION.md) for full details.

### Object Lifecycle Hooks

Objects have three lifecycle hooks:

```go
// OnInit is called first to initialize the object
func (obj *MyObject) OnInit(self Object, id string) {
    // Initialize state
}

// OnCreated is called after object is created and initialized
func (obj *MyObject) OnCreated() {
    obj.Logger.Info("Object created")
}

// OnDestroy is called when the object is being destroyed
func (obj *MyObject) OnDestroy() {
    // Perform cleanup (cancels lifetime context automatically)
    obj.Logger.Info("Object destroyed")
}
```

### Object Lifetime Context

Each object has a lifetime context that is automatically cancelled when the object is destroyed:

```go
func (obj *MyObject) OnCreated() {
    // Start a background goroutine that respects object lifetime
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-obj.Context().Done():
                // Object is being destroyed, clean up and exit
                obj.Logger.Info("Object destroyed, stopping background task")
                return
            case <-ticker.C:
                // Do periodic work
                obj.doPeriodicWork()
            }
        }
    }()
}
```

The context is accessible via `obj.Context()` and is cancelled automatically by `OnDestroy()` when:
- `DeleteObject` is called for this object
- The node is stopped with `Stop()`

**Use this to:**
- Stop background goroutines gracefully
- Cancel long-running operations
- Clean up resources when the object is destroyed

**Note:** You can override `OnDestroy()` to add custom cleanup logic, but make sure to call the base implementation to cancel the context:

```go
func (obj *MyObject) OnDestroy() {
    // Custom cleanup first
    obj.customCleanup()
    
    // Call base implementation to cancel context
    obj.BaseObject.OnDestroy()
}
```

---

## Type Aliases

The `goverseapi` package provides convenient type aliases:

| Alias | Original |
|-------|----------|
| `ServerConfig` | `server.ServerConfig` |
| `Server` | `server.Server` |
| `Node` | `node.Node` |
| `Object` | `object.Object` |
| `BaseObject` | `object.BaseObject` |
| `BaseClient` | `client.BaseClient` |
| `Cluster` | `cluster.Cluster` |

---

## Best Practices

### 1. Always Use Mutex for State

```go
type MyObject struct {
    goverseapi.BaseObject
    mu    sync.Mutex  // Protect all mutable state
    data  map[string]string
}

func (obj *MyObject) SetData(ctx context.Context, req *SetDataReq) (*emptypb.Empty, error) {
    obj.mu.Lock()
    defer obj.mu.Unlock()
    obj.data[req.Key] = req.Value
    return &emptypb.Empty{}, nil
}
```

### 2. Wait for Cluster Ready

```go
go func() {
    <-goverseapi.ClusterReady()
    // Now safe to create objects and make calls
    goverseapi.CreateObject(ctx, "MyObject", "id")
}()
```

### 3. Use Protobuf Messages

All method arguments and return values should be protobuf messages for HTTP/gRPC compatibility.

### 4. Handle Errors

```go
resp, err := goverseapi.CallObject(ctx, "Counter", "id", "Add", req)
if err != nil {
    // Handle routing errors, method errors, etc.
    return err
}
```

### 5. Use Context for Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
resp, err := goverseapi.CallObject(ctx, "Counter", "id", "Get", &emptypb.Empty{})
```

---

## Examples

See the [examples directory](../examples/) for complete working examples:

- [Object ID Creation](../examples/objectid_example/) - Demonstrates all three object ID creation methods
- [Fixed Node](../examples/fixednode/) - Using fixed node addresses
- [Persistence](../examples/persistence/) - Database-backed objects
- [HTTP Gate](../examples/httpgate/) - REST API integration

## See Also

- [Getting Started](GET_STARTED.md) - Complete tutorial
- [Push Messaging](PUSH_MESSAGING.md) - Real-time notifications
- [Object Access Control](design/OBJECT_ACCESS_CONTROL.md) - Security rules
- [HTTP Gate](design/HTTP_GATE.md) - REST API
- [Persistence](PERSISTENCE_IMPLEMENTATION.md) - Database storage
