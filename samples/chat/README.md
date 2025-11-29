# Chat Sample

A distributed chat application demonstrating the Goverse virtual actor model with real-time push messaging.

## Features

- **Multiple Chat Rooms**: Predefined rooms (General, Technology, Crypto, Sports, Movies)
- **Real-time Messaging**: Server pushes new messages to connected clients via streaming gRPC
- **Distributed Architecture**: ChatRoom objects are distributed across nodes
- **Interactive CLI Client**: Command-line interface for joining rooms and chatting
- **Web Client**: Browser-based chat client using HTTP Gate API

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

5. **Run the web client** (alternative):
   ```bash
   # Start the Gate server with HTTP enabled
   cd cmd/gate
   go run . -http-listen :8080
   
   # Serve the web client (in another terminal)
   cd samples/chat/web
   python3 -m http.server 3000
   
   # Open http://localhost:3000 in your browser
   ```

## Architecture

```
┌─────────────────┐     gRPC Stream     ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Chat Client   │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│   (CLI)         │ <───────────────── │   (:49000)      │ <──────────── │  (ChatRooms)    │
└─────────────────┘   Push Messages     └─────────────────┘               └─────────────────┘

┌─────────────────┐     HTTP + Polling  ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Web Client    │ ─────────────────>  │   Gate HTTP     │ ────────────> │  Goverse Node   │
│   (Browser)     │ <───────────────── │   (:8080)       │ <──────────── │  (ChatRooms)    │
└─────────────────┘                     └─────────────────┘               └─────────────────┘
```

### Components

- **ChatRoomMgr**: Singleton object that manages and creates chat rooms on startup
- **ChatRoom**: Virtual actor for each chat room, stores messages and connected users
- **CLI Client**: Command-line client that connects via Gate gRPC for interactive chatting
- **Web Client**: Browser-based client that uses HTTP Gate API with polling for messages

## CLI Client Commands

| Command | Description |
|---------|-------------|
| `/list` | List all available chat rooms |
| `/join <room>` | Join a chat room |
| `/messages` | Show recent messages in current room |
| `/quit` | Exit the client |
| `/help` | Show available commands |
| `<message>` | Send a message to the current room |

## Web Client Features

The web client provides a browser-based interface:

- **Room Selection**: Click on a room to join
- **Username**: Enter your name before joining a room
- **Send Messages**: Type and send messages in real-time
- **Receive Messages**: Messages from other users are fetched via polling
- **Leave Room**: Click the back button to return to room list

### API Override

You can specify a custom API endpoint using the `?api=` query parameter:
```
http://localhost:3000?api=http://custom-gate:8080/api/v1
```

## Project Structure

```
samples/chat/
├── README.md           # This file
├── proto/
│   └── chat.proto      # Protocol definitions
├── server/
│   ├── chat_server.go  # Server entry point
│   ├── ChatRoom.go     # ChatRoom object implementation
│   └── ChatRoomMgr.go  # ChatRoomMgr object implementation
├── client/
│   └── client.go       # Interactive CLI client
└── web/
    ├── index.html      # Web client HTML
    ├── chat.js         # Web client JavaScript
    └── style.css       # Web client styling
```

## License

MIT License - see the root LICENSE file.
