# Tic Tac Toe Demo Design

A simple web-based Tic Tac Toe game demonstrating Goverse HTTP Gate, where player plays as X against an AI opponent (O).

---

## Architecture

```
┌─────────────────┐     HTTP REST      ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Web Browser   │ ─────────────────> │   HTTP Gate     │ ────────────> │  Goverse Node   │
│   (HTML/JS)     │ <───────────────── │   (:8080)       │ <──────────── │  (TicTacToe)    │
└─────────────────┘                    └─────────────────┘               └─────────────────┘
```

### Distributed Server Support

The design inherently supports distributed deployment through Goverse's virtual actor model:

**Scalability:**
- Multiple Goverse nodes can run simultaneously
- Each TicTacToe object (game instance) is assigned to a specific node via consistent hashing (8192 shards)
- The HTTP Gate routes requests to the correct node automatically based on object ID
- No single point of failure for game objects

**Distribution Example:**
```
┌──────────────┐
│  Web Client  │
└──────┬───────┘
       │ HTTP
       ▼
┌──────────────┐         ┌─────────────┐
│  HTTP Gate   │◄───────►│    etcd     │  (Coordination)
└──────┬───────┘         └─────────────┘
       │ gRPC                    ▲
       │                         │
       ├─────────────────────────┼─────────────────┐
       │                         │                 │
       ▼                         ▼                 ▼
┌─────────────┐           ┌─────────────┐   ┌─────────────┐
│   Node 1    │◄─────────►│   Node 2    │◄─►│   Node 3    │
│ game-abc    │   gRPC    │ game-xyz    │   │ game-123    │
└─────────────┘           └─────────────┘   └─────────────┘
```

