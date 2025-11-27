# Chat Sample

A distributed chat application demonstrating the Goverse virtual actor model with real-time push messaging.

## Features

- **Multiple Chat Rooms**: Predefined rooms (General, Technology, Crypto, Sports, Movies)
- **Real-time Messaging**: Server pushes new messages to connected clients via streaming gRPC
- **Distributed Architecture**: ChatRoom objects are distributed across nodes
- **Interactive CLI Client**: Command-line interface for joining rooms and chatting

## Quick Start

### Prerequisites

- Go 1.21+
- etcd (for cluster coordination)
- A Gate server (for client connections)

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
   go run .
   ```

3. **Start the Chat server**:
   ```bash
   cd samples/chat/server
   go run .
   ```

4. **Run the client**:
   ```bash
   cd samples/chat/client
   go run . -user alice
   ```

## Architecture

```
┌─────────────────┐     gRPC Stream     ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Chat Client   │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│   (CLI)         │ <───────────────── │   (:48000)      │ <──────────── │  (ChatRooms)    │
└─────────────────┘   Push Messages     └─────────────────┘               └─────────────────┘
```

### Components

- **ChatRoomMgr**: Singleton object that manages and creates chat rooms on startup
- **ChatRoom**: Virtual actor for each chat room, stores messages and connected users
- **Chat Client**: CLI client that connects via Gate for interactive chatting

## Client Commands

| Command | Description |
|---------|-------------|
| `/list` | List all available chat rooms |
| `/join <room>` | Join a chat room |
| `/messages` | Show recent messages in current room |
| `/quit` | Exit the client |
| `/help` | Show available commands |
| `<message>` | Send a message to the current room |

## Project Structure

```
samples/chat/
├── README.md           # This file
├── proto/
│   ├── chat.proto      # Protocol definitions
│   └── __init__.py     # Python package marker
├── server/
│   ├── chat_server.go  # Server entry point
│   ├── ChatRoom.go     # ChatRoom object implementation
│   └── ChatRoomMgr.go  # ChatRoomMgr object implementation
└── client/
    └── client.go       # Interactive CLI client
```

## License

MIT License - see the root LICENSE file.
