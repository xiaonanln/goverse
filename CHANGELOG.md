# Changelog

All notable changes to GoVerse are documented in this file.

## [0.1.1] — 2026-05-04

### Added

- **Bomberman sample**: new reference app showing how to build a real-time multiplayer game with GoVerse (#540–#545).
- **Auth middleware**: implement `GateEventHandler` on your gate to authenticate clients and receive disconnect events; read `CallerUserID(ctx)` / `CallerRoles(ctx)` inside any object method, on any node (#554, #569).
- **HTTP access checks now match gRPC**: `CreateObject` and `DeleteObject` via HTTP enforce the same `CheckClientCreate` / `CheckClientDelete` lifecycle rules as gRPC. `DeleteObject` now requires the object type in the request (#549, #552).
- **Self-healing stuck shards**: if a node is assigned shards but goes silent, the cluster automatically reassigns them after `stuck_shard_reallocation_timeout` (default 1 min, configurable in YAML) (#570).
- **Balanced shard distribution**: shards are spread evenly across nodes (±1) with randomisation, avoiding hot spots at startup and after rebalance (#572).

### Fixed

- Bomberman stress test no longer reports spurious errors at startup while the cluster is still warming up (#573).

### Changed

- `GateServerConfig.AuthValidator` renamed to `GateServerConfig.EventHandler` (`GateEventHandler` interface). **Migration**: implement `GateEventHandler` and set `EventHandler` instead of `AuthValidator` (#569).

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
