# Changelog

All notable changes to GoVerse are documented in this file.

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
  `wallet` (demonstrates exactly-once reliable calls end-to-end; its
  stress test injects mid-flight timeout aborts and validates per-wallet
  conservation under ~1.7k calls/sec with 100 clients).
- **Python client**: `reliable_call_object` / `reliable_call_object_any`
  wrappers, `generate_call_id` helper matching the Go client's 24-char
  base64url format, and a `ReliableCallError` exception that surfaces the
  server-reported status for retry decisions.
- **SSE broadcast efficiency**: Inspector pre-formats graph events once
  and fans the bytes out to all subscribers instead of re-marshalling
  per client.
- **Operator-friendly log levels**: per-RPC chatter demoted to Debug;
  inspector, gate, and Postgres output routed through the goverse logger;
  expected shutdown paths (gate stream close, cluster "no auto-load
  configured") no longer surface as Warn.

### Known Issues (deferred to v0.2.0)

- **Migration-period unavailability**: during shard handoff,
  `GetCurrentNodeForObject` errors even though the current node is still
  alive — causes brief unavailability during rebalance.
- **Graceful shutdown**: scale-down relies on etcd lease expiry rather
  than a proactive shard release.
- **No built-in access control / TLS**: deploy in a private network or
  behind a service mesh until v0.2.0.
