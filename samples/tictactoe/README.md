# Tic Tac Toe Demo

A simple web-based Tic Tac Toe game demonstrating the Goverse distributed object runtime with HTTP Gate.

## Features

- **Player vs AI**: Play as X against a simple but strategic AI opponent (O)
- **Distributed Architecture**: Uses Goverse virtual actor model for game state management
- **HTTP REST API**: Web client communicates via HTTP Gate
- **Service-Based Design**: 10 TicTacToeService objects (`service-1` through `service-10`) manage multiple games
- **Unlimited Games**: Each service can host many concurrent games, each identified by a unique game ID
- **Session Persistence**: Game ID saved in localStorage for page refresh

## Quick Start

### Prerequisites

- Go 1.21+
- etcd (for cluster coordination)
- A web browser

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

2. **Start the server** (runs both Node and HTTP Gate):
   ```bash
   cd samples/tictactoe/server
   go run .
   # This starts:
   #   - Goverse Node on localhost:50051
   #   - HTTP Gate on localhost:49000 (gRPC) and :8080 (REST API)
   ```

3. **Serve the web client**:
   ```bash
   cd samples/tictactoe/web
   python3 -m http.server 3000
   ```

4. **Play the game**:
   Open http://localhost:3000 in your browser

## Architecture

```
┌─────────────────┐     HTTP REST      ┌─────────────────┐     gRPC      ┌─────────────────────────────┐
│   Web Browser   │ ─────────────────> │   HTTP Gate     │ ────────────> │   Goverse Node              │
│   (HTML/JS)     │ <───────────────── │   (:8080)       │ <──────────── │   TicTacToeService (1-10)   │
└─────────────────┘                    └─────────────────┘               │     └── TicTacToeGame (N)   │
                                                                         └─────────────────────────────┘
```

### Design

- **TicTacToeService**: Distributed object (10 instances: service-1 to service-10)
  - Each service can manage many concurrent games
  - Services are distributed across nodes for load balancing
  
- **TicTacToeGame**: Plain struct (not a distributed object)
  - Stores game state: board, status, moves, AI move history
  - Created on-demand when a user starts a new game
  - Each game has a unique ID (e.g., `user-abc123-1699999999999`)

## API Endpoints

All requests include a `game_id` to identify the specific game within the service.

### Create/Reset Game
```bash
POST /api/v1/objects/call/TicTacToeService/{service-id}/NewGame
# Request includes: game_id (string)
```

### Make a Move
```bash
POST /api/v1/objects/call/TicTacToeService/{service-id}/MakeMove
# Request includes: game_id (string), position (0-8)
```

### Get Game State
```bash
POST /api/v1/objects/call/TicTacToeService/{service-id}/GetState
# Request includes: game_id (string)
```

## Game State

The game state includes:
- `game_id`: Unique identifier for this game
- `board`: Array of 9 strings ("", "X", or "O")
- `status`: "playing", "x_wins", "o_wins", or "draw"
- `winner`: "", "X", or "O"
- `last_ai_move`: -1 or 0-8

## AI Strategy

The AI uses a simple strategy:
1. Take a winning move if available
2. Block the player's winning move
3. Take the center
4. Take a corner
5. Take any edge

## Multi-Node Deployment

For distributed deployment:

```bash
# Start multiple nodes
go run . --node-addr=localhost:50051
go run . --node-addr=localhost:50052  # Different terminal
go run . --node-addr=localhost:50053  # Different terminal
```

Services are automatically distributed across nodes via consistent hashing.

## Project Structure

```
samples/tictactoe/
├── DESIGN.md           # Detailed design document
├── README.md           # This file
├── proto/
│   ├── tictactoe.proto # Protocol definitions
│   └── tictactoe.pb.go # Generated Go code
├── server/
│   ├── main.go         # Server entry point
│   └── tictactoe.go    # TicTacToeService and TicTacToeGame implementation
└── web/
    ├── index.html      # Game UI
    ├── style.css       # Styling
    └── game.js         # Game logic & HTTP client
```

## License

MIT License - see the root LICENSE file.
