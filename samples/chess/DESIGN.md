# Chess Game Sample - Design Document

A distributed online chess game demonstrating the Goverse virtual actor model with player registration, matchmaking, game play, and scoring.

---

## Overview

This sample implements a complete online chess experience where:

- **Players register and login** with username and password
- **Players queue for matchmaking** and get paired with opponents
- **Two matched players play a chess game** with standard rules
- **Games end in win, lose, or draw** outcomes
- **Winners gain score, losers lose score** (ELO-like rating)

---

## Goals

1. **Multiplayer Gaming**: Demonstrate real two-player interactions via Goverse objects
2. **Player Authentication**: Show registration and login patterns with distributed objects
3. **Matchmaking**: Implement a matchmaking queue using distributed objects
4. **Game State**: Manage complex game state (chess board) with proper serialization
5. **Scoring System**: Track player ratings across games with persistence-ready design
6. **Real-time Updates**: Use push messaging to notify players of game events

---

## Non-Goals

- Full chess rule implementation (castling, en passant, pawn promotion - simplified)
- Advanced ELO algorithm (simple +10/-10 scoring)
- Password hashing/encryption (demo purposes only)
- Persistence (in-memory only for this sample)
- Spectator mode
- Reconnection after disconnect
- Time controls (no clock)

---

## Architecture

```
┌─────────────────┐     gRPC Stream     ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Chess Client  │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│   (CLI/Web)     │ <───────────────── │   (:48000)      │ <──────────── │  (Chess Objects)│
└─────────────────┘   Push Messages     └─────────────────┘               └─────────────────┘
```

### Distributed Deployment

```
┌──────────────┐
│  Chess Client │
└──────┬───────┘
       │ gRPC
       ▼
┌──────────────┐         ┌─────────────┐
│     Gate     │◄───────►│    etcd     │  (Coordination)
└──────┬───────┘         └─────────────┘
       │ gRPC                    ▲
       │                         │
       ├─────────────────────────┼─────────────────┐
       │                         │                 │
       ▼                         ▼                 ▼
┌─────────────┐           ┌─────────────┐   ┌─────────────┐
│   Node 1    │◄─────────►│   Node 2    │◄─►│   Node 3    │
│ Player-X    │   gRPC    │ ChessGame-Y │   │ Matchmaker  │
│ ChessGame-Z │           │ Player-W    │   │ Player-V    │
└─────────────┘           └─────────────┘   └─────────────┘
```

---

## Objects

### 1. Player Object

**Object ID**: `Player-{username}` (e.g., `Player-alice`, `Player-bob`)

**Purpose**: Represents a registered player with their credentials and rating.

**State**:
```go
type Player struct {
    goverseapi.BaseObject
    mu            sync.Mutex
    Username      string
    PasswordHash  string    // Plain text for demo, hash in production
    Score         int32     // Player rating (starts at 1000)
    GamesPlayed   int32     // Total games
    GamesWon      int32     // Wins count
    GamesLost     int32     // Losses count
    GamesDrawn    int32     // Draws count
    CurrentGameID string    // Active game (empty if not in game)
    IsLoggedIn    bool      // Login status
    ClientID      string    // For push messaging when logged in
}
```

**Methods**:

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `Register` | `RegisterRequest{password}` | `RegisterResponse{success, message}` | Create new player account |
| `Login` | `LoginRequest{password, clientID}` | `LoginResponse{success, player_info}` | Authenticate and start session |
| `Logout` | `LogoutRequest` | `LogoutResponse` | End session |
| `GetProfile` | `GetProfileRequest` | `ProfileResponse` | Get player stats |
| `UpdateScore` | `UpdateScoreRequest{delta}` | `UpdateScoreResponse` | Update score (internal use) |
| `SetCurrentGame` | `SetCurrentGameRequest{gameID}` | `SetCurrentGameResponse` | Track active game |

---

### 2. Matchmaker Object

**Object ID**: `Matchmaker` (singleton)

**Purpose**: Manages the matchmaking queue and pairs players for games.

**State**:
```go
type Matchmaker struct {
    goverseapi.BaseObject
    mu           sync.Mutex
    Queue        []QueueEntry  // Players waiting for match
    NextGameID   int64         // Auto-increment game ID
}

type QueueEntry struct {
    Username  string
    ClientID  string
    Score     int32
    QueuedAt  int64    // Unix timestamp
}
```

