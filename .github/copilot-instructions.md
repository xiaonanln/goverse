# GitHub Copilot Instructions for Pulse

## Project Overview

Pulse is a **distributed object runtime for Go** implementing the **virtual actor (grain) model**. It enables building systems around stateful entities with identity and methods, while the runtime handles placement, routing, lifecycle, and fault-tolerance.

### Core Concepts

- **Distributed Objects (Grains)**: Uniquely addressable, stateful entities with custom methods
- **Virtual Actor Lifecycle**: Objects are activated on demand, deactivated when idle, and reactivated seamlessly
- **Client Service**: Client connection management and method routing through server-side client objects
- **Sharding & Rebalancing**: Fixed shard model with automatic remapping via etcd
- **Fault-Tolerance**: Lease + epoch fencing prevent split-brain; safe recovery after node failures

## Architecture

### Key Components

- **server/** - Node server implementation
- **node/** - Core node logic and object management
- **object/** - Object base types and helpers (BaseObject)
- **client/** - Client service implementation and protocol definitions (BaseClient)
- **cluster/** - Cluster singleton management
- **pulseapi/** - API wrapper functions for the framework
- **proto/** - Core Pulse protocol definitions
- **util/** - Logging and utility helpers
- **samples/chat/** - Distributed chat application example
- **inspector/** - Web UI for cluster visualization
- **cmd/inspector/** - Inspector web server

### Base Types

- All distributed objects should extend `pulseapi.BaseObject`
- All client objects should extend `pulseapi.BaseClient`
- Use `pulseapi.CallObject()` to call methods on distributed objects
- Register types with `pulseapi.RegisterObjectType()` and `pulseapi.RegisterClientType()`

## Development Workflow

### Prerequisites

```bash
# Install protoc compiler
sudo apt-get install -y protobuf-compiler

# Install Go protobuf plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

### Building the Project

```bash
# Compile protobuf files first
./script/compile-proto.sh

# Tidy dependencies
go mod tidy

# Build the project
go build ./...
```

### Running Tests

```bash
# Run all tests with coverage
go test -v -coverprofile=coverage.out -covermode=atomic ./...

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
- `proto/pulse.proto` - Core Pulse protocol
- `client/proto/client.proto` - Client service protocol
- `inspector/proto/inspector.proto` - Inspector UI protocol
- `samples/chat/proto/chat.proto` - Chat sample protocol

## Code Style and Conventions

### General Go Conventions

- Follow standard Go conventions and idiomatic patterns
- Use `gofmt` for code formatting
- Keep functions focused and methods concise
- Use meaningful variable names

### Distributed Object Patterns

When implementing distributed objects:

```go
type MyObject struct {
    pulseapi.BaseObject
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
    pulseapi.BaseClient
    // Add client-specific state
}

func (c *MyClient) MyMethod(ctx context.Context, req *proto.MyRequest) (*proto.MyResponse, error) {
    // Call distributed objects using pulseapi.CallObject
    resp, err := pulseapi.CallObject(ctx, "ObjectType-id", "MethodName", request)
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
import "github.com/simonlingoogle/pulse/util/logger"

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

### Writing Tests

- Place tests in `*_test.go` files alongside the code
- Use table-driven tests for multiple scenarios
- Test both success and error cases
- Mock external dependencies when appropriate
- Use `t.Run()` for subtests

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

## Common Patterns

### Creating and Calling Objects

```go
// Create an object
pulseapi.CreateObject(ctx, "ObjectType", "ObjectType-uniqueId", initRequest)

// Call a method on an object
resp, err := pulseapi.CallObject(ctx, "ObjectType-id", "MethodName", request)
if err != nil {
    return nil, err
}
result := resp.(*proto.ResponseType)
```

### Server Setup

```go
config := &pulseapi.ServerConfig{
    ListenAddress:       "localhost:47000",
    AdvertiseAddress:    "localhost:47000",
    ClientListenAddress: "localhost:48000",
}
server := pulseapi.NewServer(config)

// Register types
pulseapi.RegisterClientType((*MyClient)(nil))
pulseapi.RegisterObjectType((*MyObject)(nil))

// Start server
server.Run()
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
