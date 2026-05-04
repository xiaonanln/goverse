# Changelog

All notable changes to GoVerse are documented in this file.

## [0.1.1] — 2026-05-04

### Added

- **Bomberman sample**: full 2–4 player multiplayer game demonstrating
  push-based tick broadcasts, cross-object `ReliableCallObject`, a
  `MatchmakingQueue` singleton, and a web UI with real-time state sync.
  Includes a stress test that drives concurrent JoinQueue→match→rejoin
  cycles and verifies exactly-once `RecordMatchResult` dedup (#540–#545).
- **Pluggable auth middleware** (`GateEventHandler`): a single callback
  on `GateServerConfig` that handles both authentication and
  client-disconnect events. `CallerUserID(ctx)` / `CallerRoles(ctx)` /
  `CallerHasRole(ctx)` helpers available inside object methods (#554,
  #569).
- **`CallerIdentity` propagation**: authenticated identity flows from
  gate → node → cross-node via gRPC metadata (`x-caller-user-id` /
  `x-caller-roles`) so object methods on any node can read the caller
  (#555, #563).
- **`CheckClientCreate` on HTTP**: the HTTP `CreateObject` handler now
  runs `LifecycleValidator.CheckClientCreate` before forwarding,
  matching the behaviour of the gRPC path (#549).
- **`CheckClientDelete` with typed request**: `DeleteObjectRequest`
  (gate-facing and inter-node) gains a required `type` field. The gate
  runs `CheckClientDelete(type, id)` before forwarding; the node
  verifies the supplied type matches the registered type to close a
  type-spoof gap. HTTP path is `POST /api/v1/objects/delete/{type}/{id}`
  (#552).
- **Stuck-shard reallocation**: the consensus leader now monitors shards
  whose `TargetNode` is alive but has not claimed ownership within a
  configurable timeout (default 1 min, `stuck_shard_reallocation_timeout`
  in YAML) and reassigns them, preventing permanently stuck shards after
  partial node failures (#570).
- **Randomised shard assignment**: shard-to-node assignment is now
  balanced (±1 per node) and non-deterministic, avoiding hot-spot
  patterns on startup or rebalance (#572).
- **Chat sample authentication**: demonstrates a concrete
  `GateEventHandler` implementation with username/password header
  validation (#560).

### Fixed

- **Bomberman stress test startup errors**: the stress test now polls
  `Player.GetStats` through the gate until the cluster accepts calls
  before launching clients, replacing the fixed 2 s sleep and eliminating
  spurious `rpc_errors` caused by unclaimed shards at startup (#573).

### Changed

- `GateServerConfig.AuthValidator` replaced by
  `GateServerConfig.EventHandler` (`GateEventHandler` interface). The new
  interface covers both auth validation (returning `*CallerIdentity`) and
  `OnClientDisconnect` events in one place. **Migration**: implement
  `GateEventHandler` and set `EventHandler` instead of `AuthValidator`
  (#569).
- Shared sample server subprocess helpers moved from `tests/samples/` to
  `samples/common/` for reuse across all sample stress tests (#539).

## [0.1.0] — 2026-04-24

First tagged release. GoVerse is a distributed object runtime for Go that
implements the virtual actor model with etcd-based placement and 8192-shard
sharding. External users can deploy GoVerse, run a distributed application,
and trust that basic failure modes are handled correctly.

### Added

- **Core runtime**: virtual objects with automatic lifecycle & activation;
  Node + Gate architecture; streaming gRPC and HTTP REST APIs.
- **Exactly-once call semantics**: `ReliableCallObject` for inter-object
  calls that tolerate retries and node failures.
- **Sharded placement**: 8192-shard model with dynamic object & shard
  rebalancing across nodes.
- **Default timeouts**: `DefaultCallTimeout`, `DefaultCreateTimeout`,
  `EtcdOperationTimeout`, and `ConnectionTimeout` on `ServerConfig` /
  `GateServerConfig` / `ClusterConfig`, configurable via YAML.
- **Timeout observability**: `TimeoutError` type with `IsTimeout(err)`
  helper (gRPC-aware); `goverse_operation_timeouts_total` and
  `goverse_operation_duration_seconds` metrics.
- **Watch reconnection**: etcd watch auto-reconnects with exponential
  backoff after disconnects or compaction, preventing stale cluster state.
- **Persistence**: PostgreSQL with JSONB storage.
- **Observability**: Prometheus metrics, pprof profiling, Inspector UI.
- **Push messaging**: real-time server-to-client delivery, including
  `BroadcastToAllClients`.
- **Operations**: `/healthz` and `/ready` endpoints on node and inspector;
  production Dockerfiles for node, gate, and inspector; reference
  Kubernetes manifests under `k8s/`; `docker-compose.yml` for local etcd
  and Postgres.
- **Docs**: end-to-end "5-Minute Tour" in `docs/GET_STARTED.md`; full
  getting-started guide, API reference, HTTP gate spec, and design docs.
- **Samples**: `counter`, `tictactoe`, `chat`, `sharding_demo`, and
  `wallet` (end-to-end reliable-call demo with a stress test that
  injects mid-flight timeout aborts and verifies per-wallet balance
  conservation).

### Known Issues (deferred to v0.2.0)

- **Migration-period unavailability**: during shard handoff,
  `GetCurrentNodeForObject` errors even though the current node is still
  alive — causes brief unavailability during rebalance.
- **Graceful shutdown**: scale-down relies on etcd lease expiry rather
  than a proactive shard release.
- **No built-in access control / TLS**: deploy in a private network or
  behind a service mesh until v0.2.0.