**Methods**:

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `JoinQueue` | `JoinQueueRequest{username, clientID, score}` | `JoinQueueResponse{position}` | Enter matchmaking queue |
| `LeaveQueue` | `LeaveQueueRequest{username}` | `LeaveQueueResponse` | Exit matchmaking queue |
| `GetQueueStatus` | `GetQueueStatusRequest{username}` | `QueueStatusResponse{position, count}` | Check queue position |

**Internal Logic**:
- When 2+ players are in queue, pairs them by closest score
- Creates a new `ChessGame` object
- Pushes `MatchFound` notification to both players
- Removes matched players from queue

---

### 3. ChessGame Object

**Object ID**: `ChessGame-{gameID}` (e.g., `ChessGame-1`, `ChessGame-42`)

**Purpose**: Manages a single chess game between two players.

**State**:
```go
type ChessGame struct {
    goverseapi.BaseObject
    mu            sync.Mutex
    GameID        string
    WhitePlayer   string    // Username
    BlackPlayer   string    // Username
    WhiteClientID string    // For push messaging
    BlackClientID string    // For push messaging
    Board         [8][8]Piece  // Chess board state
    CurrentTurn   string    // "white" or "black"
    Status        string    // "playing", "white_wins", "black_wins", "draw"
    MoveHistory   []Move    // All moves made
    CreatedAt     int64     // Unix timestamp
    FinishedAt    int64     // Unix timestamp (0 if ongoing)
}

type Piece struct {
    Type  string  // "king", "queen", "rook", "bishop", "knight", "pawn", ""
    Color string  // "white", "black", ""
}

type Move struct {
    FromRow int32
    FromCol int32
    ToRow   int32
    ToCol   int32
    Piece   string
    Player  string
}
```

**Methods**:

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `GetState` | `GetStateRequest` | `GameStateResponse` | Get current board and status |
| `MakeMove` | `MakeMoveRequest{username, from, to}` | `MakeMoveResponse{success, state}` | Execute a move |
| `OfferDraw` | `OfferDrawRequest{username}` | `OfferDrawResponse` | Propose a draw |
| `AcceptDraw` | `AcceptDrawRequest{username}` | `AcceptDrawResponse{state}` | Accept draw offer |
| `Resign` | `ResignRequest{username}` | `ResignResponse{state}` | Forfeit the game |
| `Join` | `JoinGameRequest{username, clientID}` | `JoinGameResponse{color, state}` | Join game and get assigned color |

**Game Logic**:
- Validates move legality (basic piece movement rules)
- Detects checkmate (king can be captured)
- Detects stalemate (no valid moves, not in check)
- On game end:
  - Updates both players' scores via `Player.UpdateScore`
  - Pushes `GameEnded` notification to both players

---

## Score System

Simple rating system:
- New players start at **1000** points
- Win: **+10** points
- Loss: **-10** points
- Draw: **0** points change

Future enhancement: proper ELO calculation based on opponent rating.

---

## Protocol Buffers

