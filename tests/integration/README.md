# Integration Tests

This directory contains Python-based integration tests for GoVerse.

## Tests

### test_chat.py

End-to-end integration test for the distributed chat application.

**Usage:**
```bash
# Single server test
python3 tests/integration/test_chat.py

# Multi-server clustered test
python3 tests/integration/test_chat.py --num-servers 4
```

**What it tests:**
- Inspector service startup
- Chat server(s) startup with etcd coordination
- Chat client connection and messaging
- Node registration and object tracking
- Graceful shutdown with coverage collection

**ChatServer Class:**
The test script includes a `ChatServer` class that encapsulates gRPC communication with the chat server:
- `call_status()`: Get server status (advertise address, object count, uptime)
- `list_objects()`: List all objects on the server
- `call_method()`: Make generic RPC calls to objects
- `close()`: Properly close the gRPC channel
- Context manager support for automatic cleanup

### test_chat_server.py

Unit tests for the ChatServer class.

**Usage:**
```bash
# Run unit tests
python3 -m unittest tests.integration.test_chat_server -v
```

**What it tests:**
- ChatServer initialization and channel creation
- RPC methods (call_status, list_objects, call_method)
- Error handling for RPC failures
- Context manager protocol
- Proper cleanup of gRPC connections

**Requirements:**
- Go 1.21+
- Python 3.x with grpcio package
- etcd running locally
- Protobuf compiler and Go plugins

## Running Tests

From the repository root:

```bash
# Run unit tests for ChatServer class
python3 -m unittest tests.integration.test_chat_server -v

# Run single server integration test
python3 tests/integration/test_chat.py

# Run clustered integration test
python3 tests/integration/test_chat.py --num-servers 4
```

## CI Integration

These tests are run automatically in GitHub Actions:
- `.github/workflows/chat.yml` - Single server test
- `.github/workflows/chat-clustered.yml` - Multi-server clustered test

Both workflows collect runtime coverage from the binaries for comprehensive coverage reporting.
