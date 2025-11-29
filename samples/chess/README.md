# Chess Sample

A distributed online chess game demonstrating the Goverse virtual actor model with player registration, matchmaking, and real-time gameplay.

## Features

- **Player Registration & Login**: Create accounts and authenticate
- **Matchmaking System**: Queue-based player pairing by rating
- **Two-Player Chess**: Standard chess rules with move validation
- **Scoring System**: Win/lose affects player rating (+10/-10)
- **Real-time Updates**: Push notifications for game events

## Quick Start

See [DESIGN.md](DESIGN.md) for the complete design document.

### Prerequisites

- Go 1.21+
- etcd (for cluster coordination)
- Gate server (for client connections)

### Running the Demo

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

4. **Run clients**:
   ```bash
   # Terminal 1: Alice
   cd samples/chess/client
   go run . -user alice

   # Terminal 2: Bob
   cd samples/chess/client
   go run . -user bob
   ```

## Architecture

```
┌─────────────────┐     gRPC Stream     ┌─────────────────┐     gRPC      ┌─────────────────┐
│   Chess Client  │ ─────────────────>  │      Gate       │ ────────────> │  Goverse Node   │
│   (CLI)         │ <───────────────── │   (:48000)      │ <──────────── │  (Chess Objects)│
└─────────────────┘   Push Messages     └─────────────────┘               └─────────────────┘
```

## Objects

- **Player**: User account with credentials, rating, and game history
- **Matchmaker**: Singleton that pairs players and creates games
- **ChessGame**: Individual chess match with board state and moves

## Game Flow

1. **Register**: Create a player account with username and password
2. **Login**: Authenticate and receive client ID for push messages
3. **Join Queue**: Enter matchmaking queue with your rating
4. **Match Found**: Receive notification when paired with opponent
5. **Play Game**: Make moves, receive opponent's moves via push
6. **Game Ends**: Score updated based on win/loss/draw

## Scoring

- Starting rating: 1000
- Win: +10 points
- Loss: -10 points
- Draw: 0 points

## Project Structure

```
samples/chess/
├── DESIGN.md           # Design document
├── README.md           # This file
├── compile-proto.sh    # Proto compilation script
├── proto/
│   └── chess.proto     # Protocol definitions
├── server/
│   ├── main.go         # Server entry point
│   ├── player.go       # Player object
│   ├── matchmaker.go   # Matchmaker object
│   └── chessgame.go    # ChessGame object
└── client/
    └── client.go       # CLI client
```

## License

MIT License - see the root LICENSE file.
