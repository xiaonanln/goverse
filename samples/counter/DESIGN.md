# Counter Sample - Design Document

## Overview

A simple distributed counter service demonstrating the Goverse virtual actor model with basic CRUD operations. This sample shows how to create independent stateful objects (counters) that can be accessed concurrently from multiple clients.

## Goals

1. **Simple Introduction**: Demonstrate the basics of Goverse objects without complex domain logic
2. **Independent Objects**: Show how each counter maintains its own state independently
3. **Concurrent Access**: Demonstrate safe concurrent access to counters from multiple clients
4. **Basic Operations**: Implement increment, decrement, get, and reset operations
5. **RESTful Interface**: Provide HTTP/JSON API via the Gate server

## Non-Goals

- Authentication/authorization
- Persistence (counters reset when objects are removed)
- Complex counter logic (limits, atomic operations beyond increment/decrement)
- Real-time push notifications (simple request/response only)

## Architecture

```
┌─────────────────┐     HTTP/JSON       ┌─────────────────┐     gRPC      ┌─────────────────┐
│  HTTP Client    │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│  (curl/browser) │ <─────────────────  │   (:48000)      │ <──────────── │  (Counters)     │
└─────────────────┘    JSON Response    └─────────────────┘               └─────────────────┘
```

### Components

1. **Counter Object**: Virtual actor representing a single counter
   - Object ID format: `Counter-{name}` (e.g., `Counter-visitors`)
   - State: Single integer value
   - Methods: Increment, Decrement, Get, Reset

2. **Gate Server** (separate process): Handles HTTP requests and routes to counter objects
   - Standard Goverse Gate (no counter-specific code)
   - Pure routing and protocol translation (HTTP ↔ gRPC)
   - Started separately via `cmd/gate`

3. **Counter Server** (separate process): Hosts Counter objects
   - Registers the Counter object type
   - Counters are created on-demand when first accessed
   - Runs as a Goverse node

## API Design

### HTTP Endpoints

The Counter sample uses the standard Goverse HTTP API format:

**Base URL**: `http://localhost:48000/api/v1/objects`

All requests use base64-encoded protobuf wrapped in JSON:
```json
{
  "request": "<base64-encoded protobuf Any>"
}
```

All responses return base64-encoded protobuf:
```json
{
  "response": "<base64-encoded protobuf Any>"
}
```

#### Create Counter
```
POST /api/v1/objects/create/Counter/{name}
Request: {"request": "<base64 of google.protobuf.Empty>"}
Response: {"id": "Counter-visitors"}
```

#### Get Counter Value
```
POST /api/v1/objects/call/Counter/{name}/Get
Request: {"request": "<base64 of GetRequest>"}
Response: {"response": "<base64 of CounterResponse>"}
```

#### Increment Counter
```
POST /api/v1/objects/call/Counter/{name}/Increment
Request: {"request": "<base64 of IncrementRequest with amount=5>"}
Response: {"response": "<base64 of CounterResponse>"}
```

#### Decrement Counter
```
POST /api/v1/objects/call/Counter/{name}/Decrement
Request: {"request": "<base64 of DecrementRequest with amount=3>"}
Response: {"response": "<base64 of CounterResponse>"}
```

#### Reset Counter
```
POST /api/v1/objects/call/Counter/{name}/Reset
Request: {"request": "<base64 of ResetRequest>"}
Response: {"response": "<base64 of CounterResponse>"}
```

#### Delete Counter
```
POST /api/v1/objects/delete/Counter/{name}
Response: {"success": true}
```

### Protocol Buffers

```protobuf
message IncrementRequest {
  int32 amount = 1;
}

message DecrementRequest {
  int32 amount = 1;
}

message GetRequest {
  // Empty - no parameters needed
}

message ResetRequest {
  // Empty - no parameters needed
}

message CounterResponse {
  string name = 1;
  int32 value = 2;
}
```

## Counter Object Implementation

```go
type Counter struct {
    goverseapi.BaseObject
    mu    sync.Mutex
    value int32
}

func (c *Counter) OnCreated() {
    c.value = 0
}

func (c *Counter) Increment(ctx context.Context, req *pb.IncrementRequest) (*pb.CounterResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.value += req.Amount
    
    return &pb.CounterResponse{
        Name:  c.GetId(),
        Value: c.value,
    }, nil
}

func (c *Counter) Decrement(ctx context.Context, req *pb.DecrementRequest) (*pb.CounterResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.value -= req.Amount
    
    return &pb.CounterResponse{
        Name:  c.GetId(),
        Value: c.value,
    }, nil
}

func (c *Counter) Get(ctx context.Context, req *pb.GetRequest) (*pb.CounterResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    return &pb.CounterResponse{
        Name:  c.GetId(),
        Value: c.value,
    }, nil
}

func (c *Counter) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.CounterResponse, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    c.value = 0
    
    return &pb.CounterResponse{
        Name:  c.GetId(),
        Value: c.value,
    }, nil
}
```

## Gate Integration

The Counter sample uses the standard Goverse Gate HTTP API at `/api/v1/objects/*`. 

**No custom gate code is required** - the gate server already provides:
- `POST /api/v1/objects/create/{type}/{id}` - Create objects
- `POST /api/v1/objects/call/{type}/{id}/{method}` - Call object methods  
- `POST /api/v1/objects/delete/{type}/{id}` - Delete objects

