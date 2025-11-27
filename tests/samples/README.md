# Integration Tests

This directory contains Python-based integration tests for GoVerse.

## Tests

### test_chat.py

End-to-end integration test for the distributed chat application.

**Usage:**
```bash
# Single server test
python3 tests/samples/chat/test_chat.py

# Multi-server clustered test
python3 tests/samples/chat/test_chat.py --num-servers 4
```

**What it tests:**
- Inspector service startup
- Chat server(s) startup with etcd coordination
- Chat client connection and messaging
- Node registration and object tracking
- Graceful shutdown with coverage collection

**ChatServer Class:**
The test script includes a `ChatServer` class that manages chat server processes and gRPC communication:
- **Process Management:**
  - `start()`: Start the chat server process
  - `stop()`: Gracefully stop the server process
  - `wait_for_ready()`: Wait for server to be ready to accept connections
- **RPC Methods:**
  - `Status()`: Get server status (advertise address, object count, uptime)
  - `ListObjects()`: List all objects on the server
  - `CallObject()`: Make generic RPC calls to objects
- **Connection Management:**
  - `connect()`: Establish gRPC connection
  - `close()`: Close the gRPC channel
  - Context manager support for automatic cleanup

**Requirements:**
- Go 1.21+
- Python 3.x with grpcio package
- etcd running locally
- Protobuf compiler and Go plugins

## Running Tests

From the repository root:

```bash
# Run single server integration test
python3 tests/samples/chat/test_chat.py

# Run clustered integration test
python3 tests/samples/chat/test_chat.py --num-servers 4
```

## CI Integration

These tests are run automatically in GitHub Actions:
- `.github/workflows/chat.yml` - Single server test
- `.github/workflows/chat-clustered.yml` - Multi-server clustered test

Both workflows collect runtime coverage from the binaries for comprehensive coverage reporting.
