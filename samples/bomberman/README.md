# Bomberman Sample

A multi-player Bomberman demo showing **how a real-time game maps onto
the goverse virtual-actor model**. Each match is a goverse object that
ticks at 10 Hz and broadcasts authoritative state to its players;
matchmaking, persistent stats, and exactly-once stat updates use the
same primitives any other application would.

## Object model

| Object             | Lifetime          | Role                                                             |
| ------------------ | ----------------- | ---------------------------------------------------------------- |
| `Match`            | per-game          | 13×11 grid, 10 Hz tick loop, push-broadcasts MatchSnapshot       |
| `Player`           | per-user          | Persistent stats: total matches, wins, losses, last match id     |
| `MatchmakingQueue` | singleton         | Auto-loaded; FIFO that batches queued players into Matches       |

Cross-object hops:

- `MatchmakingQueue → Match.AddPlayer / StartMatch` uses plain
  `goverseapi.CallObject`. MatchState already rejects duplicate adds
  and double-starts at the data layer; a transient failure costs at
  most one ruined match for the affected players, no permanent state
  at risk.
- `Match → Player.RecordMatchResult` uses **`goverseapi.ReliableCallObject`**
  with a stable per-player `call_id`. This is the *only* hop that
  mutates permanent reward state, so reliable-call dedup is the
  difference between a single transient timeout being eaten silently
  and a player's win count jumping from 1 to 2. `recordResults`
  retries with exponential backoff under a 60 s budget, so transient
  failures become eventual consistency.

## Running locally

You need:

- Go 1.25+, Python 3.10+ with `grpcio`, `grpcio-tools`, `pyyaml`
- `protoc` plus `protoc-gen-go` and `protoc-gen-go-grpc` on `PATH`
- **Postgres** on `localhost:5432` (`docker compose up -d postgres etcd`
  from the repo root sets up both)
- **etcd** on `localhost:2379` (same compose file)

```bash
# 1. Generate proto artifacts (the top-level script picks up sample protos)
./script/compile-proto.sh

# 2. Initialise Postgres schema (reliable-call dedup state lives there)
go run ./cmd/pgadmin --config samples/bomberman/stress_config.yml init

# 3. Run the stress test (60 s, 8 clients, fast 5-second matches)
python3 samples/bomberman/stress_test.py --duration 60 --clients 8
```

The stress driver spins up 1 inspector + 3 server nodes + 2 gates,
launches N concurrent fake players, and runs each through repeated
JoinQueue → wait-for-match-end → JoinQueue cycles. Successful run
ends with:

```
cycles=...  rpc_errors=0  timeouts=0  exactly_once_violations=0
✅ stress-player-000: 12 cycles, server total=12 wins=0 losses=12
...
✅ bomberman stress test passed
```

## Playing in a browser

The web UI in `web/` is a canvas-based client that talks to the
goverse HTTP gate via SSE for snapshot push and POST for inputs.

```bash
# 1. Start the cluster (see Running locally above for prerequisites)
go run ./cmd/inspector --config samples/bomberman/stress_config.yml &
go run ./samples/bomberman/server --config samples/bomberman/stress_config.yml --node-id bomberman-node-1 &
go run ./samples/bomberman/server --config samples/bomberman/stress_config.yml --node-id bomberman-node-2 &
go run ./samples/bomberman/server --config samples/bomberman/stress_config.yml --node-id bomberman-node-3 &
go run ./cmd/gate     --config samples/bomberman/stress_config.yml --gate-id bomberman-gate-1 &

# 2. Serve the web/ folder; the gate exposes /api/v1 on port 8080.
python3 -m http.server 8000 --directory samples/bomberman/web
```

Open `http://localhost:8000`, enter a nickname, click **Find Match**,
then open the same URL in a second tab and queue up a second player.
Once two players are queued the cluster spawns a Match and both
canvases start receiving 10 Hz snapshot pushes. WASD or arrows to
move, Space to drop a bomb.

## Stress-test invariants

The driver asserts three properties at the end of a run; all three
are exactly-once contracts that would fail if reliable-call dedup or
MatchState's data-layer guards regressed:

1. **Exactly-once stat updates** — each JoinQueue cycle must increment
   the player's `total_matches` by exactly 1. The retry loop inside
   `Match.recordResults` keeps trying until the call lands, but the
   stable per-player `call_id` ensures that lands at most once.
2. **Cycle accounting** — across all clients, completed cycles sum to
   server-reported `total_matches`. Catches drift from missed credits.
3. **Stats arithmetic** — `total_matches == wins + losses` for every
   player. Pure data invariant, but cheap to verify.

## File map

- `proto/bomberman.proto` — every message: snapshot, RPC requests / responses, persistence blob.
- `server/match.go`, `server/match_state.go` — Match goverse object + pure game logic.
- `server/player.go` — Player goverse object with ToData/FromData persistence.
- `server/matchmaking_queue.go` — MatchmakingQueue singleton with internal spawn loop.
- `server/main.go` — registers the three object types.
- `web/index.html`, `web/style.css`, `web/game.js` — browser client.
- `stress_config.yml` — 1 inspector + 3 nodes + 2 gates; Postgres + etcd wiring.
- `BombermanServer.py` — subprocess wrapper used by the stress driver.
- `stress_test.py` — the correctness stress driver.