The gate handles:
1. HTTP request parsing and validation
2. Base64 decoding of protobuf request
3. Routing to the correct node based on object ID
4. Encoding protobuf response to base64
5. HTTP response formatting

Example flow for `./counter.sh increment visitors 5`:
1. Helper script encodes `IncrementRequest{amount: 5}` to protobuf, then base64
2. Helper script POSTs to `/api/v1/objects/call/Counter/Counter-visitors/Increment`
3. Gate decodes request and routes to node hosting `Counter-visitors`
4. Counter object's `Increment` method executes
5. Gate encodes `CounterResponse` and returns to client
6. Helper script decodes and displays result

## Usage Examples

### Helper Script

Since the HTTP API uses base64-encoded protobuf, we'll provide a helper script `counter.sh` for easier testing:

```bash
#!/bin/bash
# Helper script for Counter operations

BASE_URL="http://localhost:48000/api/v1/objects"

# Usage: ./counter.sh create <name>
# Usage: ./counter.sh get <name>
# Usage: ./counter.sh increment <name> <amount>
# Usage: ./counter.sh decrement <name> <amount>
# Usage: ./counter.sh reset <name>
# Usage: ./counter.sh delete <name>
```

### Basic Counter Operations

Using the helper script:

```bash
# Create a counter
./counter.sh create visitors
# Output: Created counter: Counter-visitors

# Get current value (starts at 0)
./counter.sh get visitors
# Output: Counter visitors = 0

# Increment by 1
./counter.sh increment visitors 1
# Output: Counter visitors = 1

# Increment by 10
./counter.sh increment visitors 10
# Output: Counter visitors = 11

# Decrement by 5
./counter.sh decrement visitors 5
# Output: Counter visitors = 6

# Reset to 0
./counter.sh reset visitors
# Output: Counter visitors = 0

# Delete counter
./counter.sh delete visitors
# Output: Deleted counter: Counter-visitors
```

### Direct curl Examples

For those who want to use curl directly with base64-encoded protobuf:

```bash
# Create counter (using empty protobuf message)
EMPTY_BASE64=$(echo -n "" | base64)
curl -X POST http://localhost:48000/api/v1/objects/create/Counter/visitors \
  -H "Content-Type: application/json" \
  -d "{\"request\":\"$EMPTY_BASE64\"}"

# Note: Encoding protobuf messages to base64 is complex
# We recommend using the helper script or writing a proper client
```

### Multiple Independent Counters

```bash
# Each counter maintains its own state
./counter.sh create pageviews
./counter.sh create downloads
./counter.sh create logins

./counter.sh increment pageviews 100
./counter.sh increment downloads 50
./counter.sh increment logins 25

# They're independent objects
./counter.sh get pageviews  # Output: Counter pageviews = 100
./counter.sh get downloads  # Output: Counter downloads = 50
./counter.sh get logins     # Output: Counter logins = 25
```

## Project Structure

```
samples/counter/
├── DESIGN.md              # This file
├── README.md              # User documentation
├── counter.sh             # Helper script for testing
├── proto/
│   └── counter.proto      # Protocol buffer definitions
├── server/
│   ├── main.go           # Counter server entry point
│   └── counter.go        # Counter object implementation
└── compile-proto.sh      # Proto compilation script
```

## Running the Sample

The Counter sample requires three separate processes:

1. **Start etcd** (terminal 1):
   ```bash
   ./script/codespace/start-etcd.sh
   ```

2. **Start Gate server** (terminal 2):
   ```bash
   cd cmd/gate
   go run .
   # Gate listens on :48000 for HTTP
   ```

3. **Start Counter server** (terminal 3):
   ```bash
   cd samples/counter/server
   go run .
   # Counter server registers with etcd and gate
   ```

4. **Test with helper script** (terminal 4):
   ```bash
   cd samples/counter
   ./counter.sh create test
   ./counter.sh increment test 5
   ./counter.sh get test
   ```

**Process Architecture:**
```
┌─────────────┐     ┌─────────────┐     ┌──────────────────┐
│   etcd      │ ←── │    Gate     │ ←── │  HTTP Client     │
│  :2379      │     │   :48000    │     │  (counter.sh)    │
└─────────────┘     └─────────────┘     └──────────────────┘
      ↑                     ↓
      │                     ↓ gRPC
      │              ┌─────────────┐
      └────────────→ │   Counter   │
                     │   Server    │
                     │  (node)     │
                     └─────────────┘
```

## Learning Outcomes

After studying this sample, developers will understand:

1. **Object Creation**: How Goverse objects are created and identified
2. **State Management**: How objects maintain independent state with proper locking
3. **Method Invocation**: How to call methods on Goverse objects
4. **HTTP Integration**: How the Gate translates HTTP requests to object calls
5. **Distribution**: How counters are automatically distributed across nodes
6. **Concurrency**: How multiple clients can safely access the same counter

## Future Enhancements

Potential extensions for learning:

1. **Persistence**: Add database persistence for counter values
2. **Limits**: Add min/max bounds checking
3. **Atomic Operations**: Add compare-and-swap operations
4. **Metrics**: Add Prometheus metrics for counter operations
5. **WebSocket**: Add real-time updates when counter values change
6. **Rate Limiting**: Add per-counter rate limiting
