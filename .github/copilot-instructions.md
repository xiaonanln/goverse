# GitHub Copilot Instructions for Goverse

## Project Overview

Goverse is a **distributed object runtime for Go** implementing the **virtual actor (grain) model**. It enables building systems around stateful entities with identity and methods, while the runtime handles placement, routing, lifecycle, and fault-tolerance.

### Core Concepts

- **Distributed Objects (Grains)**: Uniquely addressable, stateful entities with custom methods
- **Virtual Actor Lifecycle**: Objects are activated on demand, deactivated when idle, and reactivated seamlessly
- **Client Service**: Client connection management and method routing through server-side client objects
- **Sharding & Rebalancing**: Fixed shard model (8192 shards) with automatic mapping management via etcd
- **Automatic Shard Management**: Leader node automatically manages shard mapping when nodes join/leave
- **Fault-Tolerance**: Lease + epoch fencing prevent split-brain; safe recovery after node failures
- **Concurrency Modes**: Sequential, concurrent, or read-only execution strategies

## Architecture

### Key Components

- **server/** - Node server implementation with context-based shutdown
- **node/** - Core node logic and object management
- **object/** - Object base types and helpers (BaseObject)
- **client/** - Client service implementation and protocol definitions (BaseClient)
- **cluster/** - Cluster singleton management, leadership election, and automatic shard mapping
- **cluster/sharding/** - Shard-to-node mapping with 8192 fixed shards (see cluster/sharding/README.md)
- **cluster/etcdmanager/** - etcd connection management and node registry
- **goverseapi/** - API wrapper functions for the framework
- **proto/** - Core Goverse protocol definitions
- **util/logger/** - Logging utilities
- **util/testutil/** - Test helpers including EtcdTestMutex for test isolation
- **util/uniqueid/** - Unique ID generation utilities
- **samples/chat/** - Distributed chat application example
- **inspector/** - Web UI for cluster visualization
- **cmd/inspector/** - Inspector web server

### Base Types

- All distributed objects should extend `goverseapi.BaseObject`
- All client objects should extend `goverseapi.BaseClient`
- Use `goverseapi.CallObject()` to call methods on distributed objects
- Register types with `goverseapi.RegisterObjectType()` and `goverseapi.RegisterClientType()`

### Node Package Locking Strategy

The `node` package uses a sophisticated multi-level locking strategy to ensure thread safety while avoiding deadlocks:

#### Lock Hierarchy (Must Be Acquired in This Order)

1. **stopMu (RWMutex)** - Protects node lifecycle (Start/Stop)
2. **keyLock (per-object key-based locks)** - Serializes operations on individual objects
3. **objectsMu (RWMutex)** - Protects the objects map

**CRITICAL**: Always acquire locks in this order to prevent deadlocks. Never acquire a higher-level lock while holding a lower-level lock.

#### Per-Object Key-Based Locking

The node uses a `KeyLock` manager that provides per-object exclusive and read-write locks:

```go
// Exclusive lock for an object (used in CreateObject, DeleteObject)
unlockKey := node.keyLock.Lock(objectID)
defer unlockKey()

// Read lock for an object (used in CallObject for read-only methods)
unlockKey := node.keyLock.RLock(objectID)
defer unlockKey()
```

**Key Benefits:**
- Operations on different objects can proceed in parallel
- Operations on the same object are serialized to prevent race conditions
- Automatic cleanup: locks are garbage collected when reference count reaches zero
- No global lock contention for unrelated objects

#### Typical Lock Patterns

**CreateObject Pattern:**
```go
func (node *Node) CreateObject(...) {
    // 1. Acquire stopMu read lock (allows concurrent operations while preventing Stop)
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    // Check if stopped
    if node.stopped.Load() {
        return fmt.Errorf("node is stopped")
    }
    
    // 2. Acquire per-key exclusive lock (serialize operations on this object)
    unlockKey := node.keyLock.Lock(id)
    defer unlockKey()
    
    // 3. Acquire objectsMu for map operations (double-checked locking pattern)
    node.objectsMu.RLock()
    _, exists := node.objects[id]
    node.objectsMu.RUnlock()
    
    if exists {
        return fmt.Errorf("object with id %s already exists", id)
    }
    
    // Create object...
    
    // 4. Add to map with write lock
    node.objectsMu.Lock()
    node.objects[id] = obj
    node.objectsMu.Unlock()
}
```

**DeleteObject Pattern (Idempotent):**
```go
func (node *Node) DeleteObject(...) {
    // 1. Acquire stopMu read lock
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    // Check if stopped - IDEMPOTENT: succeed if node stopped (objects already cleared)
    if node.stopped.Load() {
        node.logger.Infof("Node is stopped, object %s already cleared", id)
        return nil  // Success - desired state achieved
    }
    
    // 2. Acquire per-key exclusive lock
    unlockKey := node.keyLock.Lock(id)
    defer unlockKey()
    
    // 3. Check if object exists - IDEMPOTENT: succeed if object doesn't exist
    node.objectsMu.RLock()
    obj, exists := node.objects[id]
    node.objectsMu.RUnlock()
    
    if !exists {
        node.logger.Infof("Object %s does not exist, nothing to delete", id)
        return nil  // Success - desired state achieved
    }
    
    // Delete from persistence and memory...
}
```

**CallObject Pattern:**
```go
func (node *Node) CallObject(...) {
    // 1. Acquire stopMu read lock
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    if node.stopped.Load() {
        return nil, fmt.Errorf("node is stopped")
    }
    
    // 2. Acquire per-key lock (exclusive or read based on method)
    unlockKey := node.keyLock.Lock(id)  // or RLock for read-only methods
    defer unlockKey()
    
    // 3. Get object from map (may auto-create)
    node.objectsMu.RLock()
    obj, exists := node.objects[id]
    node.objectsMu.RUnlock()
    
    // Call method on object...
}
```

#### Idempotency Principles

The node package follows idempotent operation semantics where appropriate:

**DeleteObject is Fully Idempotent:**
- ✅ **Object exists** → Delete it and return success
- ✅ **Object doesn't exist** → Return success (desired state already achieved)
- ✅ **Node is stopped** → Return success (all objects already cleared)
- ❌ **Only fails for true operational errors** (e.g., persistence deletion failure)

This follows distributed systems best practices: operations should succeed if the desired postcondition is met, regardless of how that state was achieved. This matches HTTP DELETE idempotency semantics.

**Other Operations After Stop:**
- `CreateObject` - Returns error (cannot create on stopped node)
- `CallObject` - Returns error (cannot call on stopped node)
- `SaveAllObjects` - Returns error (cannot save on stopped node)

DeleteObject is special because when the node is stopped, all objects are already cleared, so the deletion request is already satisfied.

#### Double-Checked Locking Pattern

Many operations use double-checked locking to minimize lock contention:

```go
// First check with read lock (fast path)
node.objectsMu.RLock()
obj, exists := node.objects[id]
node.objectsMu.RUnlock()

if !exists {
    // Object doesn't exist, create it
    // ... creation logic ...
    
    // Add to map with write lock
    node.objectsMu.Lock()
    // Check again - another goroutine may have created it
    if _, exists := node.objects[id]; exists {
        node.objectsMu.Unlock()
        return existingObject
    }
    node.objects[id] = newObject
    node.objectsMu.Unlock()
}
```

#### Preventing Deadlocks

**Golden Rules:**
1. **Always acquire locks in the defined hierarchy order**
2. **Never call a method that acquires a higher-level lock while holding a lower-level lock**
3. **Use per-key locks to isolate operations on different objects**
4. **Keep critical sections short - release locks as soon as possible**
5. **Use defer for lock release to ensure cleanup on panic**

**Common Deadlock Scenarios to Avoid:**
- ❌ Holding objectsMu while calling a method that acquires stopMu
- ❌ Holding a per-key lock while acquiring another per-key lock
- ❌ Holding any lock while calling user code (object methods)

**Safe Patterns:**
- ✅ Release map locks before calling object methods
- ✅ Use double-checked locking to minimize write lock duration
- ✅ Acquire stopMu.RLock at the start, per-key locks in the middle, map locks briefly for map operations

## Development Workflow

### Prerequisites

**IMPORTANT**: Always install prerequisites and compile protocol buffers before building or testing.

```bash
# Install protoc compiler (Linux/WSL)
sudo apt-get update
sudo apt-get install -y protobuf-compiler

# Install protoc compiler (macOS)
brew install protobuf

# Install protoc compiler (Windows with Chocolatey)
choco install protoc

# Install Go protobuf plugins (required for all platforms)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Ensure the Go bin directory is in your PATH
# Add to ~/.bashrc, ~/.zshrc, or PowerShell profile:
# export PATH="$PATH:$(go env GOPATH)/bin"  # Linux/macOS
# $env:PATH += ";$(go env GOPATH)\bin"      # PowerShell
```

### Building the Project

**CRITICAL**: Always compile protobuf files FIRST before any build, test, or run operations.

```bash
# Step 1: Compile protobuf files (REQUIRED FIRST STEP)
./script/compile-proto.sh

# On Windows, use:
# .\script\win\compile-proto.cmd

# Step 2: Tidy dependencies
go mod tidy

# Step 3: Build the project
go build ./...

# Step 4: Run go vet to check for common mistakes
go vet ./...
```

### Running Tests

**Always compile proto files before running tests:**

```bash
# Compile proto files first
./script/compile-proto.sh

# Then run tests
go test -v -p 1 -coverprofile=coverage.out -covermode=atomic ./...

# Run tests for a specific package
go test -v ./server/
go test -v ./object/
go test -v ./client/

# View coverage report
go tool cover -html=coverage.out
```

### Protocol Buffer Changes

When modifying `.proto` files:

1. Edit the proto file in its respective directory
2. Run `./script/compile-proto.sh` to regenerate Go code
3. Update the corresponding Go implementations
4. Run tests to verify changes

Proto files to be aware of:
- `proto/goverse.proto` - Core Goverse protocol
- `client/proto/client.proto` - Client service protocol
- `inspector/proto/inspector.proto` - Inspector UI protocol
- `samples/chat/proto/chat.proto` - Chat sample protocol

## Code Style and Conventions

### General Go Conventions

- Follow standard Go conventions and idiomatic patterns
- Use `gofmt` for code formatting
- **Always run `go build ./...` and `go vet ./...`** before submitting PRs to catch common mistakes and ensure the code compiles
- Keep functions focused and methods concise
- Use meaningful variable names
- **Always remove deprecated methods** - Do not leave deprecated methods for compatibility. When a method is deprecated, remove it entirely rather than keeping it with a deprecation notice

### Distributed Object Patterns

When implementing distributed objects:

```go
type MyObject struct {
    goverseapi.BaseObject
    // Add your state fields here
    mu sync.Mutex  // Use mutex for concurrent access
}

func (obj *MyObject) MyMethod(ctx context.Context, req *proto.MyRequest) (*proto.MyResponse, error) {
    obj.mu.Lock()
    defer obj.mu.Unlock()

    // Implementation
    return &proto.MyResponse{}, nil
}
```

### Client Object Patterns

When implementing client objects:

```go
type MyClient struct {
    goverseapi.BaseClient
    // Add client-specific state
}

func (c *MyClient) MyMethod(ctx context.Context, req *proto.MyRequest) (*proto.MyResponse, error) {
    // Call distributed objects using goverseapi.CallObject
    resp, err := goverseapi.CallObject(ctx, "ObjectType-id", "MethodName", request)
    if err != nil {
        return nil, err
    }
    return resp.(*proto.MyResponse), nil
}
```

### Concurrency

- Use `sync.Mutex` to protect shared state in objects
- Lock at the beginning of methods that access shared state
- Use `defer obj.mu.Unlock()` immediately after locking
- Consider concurrency modes: Sequential, Concurrent, or Read-only

### Error Handling

- Always check and handle errors appropriately
- Return meaningful error messages
- Use `context.Context` for cancellation and timeouts
- Log errors using the util/logger package

### Logging

```go
import "github.com/xiaonanln/goverse/util/logger"

// Use the logger package for consistent logging
logger.Info("Starting operation")
logger.Error("Operation failed: %v", err)
logger.Debug("Debug information: %s", details)
```

## Testing Practices

### Test Coverage Goals

The project aims for high test coverage across all packages:

- **util packages**: Target 100% coverage
- **object package**: Target 100% coverage
- **client package**: Target 100% coverage
- **cluster package**: Target 90%+ coverage
- **server package**: Gradually improving coverage

### etcd Integration Test Isolation

**CRITICAL**: All tests using etcd must be properly isolated to prevent interference.

#### Using PrepareEtcdPrefix (Recommended)

For tests that actually connect to etcd, **always use `PrepareEtcdPrefix`** to ensure proper test isolation:

```go
import "github.com/xiaonanln/goverse/util/testutil"

func TestWithEtcd(t *testing.T) {
    // PrepareEtcdPrefix provides automatic test isolation
    // - Generates unique prefix based on test name: /goverse-test/TestWithEtcd
    // - Cleans up prefix before test starts
    // - Registers automatic cleanup via t.Cleanup after test ends
    prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
    
    // Use the prefix when creating etcd managers
    mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
    if err != nil {
        t.Skipf("Skipping test - etcd not available: %v", err)
        return
    }
    
    err = mgr.Connect()
    if err != nil {
        t.Skipf("Skipping test - etcd connection failed: %v", err)
        return
    }
    defer mgr.Close()
    
    // Your test code here - no manual cleanup needed
}
```

**Benefits of PrepareEtcdPrefix:**
- **Automatic Isolation**: Each test gets a unique prefix (e.g., `/goverse-test/TestName`)
- **Automatic Cleanup**: Cleans up before and after test via `t.Cleanup`
- **Consistent Pattern**: Same approach used across all integration tests
- **Parallel Test Safe**: Prevents interference between concurrent tests

**When NOT to use PrepareEtcdPrefix:**
- Unit tests that only create etcd manager objects without connecting
- Tests that don't actually interact with etcd
- Use hardcoded prefixes like `"/test"` for pure unit tests

### Cluster Singleton Reset Pattern

Tests that use the cluster singleton must reset it between tests:

```go
import "github.com/xiaonanln/goverse/cluster"

func TestWithCluster(t *testing.T) {
    // Reset cluster singleton for test isolation
    cluster.Get().ResetForTesting()
    
    // Your test code here
}
```

### Mock gRPC Server for Distributed Testing

When testing distributed scenarios that require inter-node gRPC communication, use the `TestServerHelper` and `MockGoverseServer` pattern:

```go
import "github.com/xiaonanln/goverse/cluster"

func TestDistributedScenario(t *testing.T) {
    t.Parallel()
    testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
    ctx := context.Background()

    // Create nodes
    node1 := node.NewNode("localhost:47001")
    node2 := node.NewNode("localhost:47002")
    
    // Start mock gRPC servers for inter-node communication
    mockServer1 := cluster.NewMockGoverseServer()
    mockServer1.SetNode(node1)  // Delegate to actual node
    testServer1 := cluster.NewTestServerHelper("localhost:47001", mockServer1)
    err := testServer1.Start(ctx)
    if err != nil {
        t.Fatalf("Failed to start mock server: %v", err)
    }
    defer testServer1.Stop()

    mockServer2 := cluster.NewMockGoverseServer()
    mockServer2.SetNode(node2)
    testServer2 := cluster.NewTestServerHelper("localhost:47002", mockServer2)
    err = testServer2.Start(ctx)
    if err != nil {
        t.Fatalf("Failed to start mock server: %v", err)
    }
    defer testServer2.Stop()

    // Now NodeConnections can establish gRPC connections
    err = cluster1.StartNodeConnections(ctx)
    if err != nil {
        t.Fatalf("Failed to start node connections: %v", err)
    }
    defer cluster1.StopNodeConnections()

    // Test distributed operations (routing, remote CreateObject, etc.)
}
```

**Key Points for Mock Server Testing:**
- `TestServerHelper` creates real TCP socket-based gRPC servers for testing (lightweight, localhost only)
- `MockGoverseServer` implements the Goverse gRPC service and delegates to actual nodes
- Always call `SetNode()` to connect the mock server to the real node implementation
- Mock servers enable actual gRPC calls through NodeConnections without requiring full server infrastructure
- Use unique ports (47001, 47002, etc.) to avoid conflicts between parallel tests
- Always defer `Stop()` to ensure cleanup even if tests fail

### MockPersistenceProvider for Persistence Testing

**CRITICAL**: When testing persistence functionality, use `MockPersistenceProvider` and **always access it through thread-safe methods** to avoid race conditions.

The `MockPersistenceProvider` is defined in `node/node_persistence_test.go` and provides an in-memory persistence implementation for testing.

#### Creating and Using MockPersistenceProvider

```go
import "github.com/xiaonanln/goverse/node"

func TestWithPersistence(t *testing.T) {
    // Create a new mock provider
    provider := node.NewMockPersistenceProvider()
    
    // Configure the node to use it
    n := node.NewNode("localhost:47000")
    n.SetPersistenceProvider(provider)
    
    // Create persistent objects and test...
}
```

#### Thread-Safe Access Methods

**ALWAYS use these thread-safe methods** when accessing the mock provider's data in tests:

```go
// Check if data exists for an object
if provider.HasStoredData("object-id") {
    // Object was saved
}

// Get stored data (returns a copy to prevent race conditions)
data := provider.GetStoredData("object-id")
if data != nil {
    // Unmarshal and verify data
    var protoMsg structpb.Struct
    object.UnmarshalProtoFromJSON(data, &protoMsg)
}

// Get the number of objects stored
count := provider.GetStorageCount()

// Get the number of times SaveObject was called
saveCount := provider.GetSaveCount()
```

#### NEVER Access Storage Directly

**❌ WRONG - Race Condition:**
```go
// DO NOT DO THIS - causes data races
if _, ok := provider.storage["object-id"]; ok {
    // ...
}
data := provider.storage["object-id"]
count := len(provider.storage)
```

**✅ CORRECT - Thread-Safe:**
```go
// Use the provided methods instead
if provider.HasStoredData("object-id") {
    // ...
}
data := provider.GetStoredData("object-id")
count := provider.GetStorageCount()
```

#### Why Thread Safety Matters

The `MockPersistenceProvider` is often accessed concurrently by:
- **Test goroutine**: Reading data to verify test expectations
- **Periodic persistence goroutine**: Writing data in the background
- **SaveAllObjects calls**: Writing data from various test operations

All access methods use an internal mutex to ensure thread-safe access to the underlying storage map. The `GetStoredData` method returns a **copy** of the data to prevent external modifications from causing race conditions.

#### Simulating Errors

You can configure the mock provider to simulate errors:

```go
provider := node.NewMockPersistenceProvider()

// Simulate save errors
provider.SaveErr = errors.New("simulated save error")

// Simulate load errors
provider.LoadErr = errors.New("simulated load error")
```

### Writing Tests

- Place tests in `*_test.go` files alongside the code
- Use table-driven tests for multiple scenarios
- Test both success and error cases
- Mock external dependencies when appropriate
- Use `t.Run()` for subtests
- Use `t.Cleanup()` for automatic cleanup even on test failure

Example test structure:

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid case", "input", "expected", false},
        {"error case", "bad", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("MyFunction() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("MyFunction() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Verifying Distributed Object Creation

When testing that objects are created on the correct nodes:

```go
// Get target node for an object based on shard mapping
targetNode, err := cluster.GetCurrentNodeForObject(ctx, objID)
if err != nil {
    t.Fatalf("GetCurrentNodeForObject failed: %v", err)
}

// Create the object (will be routed to target node)
createdID, err := cluster.CreateObject(ctx, "ObjectType", objID, nil)
if err != nil {
    t.Fatalf("CreateObject failed: %v", err)
}

// Verify object exists on the target node
var objectNode *node.Node
if targetNode == "localhost:47001" {
    objectNode = node1
} else if targetNode == "localhost:47002" {
    objectNode = node2
}

objExists := false
for _, obj := range objectNode.ListObjects() {
    if obj.Id == objID {
        objExists = true
        break
    }
}
if !objExists {
    t.Errorf("Object %s should exist on target node %s", objID, targetNode)
}
```

## Common Patterns

### Creating and Calling Objects

```go
// Create an object with a unique ID
// Note: Creating an object with the same ID multiple times will fail
// The second and subsequent attempts will return an error: "object with id <id> already exists"
goverseapi.CreateObject(ctx, "ObjectType", "ObjectType-uniqueId", initRequest)

// Call a method on an object
resp, err := goverseapi.CallObject(ctx, "ObjectType-id", "MethodName", request)
if err != nil {
    return nil, err
}
result := resp.(*proto.ResponseType)
```

**Important**: Object IDs must be unique within a node. Attempting to create multiple objects with the same ID will fail with an error. The creation operation is atomic - the system checks for existence and creates the object under a single lock to prevent race conditions.

### Server Setup

```go
config := &goverseapi.ServerConfig{
    ListenAddress:       "localhost:47000",
    AdvertiseAddress:    "localhost:47000",
    ClientListenAddress: "localhost:48000",
}
server, err := goverseapi.NewServer(config)
if err != nil {
    log.Fatal(err)
}

// Register types
goverseapi.RegisterClientType((*MyClient)(nil))
goverseapi.RegisterObjectType((*MyObject)(nil))

// Start server (blocks until shutdown)
server.Run()
```

### Cluster and Shard Mapping

```go
import "github.com/xiaonanln/goverse/cluster"

c := cluster.Get()

// Check if this node is the leader
if c.IsLeader() {
    // Initialize shard mapping (first time)
    err := c.InitializeShardMapping(ctx)
    if err != nil {
        log.Fatal(err)
    }
    
    // Or update when nodes change
    err = c.UpdateShardMapping(ctx)
    if err != nil {
        log.Fatal(err)
    }
}

// Start automatic shard mapping management (leader manages, others refresh)
err := c.StartShardMappingManagement(ctx)
if err != nil {
    log.Fatal(err)
}
defer c.StopShardMappingManagement()

// Get node for an object (any node can call this)
node, err := c.GetCurrentNodeForObject(ctx, "object-123")
if err != nil {
    log.Fatal(err)
}
```

### Client Connection

```go
// Connect to server
conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
client := client_pb.NewClientServiceClient(conn)

// Register client
stream, err := client.Register(ctx, &client_pb.Empty{})
regResp, err := stream.Recv()
clientID := regResp.(*client_pb.RegisterResponse).ClientId

// Call client methods
anyReq, _ := anypb.New(request)
resp, err := client.Call(ctx, &client_pb.CallRequest{
    ClientId: clientID,
    Method:   "MethodName",
    Request:  anyReq,
})
```

## File Organization

- Keep related functionality together in packages
- Place protocol buffers in `proto/` subdirectories within each package
- Generated protobuf code lives alongside `.proto` files
- Tests go in `*_test.go` files in the same directory as the code
- Sample applications go in `samples/` directory
- Scripts for development/CI go in `script/` directory

## Dependencies

- **gRPC**: Use for all RPC communication
- **Protocol Buffers**: Use for message serialization
- **etcd**: Used internally for cluster coordination
- Avoid adding new dependencies unless absolutely necessary
- Keep dependencies minimal and well-justified

## CI/CD

The project uses GitHub Actions for:

- **test.yml**: Run unit tests with coverage reporting
- **build.yml**: Build verification
- **chat.yml**: Test chat sample application
- **chat-clustered.yml**: Test clustered chat deployment
- **docker.yml**: Docker image builds

All tests must pass before merging. Maintain or improve test coverage with new changes.

## Additional Resources

- See `README.md` for feature overview and examples
- See `TESTING.md` for detailed test documentation
- See `samples/chat/` for a complete example application
- Use the Inspector UI at http://localhost:8080 for cluster visualization