**Key Features:**
- **Automatic routing:** Gate uses shard mapping to route `game-{id}` to the correct node
- **Load distribution:** Games are distributed across nodes via consistent hashing
- **Node-to-node communication:** If needed (future), objects can communicate across nodes
- **Dynamic scaling:** Add more nodes to handle more concurrent games
- **Fault tolerance:** If a node fails, its games can be recreated on other nodes (without persistence, games are lost; with persistence, they'd be recovered)

**For this demo:**
- Single node deployment is sufficient for demonstration
- Architecture supports multi-node without code changes
- Simply start multiple nodes with the same etcd cluster to scale

---

## Directory Structure

```
samples/tictactoe/
├── DESIGN.md           # Design document
├── README.md           # User documentation  
├── proto/
│   ├── tictactoe.proto
│   └── tictactoe.pb.go
├── server/
│   ├── main.go         # Server entry point
│   └── tictactoe.go    # TicTacToe object implementation
└── web/
    ├── index.html      # Game UI
    ├── style.css       # Styling
    └── game.js         # Game logic & HTTP client
```

---

## TicTacToe Object

**Fixed pool:** 10 objects with IDs `game-1`, `game-2`, ..., `game-10`

**State:**
```go
type TicTacToe struct {
    goverseapi.BaseObject
    mu       sync.Mutex
    board    [9]string  // "", "X", or "O"
    status   string     // "playing", "x_wins", "o_wins", "draw"
    moves    int        // Total moves made
}
```

**Methods:**

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `NewGame` | (empty) | `GameState` | Create/reset game |
| `MakeMove` | `{position: 0-8}` | `GameState` | Player move + AI response |
| `GetState` | (empty) | `GameState` | Get current game state |

---

## Proto Definitions

```protobuf
syntax = "proto3";
package tictactoe;
option go_package = "github.com/xiaonanln/goverse/samples/tictactoe/proto";

message MoveRequest {
  int32 position = 1;  // 0-8, representing board position
}

message GameState {
  repeated string board = 1;   // 9 elements: "", "X", or "O"
  string status = 2;           // "playing", "x_wins", "o_wins", "draw"
  string winner = 3;           // "", "X", or "O"
  int32 last_ai_move = 4;      // -1 if no AI move yet, or 0-8
}
```

---

## AI Logic (Simple)

1. Take winning move if available
2. Block opponent's winning move
3. Take center if available
4. Take a corner
5. Take any edge

This provides reasonable gameplay without being unbeatable.

---

## API Endpoints

### Create Game
```bash
POST /api/v1/objects/create/TicTacToe/game-{id}
Response: {"id": "game-abc123"}
```

### Start New Game
```bash
POST /api/v1/objects/call/TicTacToe/game-{id}/NewGame
{"request": "<empty Any>"}
Response: {"response": "<GameState Any>"}
```

### Make Move
```bash
POST /api/v1/objects/call/TicTacToe/game-{id}/MakeMove
{"request": "<MoveRequest Any: {position: 4}>"}
Response: {"response": "<GameState Any>"}
```

### Get State
```bash
POST /api/v1/objects/call/TicTacToe/game-{id}/GetState
{"request": "<empty Any>"}
Response: {"response": "<GameState Any>"}
```

---

## Web Client UI

```
┌────────────────────────────────────┐
│         Tic Tac Toe                │
│                                    │
│     ┌───┬───┬───┐                  │
│     │ X │   │ O │                  │
│     ├───┼───┼───┤                  │
│     │   │ X │   │                  │
│     ├───┼───┼───┤                  │
│     │   │   │   │                  │
│     └───┴───┴───┘                  │
│                                    │
│     Status: Your turn              │
│                                    │
│     [New Game]                     │
└────────────────────────────────────┘
```

### Game Flow

Each web client represents a user - no login or authentication required. The system uses a fixed pool of 10 game objects (`game-1` through `game-10`).

1. **Page load** → Check localStorage for saved game ID → If none, randomly pick from 1-10 → Save to localStorage
2. **Initialize** → Create object if not exists → Start new game
3. **Click cell** → Send MakeMove → Display result (includes AI move)
4. **Game over** → Show winner/draw → Enable "New Game" button
5. **New Game** → Reset board → Ready to play
6. **Page refresh** → Reuse same game ID from localStorage → Resume or restart game

**Note:** If multiple users randomly pick the same game number, they will share the same game board (they'll see each other's moves). This demonstrates object sharing in distributed systems.

### How Clients Know Which Object to Access

**Fixed object pool (1-10):**
```javascript
// On page load, randomly choose a game from fixed pool
function chooseGameID() {
    const gameNumber = Math.floor(Math.random() * 10) + 1;  // 1-10
    return 'game-' + gameNumber;
}

// Store in localStorage to persist across page refreshes
let gameID = localStorage.getItem('tictactoe_game_id');
if (!gameID) {
    gameID = chooseGameID();  // e.g., "game-5"
    localStorage.setItem('tictactoe_game_id', gameID);
}
```

**Object ID usage:**
- Client randomly selects one game from a fixed pool: `game-1` through `game-10`
- This ID is saved in localStorage and reused on page refresh
- All subsequent API calls use this same ID: `/api/v1/objects/call/TicTacToe/{gameID}/MakeMove`
- The ID uniquely identifies the player's game instance

**Why this works:**
- **Fixed pool:** Only 10 TicTacToe objects exist: `game-1`, `game-2`, ..., `game-10`
- **Random selection:** Each browser randomly picks one and keeps it
- **Persistence:** localStorage ensures the same game is used after page refresh
- **Shared games:** Multiple players might randomly choose the same game (they'll see each other's moves)
- **Distributed:** The 10 games are distributed across nodes via consistent hashing
- **Pre-created:** Objects can be pre-created at server startup, or created on first access

**Benefits of fixed pool:**
- Predictable object IDs for debugging and monitoring
- Demonstrates distributed object placement (10 games across N nodes)
- Simpler than globally unique IDs
- Shows how multiple clients can interact with same objects (if they pick the same game)

---

## Implementation Plan

### Phase 1: Server (Go)
1. Create proto file and generate Go code
2. Implement TicTacToe object with game logic
3. Implement simple AI
4. Create main.go with server setup

### Phase 2: Web Client (HTML/JS)
1. Create index.html with game board
2. Create style.css for visual design
3. Create game.js with HTTP client and game logic
4. Add protobuf.js for encoding/decoding

### Phase 3: Integration
1. Test end-to-end flow
2. Add error handling
3. Write README with setup instructions

---

## Running the Demo

### Single Node (Simple)

```bash
# Terminal 1: Start etcd
./script/codespace/start-etcd.sh

# Terminal 2: Start server
cd samples/tictactoe/server
go run .

# Terminal 3: Serve web client
cd samples/tictactoe/web
python3 -m http.server 3000

# Open browser
open http://localhost:3000
```

### Multi-Node (Distributed)

To demonstrate distributed capabilities, run multiple nodes:

```bash
# Terminal 1: Start etcd
./script/codespace/start-etcd.sh

# Terminal 2: Start first node
cd samples/tictactoe/server
go run . --node-addr=localhost:50051

# Terminal 3: Start second node
cd samples/tictactoe/server
go run . --node-addr=localhost:50052

# Terminal 4: Start third node
cd samples/tictactoe/server
go run . --node-addr=localhost:50053

# Terminal 5: Serve web client
cd samples/tictactoe/web
python3 -m http.server 3000

# Open browser
open http://localhost:3000
```

Games will automatically be distributed across all three nodes based on their game IDs.

---

## Simplifications

- **No login required** - Each web client is a user; no authentication needed
- **Independent games** - Multiple players can play simultaneously on different browsers, each with their own game
- **No multiplayer** - Player vs AI only (no player vs player)
- **No persistence** - Games exist only in memory
- **No real-time updates** - Request/response model only (no SSE)
- **Single game per ID** - Object ID is the game ID

---

## Future Enhancements (Out of Scope)

- Real-time multiplayer with SSE push messaging
- Game lobby with matchmaking
- Persistent game history
- Leaderboard
- Difficulty levels for AI