```protobuf
syntax = "proto3";
package chess;
option go_package = "github.com/xiaonanln/goverse/samples/chess/proto;chess_pb";

//
// Player Messages
//

message RegisterRequest {
  string password = 1;
}

message RegisterResponse {
  bool success = 1;
  string message = 2;
}

message LoginRequest {
  string password = 1;
  string client_id = 2;
}

message LoginResponse {
  bool success = 1;
  string message = 2;
  PlayerInfo player_info = 3;
}

message LogoutRequest {}

message LogoutResponse {
  bool success = 1;
}

message GetProfileRequest {}

message ProfileResponse {
  string username = 1;
  int32 score = 2;
  int32 games_played = 3;
  int32 games_won = 4;
  int32 games_lost = 5;
  int32 games_drawn = 6;
  string current_game_id = 7;
}

message PlayerInfo {
  string username = 1;
  int32 score = 2;
  string current_game_id = 3;
}

message UpdateScoreRequest {
  int32 delta = 1;  // +10 for win, -10 for loss
  string game_result = 2;  // "win", "loss", "draw"
}

message UpdateScoreResponse {
  int32 new_score = 1;
}

message SetCurrentGameRequest {
  string game_id = 1;  // Empty string to clear
}

message SetCurrentGameResponse {
  bool success = 1;
}

//
// Matchmaker Messages
//

message JoinQueueRequest {
  string username = 1;
  string client_id = 2;
  int32 score = 3;
}

message JoinQueueResponse {
  bool success = 1;
  int32 position = 2;  // Position in queue (1-based)
  string message = 3;
}

message LeaveQueueRequest {
  string username = 1;
}

message LeaveQueueResponse {
  bool success = 1;
}

message GetQueueStatusRequest {
  string username = 1;
}

message QueueStatusResponse {
  bool in_queue = 1;
  int32 position = 2;
  int32 total_waiting = 3;
}

//
// ChessGame Messages
//

message JoinGameRequest {
  string username = 1;
  string client_id = 2;
}

message JoinGameResponse {
  bool success = 1;
  string color = 2;  // "white" or "black"
  GameState state = 3;
}

message GetStateRequest {}

message GameStateResponse {
  GameState state = 1;
}

message GameState {
  string game_id = 1;
  string white_player = 2;
  string black_player = 3;
  repeated BoardRow board = 4;  // 8 rows
  string current_turn = 5;      // "white" or "black"
  string status = 6;            // "playing", "white_wins", "black_wins", "draw"
  repeated MoveRecord moves = 7;
}

message BoardRow {
  repeated PieceInfo pieces = 1;  // 8 pieces per row
}

message PieceInfo {
  string type = 1;   // "king", "queen", "rook", "bishop", "knight", "pawn", ""
  string color = 2;  // "white", "black", ""
}

message MakeMoveRequest {
  string username = 1;
  int32 from_row = 2;
  int32 from_col = 3;
  int32 to_row = 4;
  int32 to_col = 5;
}

message MakeMoveResponse {
  bool success = 1;
  string message = 2;
  GameState state = 3;
}

message MoveRecord {
  int32 from_row = 1;
  int32 from_col = 2;
  int32 to_row = 3;
  int32 to_col = 4;
  string piece_type = 5;
  string player = 6;
}

message OfferDrawRequest {
  string username = 1;
}

message OfferDrawResponse {
  bool success = 1;
  string message = 2;
}

message AcceptDrawRequest {
  string username = 1;
}

message AcceptDrawResponse {
  bool success = 1;
  GameState state = 2;
}

message ResignRequest {
  string username = 1;
}

message ResignResponse {
  bool success = 1;
  GameState state = 2;
}

//
// Push Messages (Server -> Client)
//

message MatchFoundNotification {
  string game_id = 1;
  string opponent_username = 2;
  int32 opponent_score = 3;
  string your_color = 4;  // "white" or "black"
}

message GameUpdateNotification {
  GameState state = 1;
  string message = 2;  // Description of what happened
}

message GameEndedNotification {
  string game_id = 1;
  string result = 2;       // "win", "loss", "draw"
  int32 score_change = 3;
  int32 new_score = 4;
}

message DrawOfferNotification {
  string game_id = 1;
  string from_player = 2;
}
```

---

## API Endpoints (via HTTP Gate)

### Player Endpoints

**Register new player**:
```
POST /api/v1/objects/create/Player/{username}
Request: {"request": "<base64 of RegisterRequest>"}
Response: {"id": "Player-alice"}
```

**Login**:
```
POST /api/v1/objects/call/Player/{username}/Login
Request: {"request": "<base64 of LoginRequest>"}
Response: {"response": "<base64 of LoginResponse>"}
```

**Get profile**:
```
POST /api/v1/objects/call/Player/{username}/GetProfile
Request: {"request": "<base64 of GetProfileRequest>"}
Response: {"response": "<base64 of ProfileResponse>"}
```

### Matchmaking Endpoints

**Join queue**:
```
POST /api/v1/objects/call/Matchmaker/Matchmaker/JoinQueue
Request: {"request": "<base64 of JoinQueueRequest>"}
Response: {"response": "<base64 of JoinQueueResponse>"}
```

**Leave queue**:
```
POST /api/v1/objects/call/Matchmaker/Matchmaker/LeaveQueue
Request: {"request": "<base64 of LeaveQueueRequest>"}
Response: {"response": "<base64 of LeaveQueueResponse>"}
```

### Game Endpoints

**Join game (after match found)**:
```
POST /api/v1/objects/call/ChessGame/{gameID}/Join
Request: {"request": "<base64 of JoinGameRequest>"}
Response: {"response": "<base64 of JoinGameResponse>"}
```

