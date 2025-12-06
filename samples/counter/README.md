# Counter Sample

A simple distributed counter service demonstrating the Goverse virtual actor model with basic CRUD operations.

## Features

- **Simple Introduction**: Demonstrates the basics of Goverse objects without complex domain logic
- **Independent Objects**: Each counter maintains its own state independently
- **Concurrent Access**: Safe concurrent access to counters from multiple clients
- **Basic Operations**: Increment, decrement, get, and reset operations
- **RESTful Interface**: HTTP/JSON API via the Gate server

## Quick Start

### Prerequisites

- Go 1.21+
- Python 3.8+ with protobuf: `pip install grpcio grpcio-tools protobuf`
- etcd (for cluster coordination)
- A Gate server (for HTTP client connections)

### Running the Demo

1. **Start etcd**:
   ```bash
   # Using the provided script
   ./script/codespace/start-etcd.sh
   
   # Or using Docker
   docker run -d --name etcd -p 2379:2379 \
     quay.io/coreos/etcd:latest \
     /usr/local/bin/etcd \
     --listen-client-urls http://0.0.0.0:2379 \
     --advertise-client-urls http://localhost:2379
   ```

2. **Start the Gate server**:
   ```bash
   cd cmd/gate
   go run . --http-listen=:48000
   ```

3. **Start the Counter server**:
   ```bash
   cd samples/counter/server
   go run .
   ```

4. **Compile proto files** (first time only):
   ```bash
   cd samples/counter
   ./compile-proto.sh
   ```

5. **Use the CLI to interact with counters**:
   ```bash
   cd samples/counter
   python counter.py create visitors
   python counter.py increment visitors 5
   python counter.py get visitors
   # Output: Counter visitors = 5
   ```

## CLI Usage

The `counter.py` script provides a simple interface to interact with counters:

```bash
# Create a counter
python counter.py create visitors

# Get current value
python counter.py get visitors

# Increment by an amount
python counter.py increment visitors 10

# Decrement by an amount
python counter.py decrement visitors 3

# Reset to zero
python counter.py reset visitors

# Delete a counter
python counter.py delete visitors
```

## Architecture

```
┌─────────────────┐     HTTP/JSON       ┌─────────────────┐     gRPC      ┌─────────────────┐
│  HTTP Client    │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│  (curl/browser) │ <─────────────────  │   (:48000)      │ <──────────── │  (Counters)     │
└─────────────────┘    JSON Response    └─────────────────┘               └─────────────────┘
```

### Components

- **Counter Object**: Virtual actor representing a single counter
  - Object ID format: `Counter-{name}` (e.g., `Counter-visitors`)
  - State: Single integer value
  - Methods: Increment, Decrement, Get, Reset

- **Gate Server**: Handles HTTP requests and routes to counter objects
  - Standard Goverse Gate (no counter-specific code)
  - Pure routing and protocol translation (HTTP ↔ gRPC)

- **Counter Server**: Hosts Counter objects
  - Registers the Counter object type
  - Counters are created on-demand when first accessed
  - Runs as a Goverse node

## Project Structure

```
samples/counter/
├── DESIGN.md              # Design document
├── README.md              # This file
├── counter.py             # CLI to interact with counters
├── proto/
│   └── counter.proto      # Protocol buffer definitions
├── server/
│   ├── main.go           # Counter server entry point
│   └── counter.go        # Counter object implementation
└── compile-proto.sh      # Proto compilation script
```

## API Design

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

### Endpoints

| Operation | Method | Endpoint |
|-----------|--------|----------|
| Create Counter | POST | `/api/v1/objects/create/Counter/{name}` |
| Get Counter Value | POST | `/api/v1/objects/call/Counter/{name}/Get` |
| Increment Counter | POST | `/api/v1/objects/call/Counter/{name}/Increment` |
| Decrement Counter | POST | `/api/v1/objects/call/Counter/{name}/Decrement` |
| Reset Counter | POST | `/api/v1/objects/call/Counter/{name}/Reset` |
| Delete Counter | POST | `/api/v1/objects/delete/Counter/{name}` |

## License

Apache-2.0 License - see the root LICENSE file.
