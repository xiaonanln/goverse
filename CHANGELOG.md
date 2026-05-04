# Changelog

All notable changes to GoVerse are documented in this file.

## [0.1.1] — 2026-05-04

### Added

- **Bomberman sample**: multiplayer game with Match/Player/MatchmakingQueue objects, web UI, and stress test (#540–#545).
- **Auth middleware** (`GateEventHandler`): pluggable auth + client-disconnect callback on `GateServerConfig`; `CallerUserID(ctx)` / `CallerRoles(ctx)` helpers in object methods (#554, #569).
- **`CallerIdentity` propagation**: identity flows gate → node → cross-node via gRPC metadata (#555, #563).
- **Gate access checks**: `CheckClientCreate` enforced on HTTP CreateObject (#549); `DeleteObjectRequest` gains required `type` field with `CheckClientDelete` at gate and type-spoof check at node (#552).
- **Stuck-shard reallocation**: leader reassigns shards whose `TargetNode` fails to claim within `stuck_shard_reallocation_timeout` (default 1 min) (#570).
- **Randomised shard assignment**: balanced (±1 per node) and non-deterministic (#572).

### Fixed

- Bomberman stress test: poll cluster-ready before launching clients instead of fixed sleep (#573).

### Changed

- `GateServerConfig.AuthValidator` → `GateServerConfig.EventHandler` (`GateEventHandler`). **Migration**: implement `GateEventHandler` and set `EventHandler` (#569).
- Sample subprocess helpers moved from `tests/samples/` to `samples/common/` (#539).

## [0.1.0] — 2026-04-24

First tagged release.

### Added

- **Core runtime**: virtual objects, Node + Gate architecture, gRPC and HTTP REST APIs.
- **Exactly-once call semantics**: `ReliableCallObject` tolerates retries and node failures.
- **Sharded placement**: 8192-shard model with dynamic rebalancing.
- **Timeouts**: configurable `DefaultCallTimeout`, `DefaultCreateTimeout`, `EtcdOperationTimeout`; `IsTimeout(err)` helper; timeout metrics.
- **Watch reconnection**: etcd watch auto-reconnects with exponential backoff.
- **Persistence**: PostgreSQL with JSONB storage.
- **Observability**: Prometheus metrics, pprof, Inspector UI, `/healthz` + `/ready` endpoints.
- **Push messaging**: real-time server-to-client delivery, `BroadcastToAllClients`.
- **Operations**: production Dockerfiles, Kubernetes manifests, `docker-compose.yml`.
- **Samples**: `counter`, `tictactoe`, `chat`, `sharding_demo`, `wallet`.

### Known Issues

- **Migration-period unavailability**: brief errors during shard handoff even though the current node is still alive.
- **Graceful shutdown**: scale-down relies on etcd lease expiry rather than proactive shard release.
- **No built-in TLS**: deploy behind a TLS-terminating proxy. Auth middleware ships in v0.1.1; native gate TLS is deferred.