**Make move**:
```
POST /api/v1/objects/call/ChessGame/{gameID}/MakeMove
Request: {"request": "<base64 of MakeMoveRequest>"}
Response: {"response": "<base64 of MakeMoveResponse>"}
```

**Get game state**:
```
POST /api/v1/objects/call/ChessGame/{gameID}/GetState
Request: {"request": "<base64 of GetStateRequest>"}
Response: {"response": "<base64 of GameStateResponse>"}
```

**Resign**:
```
POST /api/v1/objects/call/ChessGame/{gameID}/Resign
Request: {"request": "<base64 of ResignRequest>"}
Response: {"response": "<base64 of ResignResponse>"}
```

---

## Client Flow

### 1. Registration Flow

```
Client                          Gate                           Node
  │                               │                               │
  │  POST /create/Player/alice    │                               │
  │  + RegisterRequest{password}  │                               │
  │ ─────────────────────────────>│                               │
  │                               │  CreateObject(Player-alice)   │
  │                               │──────────────────────────────>│
  │                               │                               │
  │                               │  Register() method called     │
  │                               │<──────────────────────────────│
  │  RegisterResponse{success}    │                               │
  │<─────────────────────────────│                               │
```

### 2. Login Flow

```
Client                          Gate                           Node
  │                               │                               │
  │  POST /call/Player/alice/Login│                               │
  │  + LoginRequest{pw, clientID} │                               │
  │ ─────────────────────────────>│                               │
  │                               │  CallObject(Player-alice, Login)
  │                               │──────────────────────────────>│
  │                               │                               │
  │                               │  LoginResponse{success, info} │
  │                               │<──────────────────────────────│
  │  LoginResponse                │                               │
  │<─────────────────────────────│                               │
```

### 3. Matchmaking Flow

```
Player A                        Matchmaker                    Player B
    │                               │                             │
    │  JoinQueue(alice, 1000)       │                             │
    │ ─────────────────────────────>│                             │
    │  JoinQueueResponse{pos: 1}    │                             │
    │<─────────────────────────────│                             │
    │                               │                             │
    │                               │  JoinQueue(bob, 1050)       │
    │                               │<────────────────────────────│
    │                               │  JoinQueueResponse{pos: 1}  │
    │                               │ ────────────────────────────>│
    │                               │                             │
    │                               │ ┌──────────────────────┐    │
    │                               │ │ Match found!         │    │
    │                               │ │ Create ChessGame-1   │    │
    │                               │ └──────────────────────┘    │
    │                               │                             │
    │  Push: MatchFoundNotification │                             │
    │<─────────────────────────────│                             │
    │                               │  Push: MatchFoundNotification
    │                               │ ────────────────────────────>│
```

### 4. Game Play Flow

```
White Player                   ChessGame-1                  Black Player
    │                               │                             │
    │  Join(alice, clientID)        │                             │
    │ ─────────────────────────────>│                             │
    │  JoinResponse{white, state}   │                             │
    │<─────────────────────────────│                             │
    │                               │  Join(bob, clientID)        │
    │                               │<────────────────────────────│
    │                               │  JoinResponse{black, state} │
    │                               │ ────────────────────────────>│
    │                               │                             │
    │  MakeMove(e2->e4)             │                             │
    │ ─────────────────────────────>│                             │
    │  MakeMoveResponse{ok, state}  │                             │
    │<─────────────────────────────│                             │
    │                               │  Push: GameUpdateNotification
    │                               │ ────────────────────────────>│
    │                               │                             │
    │                               │  MakeMove(e7->e5)           │
    │                               │<────────────────────────────│
    │  Push: GameUpdateNotification │  MakeMoveResponse{ok, state}│
    │<─────────────────────────────│ ────────────────────────────>│
    │                               │                             │
    │         ... more moves ...    │                             │
    │                               │                             │
    │                               │ ┌──────────────────────┐    │
    │                               │ │ Checkmate detected!  │    │
    │                               │ │ White wins           │    │
    │                               │ │ Update scores        │    │
    │                               │ └──────────────────────┘    │
    │                               │                             │
    │  Push: GameEndedNotification  │                             │
    │  {win, +10, 1010}             │                             │
    │<─────────────────────────────│                             │
    │                               │  Push: GameEndedNotification│
    │                               │  {loss, -10, 1040}          │
    │                               │ ────────────────────────────>│
```

---

## Project Structure

