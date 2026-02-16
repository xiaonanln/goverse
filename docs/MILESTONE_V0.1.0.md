# Milestone v0.1.0 — "Production-Ready Core"

**Goal**: A stable release where an external user can deploy GoVerse on Kubernetes, run a distributed application, and trust it won't hang or silently fail under normal operating conditions.

**Target**: When all items below are complete.

---

## Scope

### ✅ Already Done (P0 Blockers)

These were prerequisites — now complete:

- [x] Health/readiness endpoints (`/healthz`, `/ready`) on node server
- [x] `/ready` endpoint on inspector
- [x] Production Node Dockerfile (`docker/Dockerfile.node`)
- [x] `.dockerignore` to keep Docker builds clean

### 🔧 Must Do

#### 1. Default Timeout Enforcement (P1)

**Why**: Without default timeouts, any network hiccup or unresponsive node causes indefinite hangs. This is the single biggest gap for production use.

**Scope**:
- Add configurable default timeouts to `ServerConfig` / `GateServerConfig` / `ClusterConfig`:
  - `DefaultCallTimeout` (default: 30s) — `CallObject` operations
  - `DefaultCreateTimeout` (default: 10s) — `CreateObject` operations
  - `EtcdOperationTimeout` (default: 60s) — all etcd reads/writes/watches
  - `ConnectionTimeout` (default: 30s) — gRPC dial operations
- Wrap public API entry points: if context has no deadline, apply default timeout
- Create `TimeoutError` type with `IsTimeout(err)` helper
- Add metrics: `goverse_operation_timeouts_total`, `goverse_operation_duration_seconds`

**Reference**: `docs/TIMEOUT_DESIGN.md`

#### 2. Graceful Shutdown Mode (P1)

**Why**: K8s rolling updates and scale-down behave differently. Restart should keep shard assignments for fast reclaim; scale-down should release shards proactively.

**Scope**:
- Add shutdown mode parameter: `graceful` (release shards) vs `fast` (keep assignments)
- For graceful mode: release shards before unregistering, with configurable timeout
- Block new object creation during graceful shutdown
- Document when to use each mode (rolling updates → fast, scale-down → graceful)

**Reference**: `SHARDING_TODO.md` P1

#### 3. K8s SecurityContext (P1)

**Why**: Dockerfiles create non-root users but K8s manifests don't enforce it. Basic security hygiene.

**Scope**:
- Add `securityContext` to all pod specs:
  - `runAsNonRoot: true`, `runAsUser: 1000`, `runAsGroup: 1000`
  - `readOnlyRootFilesystem: true`
  - `allowPrivilegeEscalation: false`
  - `capabilities.drop: ["ALL"]`
- Add seccomp profile where supported

#### 4. PodDisruptionBudgets (P1)

**Why**: Without PDBs, `kubectl drain` can take down all nodes/gates at once.

**Scope**:
- Create PDBs for nodes, gates, and etcd
- `minAvailable: 1` for gates and etcd, appropriate values for nodes based on shard coverage

#### 5. Getting Started Guide

**Why**: No amount of code quality matters if people can't figure out how to use it. Currently README has a quick start, but no end-to-end tutorial.

**Scope**:
- Step-by-step guide: prerequisites → local setup → define an object → run node + gate → call via HTTP → observe in Inspector
- Cover both local (docker-compose) and K8s deployment paths
- Include a complete sample app (more substantial than current examples)

---

### 📋 Known Issues (Deferred)

#### Migration-Period Unavailability
During shard migration (TargetNode ≠ CurrentNode), `GetCurrentNodeForObject` returns an error even though CurrentNode is still alive and holding the data. This means every rebalance causes brief unavailability for affected shards. The fix is to route to CurrentNode during migration instead of failing, but this requires careful handling of the handoff race. Tracked for a future release.

### 🚫 Explicitly Out of Scope

These are important but deferred to future milestones:

| Feature | Why Defer |
|---------|-----------|
| Access Control / Auth | Workaround: deploy in private network |
| TLS/mTLS | Workaround: service mesh or private network |
| Helm Chart | Raw manifests + Kustomize work for now |
| Load-aware rebalancing | Count-based rebalancing is sufficient at this scale |
| Runtime shard reconfiguration | 8192 shards handles most use cases |
| Consistent hashing | Round-robin is fine for initial deployments |
| K8s Operator / CRD | Too complex for v0.1.0 |
| OpenTelemetry tracing | Prometheus metrics + logs are enough to start |
| Chaos engineering | Manual testing sufficient for now |
| Alternative storage backends | PostgreSQL is the supported backend |

---

## Execution Order

| # | Item | Est. Complexity | Dependencies |
|---|------|-----------------|--------------|
| 1 | Default Timeout Enforcement | Large | None |
| 2 | Graceful Shutdown Mode | Medium | None |
| 3 | SecurityContext + PDB | Small | None |
| 4 | Getting Started Guide + Sample App | Medium | 1, 2 (need stable behavior to document) |
| 5 | Tag v0.1.0 | — | All above |

---

## Release Criteria

Before tagging v0.1.0:

- [ ] All "Must Do" items complete and merged
- [ ] CI passes (all existing tests + any new tests)
- [ ] Getting Started guide tested end-to-end on a fresh machine
- [ ] README updated to reflect v0.1.0 status (remove or update "EARLY DEVELOPMENT" warning)
- [ ] CHANGELOG.md updated with v0.1.0 entry
- [ ] No known P0 or P1 bugs open

---

## What v0.1.0 Means

- **Stable enough to try**: External users can deploy and experiment
- **APIs may still change**: But core concepts (objects, nodes, gates) are settled
- **Not battle-tested**: Production use at your own risk, but basic failure modes are handled
- **Foundation for v0.2.0**: Access control, TLS, Helm, and observability improvements come next
