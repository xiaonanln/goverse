# Goverse Integration Test Setup

This directory contains integration tests for the Goverse distributed object runtime, including the chat sample application.

## Prerequisites

### System Requirements

1. **Python 3.x** with pip
2. **Go** (for building binaries)
3. **protobuf compiler** (`protoc`)
4. **Docker** (for running etcd)

### Installing Dependencies

```bash
# Install Python dependencies
pip3 install grpcio grpcio-tools protobuf

# Install protobuf compiler (Ubuntu/Debian)
sudo apt-get install -y protobuf-compiler

# Install Go protobuf plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Add Go bin to PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

### Generate Proto Files

Before running tests, compile the protocol buffer files:

```bash
# From repository root
./script/compile-proto.sh
```

This generates both Go and Python proto files needed for the tests.

### Start etcd

Goverse requires etcd for cluster coordination:

```bash
# Start etcd using Docker
docker run -d --name etcd-test \
  -p 2379:2379 \
  gcr.io/etcd-development/etcd:v3.5.0 \
  /usr/local/bin/etcd \
  --listen-client-urls http://0.0.0.0:2379 \
  --advertise-client-urls http://0.0.0.0:2379

# Verify etcd is running
curl http://localhost:2379/health
```

## Running Tests

### Chat Integration Test

```bash
# Run with default configuration (1 server)
python3 tests/integration/test_chat.py

# Run with multiple servers (clustered mode)
python3 tests/integration/test_chat.py --num-servers 2
```

## Architecture

The test infrastructure uses several helper classes:

- **BinaryHelper.py**: Compiles Go binaries with optional coverage instrumentation
- **Inspector.py**: Manages the inspector process for cluster visibility
- **ChatServer.py**: Manages chat server (Goverse node) processes
- **Gateway.py**: Manages the gateway process for client connections
- **ChatClient.py**: Python client that connects to the gateway and calls chat objects

### Component Roles

1. **Inspector**: Provides HTTP/gRPC interface for cluster monitoring
2. **Chat Server(s)**: Goverse nodes that host ChatRoom and ChatRoomMgr objects
3. **Gateway**: Handles client connections and routes calls to appropriate nodes
4. **Chat Client**: Test client that connects to gateway and exercises chat functionality

## Known Issues

### Push Messaging Test (Disabled)

The push messaging test is currently disabled due to a proto unmarshaling issue in the gateway:

```
[ERROR] [Gateway] Failed to unmarshal message for client: proto: not found
```

**Issue**: The gateway cannot unmarshal `Client_NewMessageNotification` proto messages pushed from chat rooms to clients.

**Root Cause**: The gateway needs application-specific proto types registered to unmarshal push notifications. This appears to be an architecture/design issue where the gateway (which is application-agnostic) needs to handle application-specific message types.

**Workaround**: The push messaging test is temporarily disabled. Basic chat functionality (list rooms, join, send, get messages) works correctly.

**To Fix**: The gateway or cluster architecture needs to be updated to handle application-specific proto types for push messages.

## Cleanup

```bash
# Stop and remove etcd container
docker stop etcd-test
docker rm etcd-test

# Clean up built binaries
rm -f /tmp/inspector /tmp/chat_server /tmp/gateway
```

## Coverage

To enable code coverage for built binaries:

```bash
# Set coverage environment variables before running tests
export ENABLE_COVERAGE=true
export GOCOVERDIR=/tmp/coverage
mkdir -p $GOCOVERDIR

# Run tests
python3 tests/integration/test_chat.py

# Generate coverage report
go tool covdata textfmt -i=$GOCOVERDIR -o coverage.txt
go tool cover -func=coverage.txt
```