```
samples/chess/
├── DESIGN.md              # This file
├── README.md              # User documentation
├── compile-proto.sh       # Proto compilation script
├── proto/
│   ├── chess.proto        # Protocol definitions
│   └── chess.pb.go        # Generated Go code
├── server/
│   ├── main.go            # Server entry point
│   ├── player.go          # Player object implementation
│   ├── matchmaker.go      # Matchmaker object implementation
│   ├── chessgame.go       # ChessGame object implementation
│   └── chess_logic.go     # Chess rules and validation
└── client/
    └── client.go          # Interactive CLI client (optional)
```

---

## Running the Sample

### Prerequisites

- Go 1.21+
- etcd (for cluster coordination)
- Gate server (for client connections)

### Start Services

1. **Start etcd**:
   ```bash
   ./script/codespace/start-etcd.sh
   ```

2. **Start Gate server**:
   ```bash
   cd cmd/gate
   go run .
   ```

3. **Start Chess server**:
   ```bash
   cd samples/chess/server
   go run .
   ```

4. **Run CLI client** (optional):
   ```bash
   cd samples/chess/client
   go run . -user alice
   ```

### Multi-Node Deployment

```bash
# Terminal 1: etcd
./script/codespace/start-etcd.sh

# Terminal 2: Gate
cd cmd/gate && go run .

# Terminal 3: Node 1
cd samples/chess/server && go run . --node-addr=localhost:50051

# Terminal 4: Node 2
cd samples/chess/server && go run . --node-addr=localhost:50052

# Terminal 5: Node 3
cd samples/chess/server && go run . --node-addr=localhost:50053

# Terminals 6+: Clients
cd samples/chess/client && go run . -user alice
cd samples/chess/client && go run . -user bob
```

Objects will be distributed across nodes:
- `Player-alice` might be on Node 1
- `Player-bob` might be on Node 2
- `Matchmaker` might be on Node 3
- `ChessGame-1` might be on Node 1

---

## Chess Move Validation (Simplified)

For the demo, we implement basic piece movement:

| Piece | Movement |
|-------|----------|
| King | One square any direction |
| Queen | Any number of squares horizontally, vertically, or diagonally |
| Rook | Any number of squares horizontally or vertically |
| Bishop | Any number of squares diagonally |
| Knight | L-shape: 2+1 squares |
| Pawn | Forward 1 (or 2 from start), capture diagonally |

**Simplified Rules**:
- No castling
- No en passant
- Pawn auto-promotes to Queen
- Check detection: if your king can be captured next turn, you lose
- Stalemate: no valid moves and not in check = draw

---

## Implementation Plan

### Phase 1: Core Objects
1. Define and generate proto files
2. Implement `Player` object with registration/login
3. Implement basic `Matchmaker` with queue logic
4. Implement `ChessGame` with board state

### Phase 2: Game Logic
1. Implement piece movement validation
2. Implement check/checkmate detection
3. Implement score updating
4. Add draw/resign functionality

### Phase 3: Push Messaging
1. Implement `MatchFoundNotification` push
2. Implement `GameUpdateNotification` push
3. Implement `GameEndedNotification` push

### Phase 4: Client
1. Create interactive CLI client
2. Add game board display
3. Handle push notifications
4. Test full game flow

### Phase 5: Documentation
1. Write README with setup instructions
2. Add example gameplay session
3. Document all API endpoints

---

## Future Enhancements (Out of Scope)

- Full chess rules (castling, en passant, promotion choice)
- Time controls (blitz, rapid, classical)
- ELO rating algorithm
- Spectator mode
- Game history and replay
- Web UI
- Password hashing
- Persistence with PostgreSQL
- Reconnection support
- Ranked matchmaking by rating range
- Private games with invite codes
- Chat during games

---

## Learning Outcomes

After studying this sample, developers will understand:

1. **Multi-Object Coordination**: How `Matchmaker`, `Player`, and `ChessGame` objects interact
2. **Authentication Pattern**: Simple login/session management with distributed objects
3. **State Management**: Complex game state (chess board) in virtual actors
4. **Push Messaging**: Real-time notifications from server to clients
5. **Scoring Systems**: Tracking and updating player ratings
6. **Matchmaking**: Implementing a queue-based pairing system
7. **Turn-Based Games**: Managing turns and game flow

---

## License

MIT License - see the root LICENSE file.
