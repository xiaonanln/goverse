# PR TODO

This document lists pull requests (PRs) that need to be developed for Goverse. Each PR focuses on **what** needs to be done and **why** it matters, not **how** to implement it.

PRs are organized by category and priority. Cross-references to related TODO files and design documents are included where applicable.

---

## Core System Improvements

### Reliable Calls - Complete Implementation
**What**: Complete the reliable calls implementation for exactly-once call semantics between objects.  
**Why**: Distributed systems need exactly-once guarantees to prevent duplicate processing during failures, retries, or network issues. This is critical for operations like financial transactions, inventory updates, or any stateful operation where duplicates cause data corruption.  
**Reference**: `docs/RELIABLE_CALLS_DESIGN.md` contains full architecture and PR plan.

### Configuration Hot Reload
**What**: Support runtime configuration updates without cluster restart for access control rules, lifecycle policies, and other non-structural settings.  
**Why**: Production systems cannot afford downtime for configuration changes. Operators need to adjust policies (rate limits, access rules, timeouts) dynamically as traffic patterns and security requirements evolve.  
**Reference**: Mentioned in `README.md` TODO section.

### Default Timeout Enforcement
**What**: Add default timeouts for all operations (CallObject, CreateObject, DeleteObject) when client doesn't provide context deadlines.  
**Why**: Operations without timeouts can hang indefinitely during network issues or node failures, exhausting resources and blocking user requests. Default timeouts ensure graceful degradation with clear error messages.  
**Reference**: `docs/TIMEOUT_DESIGN.md` describes the complete design.

### Runtime Shard Count Reconfiguration
**What**: Allow changing the number of shards (currently fixed at 8192) without full cluster rebuild.  
**Why**: Small clusters waste resources with 8192 shards, while very large clusters (1000+ nodes) may need more shards for better distribution. Dynamic reconfiguration enables clusters to adapt as they grow without downtime.  
**Reference**: Mentioned in `README.md` TODO and `SHARDING_TODO.md` P3.

---

## Observability & Monitoring

### Shard Operation Latency Metrics
**What**: Add Prometheus histograms tracking durations of shard claim, release, and migration operations.  
**Why**: Existing metrics only track counts, not performance. Without latency metrics, operators cannot detect slow shard operations that delay object access or identify performance regressions.  
**Reference**: `SHARDING_TODO.md` P1.

### Stuck Shard Migration Detection
**What**: Add metrics and alerting for shards stuck in migration state (TargetNode != CurrentNode) beyond a configurable timeout.  
**Why**: While leader reassigns shards from dead nodes, there's no visibility into migration progress. Stuck migrations block object access on those shards. Early detection enables faster remediation.  
**Reference**: `SHARDING_TODO.md` P1.

### Shard Lock Contention Metrics
**What**: Add instrumentation to shard locks tracking acquisition attempts, wait times, and currently held write locks.  
**Why**: Shard lock contention is invisible today. High contention during shard transitions causes slow object operations. Metrics enable identifying contention hotspots and tuning lock behavior.  
**Reference**: `SHARDING_TODO.md` P1.

### Per-Shard Object Count Metrics
**What**: Add gauge showing how many objects each shard holds, and visualize in Inspector UI.  
**Why**: Hot shards with many objects or high call rates cause imbalanced load. Without per-shard metrics, operators cannot identify which shards need rebalancing or which nodes are overloaded.  
**Reference**: `SHARDING_TODO.md` P1.

### Rebalance Decision Metrics
**What**: Add counters and gauges tracking why rebalancing happens or is skipped (balanced, not stable, not all assigned), plus current imbalance ratios.  
**Why**: Rebalance decisions are only logged at debug level. Operators cannot tell why rebalancing isn't happening when they expect it to, or whether the cluster is actually balanced.  
**Reference**: `SHARDING_TODO.md` P1.

### Inspector Shard Distribution Visualization
**What**: Add dedicated view in Inspector UI showing shard-to-node mapping, migration state, per-shard object counts, and call rate heatmaps.  
**Why**: Operators need visual tools to understand shard distribution at a glance. Text logs and metrics don't show the big picture. Heatmaps reveal hot shards and migration bottlenecks instantly.  
**Reference**: `SHARDING_TODO.md` P3 and `README.md` TODO section.

### Enhanced Metrics & Alerting
**What**: Add more granular Prometheus metrics beyond current basic counters (object counts, method calls). Include SLO tracking for latency percentiles, error rates, and availability.  
**Why**: Production systems need detailed metrics for capacity planning and incident response. Current metrics cover existence but not performance or reliability SLOs.  
**Reference**: `README.md` TODO section.

---

## Security & Access Control

### Object Access Control Implementation
**What**: Implement config-based access control rules validating object IDs and method calls before execution.  
**Why**: Currently, any client can create objects with arbitrary IDs and call any method, enabling resource exhaustion attacks and unauthorized access to internal methods. Access control is essential for production deployments.  
**Reference**: `docs/design/OBJECT_ACCESS_CONTROL.md` contains full design.

### Gate Authorization Mechanism
**What**: Add authentication and fine-grained authorization for client connections to gates.  
**Why**: Production gates are exposed to the internet. Without authentication, attackers can create objects, call methods, and exhaust resources. Authorization ensures only authorized clients access specific objects.  
**Reference**: `README.md` TODO section.

### TLS/mTLS Support for gRPC
**What**: Add TLS configuration for all gRPC connections (node-to-node, gate-to-node, etcd client).  
**Why**: All gRPC traffic is currently unencrypted. Production deployments, especially multi-tenant or cross-region, require encryption to protect sensitive data and prevent man-in-the-middle attacks.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

---

## Performance & Scalability

### Load-Aware Shard Rebalancing
**What**: Factor actual node load (object count, method call rate, CPU, memory) into rebalance decisions, not just shard count.  
**Why**: Current rebalancing treats all shards equally. A node with 100 shards holding 1 object each is rebalanced the same as 100 shards with 1000 objects each. Load-aware rebalancing prevents hot nodes.  
**Reference**: `SHARDING_TODO.md` P2 and `README.md` TODO section.

### Shard Lock Timeout
**What**: Add context-aware lock acquisition with timeout and maximum lock hold duration.  
**Why**: Shard lock acquisition has no timeout. If a goroutine holding a write lock hangs (e.g., stuck etcd write), all operations on that shard block indefinitely. Timeouts enable detection and recovery.  
**Reference**: `SHARDING_TODO.md` P2.

### Consistent Hashing for Shard Assignment
**What**: Evaluate and optionally implement consistent hashing (jump hash, rendezvous hashing) for shard-to-node assignment instead of round-robin.  
**Why**: Round-robin assignment causes all shard mappings to change when nodes join/leave. Consistent hashing minimizes shard movement, reducing migration overhead and downtime.  
**Reference**: `SHARDING_TODO.md` P3.

### Weighted Node Capacity
**What**: Add configurable node weight/capacity for heterogeneous clusters, factoring into shard assignment and rebalancing.  
**Why**: All nodes are treated equally today. Heterogeneous clusters (different CPU/memory per node) get equal shard counts regardless of actual capacity, causing imbalanced load.  
**Reference**: `SHARDING_TODO.md` P3.

---

## Gate & Client Features

### Gate Rate Limiting
**What**: Add per-client and per-object rate limiting in gates to prevent abuse.  
**Why**: Without rate limiting, a single misbehaving client can overwhelm the cluster with requests, causing resource exhaustion and degraded service for other clients.  
**Reference**: `README.md` TODO section.

### Client Reconnection & Backoff
**What**: Add automatic retry logic with exponential backoff for client connections to gates.  
**Why**: Clients currently need to implement their own reconnection logic. Transient network failures or gate restarts break client connections. Built-in reconnection improves client reliability.  
**Reference**: `README.md` TODO section.

### Client Push Message Reliability
**What**: Add delivery confirmation and retry logic for PushMessageToClient.  
**Why**: Current push messaging is best-effort using buffered channels. If client disconnects or channel is full, messages are silently dropped. Critical notifications need guaranteed delivery.  
**Reference**: `docs/design/CLIENT_PUSH_OPTIMIZATION.md` discusses optimization but not reliability.

---

## Kubernetes & Production Operations

### Add .dockerignore
**What**: Create `.dockerignore` file excluding `.git`, `.github`, `docs`, `examples`, `samples`, `tests`, and other non-essential files from Docker build context.  
**Why**: Docker builds currently send the entire repository as build context including git history and test files, slowing down builds and increasing image layer sizes unnecessarily.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P0.

### Add SecurityContext to K8s Manifests
**What**: Add `securityContext` to all pod specs enforcing non-root execution, read-only filesystem, dropped capabilities, and seccomp profiles.  
**Why**: Dockerfiles create non-root users but K8s manifests don't enforce security constraints. Security hardening prevents container escapes and reduces attack surface.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P1.

### Prometheus ServiceMonitor & Alerts
**What**: Create ServiceMonitor CRDs for Prometheus Operator and PrometheusRule with basic alerts (node down, high error rate, stuck migrations).  
**Why**: Metrics endpoints exist but no ServiceMonitor CRDs. Prometheus Operator deployments cannot scrape metrics without ServiceMonitors. Alerts enable proactive incident response.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P1.

### Environment Variable Configuration Override
**What**: Add direct environment variable override support for key configuration values (etcd endpoints, postgres password, node ID, POD_IP).  
**Why**: K8s deployments inject config via env vars. Current workaround passes env vars as flag values in container command. Direct env var support simplifies manifests and follows K8s conventions.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P1 and `SHARDING_TODO.md` P2.

### PodDisruptionBudgets
**What**: Create PodDisruptionBudgets for nodes, gates, and etcd ensuring minimum availability during cluster operations (node drains, upgrades).  
**Why**: Without PDBs, cluster operations can drain all nodes/gates simultaneously, causing complete service outage. PDBs ensure safe maintenance windows.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P1.

### JSON Logging Format
**What**: Add `LOG_FORMAT=json` and `LOG_LEVEL` environment variable support to logger.  
**Why**: Text-format logs don't integrate well with log aggregation systems (Fluentd, Loki, CloudWatch). JSON logs enable structured querying and better observability in production.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

### Helm Chart
**What**: Create Helm chart with parameterized replica counts, resource limits, image tags, and optional components (inspector, persistence).  
**Why**: Raw YAML manifests are inflexible. Helm simplifies parameterization, upgrades, rollbacks, and GitOps workflows, making production deployments more maintainable.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

### NetworkPolicy Manifests
**What**: Create NetworkPolicy resources for namespace isolation: default deny-all, then allow node-to-node, gate-to-node, node/gate-to-etcd, and node-to-postgres.  
**Why**: No network segmentation exists today. NetworkPolicies reduce attack surface by restricting lateral movement and preventing unauthorized access to cluster components.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

### GOMAXPROCS and GOMEMLIMIT Support
**What**: Add `github.com/uber-go/automaxprocs` for automatic CPU limit detection and document setting `GOMEMLIMIT` relative to K8s memory limits.  
**Why**: Go uses all CPUs by default, ignoring K8s CPU limits, causing CPU throttling and quota exceeded errors. Memory limits also need alignment to prevent OOMKills.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

### Multi-Architecture Docker Images
**What**: Create GitHub Actions workflow building and pushing production images for amd64 and arm64 on tagged releases.  
**Why**: ARM64 adoption is growing (AWS Graviton, Apple Silicon). Multi-arch images enable broader deployment without separate build pipelines.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P2.

### Grafana Dashboards
**What**: Create and export Grafana dashboards for cluster overview (nodes, gates, shards), method call latency/errors, and shard distribution.  
**Why**: Metrics without dashboards are hard to interpret. Pre-built dashboards accelerate time-to-value for new deployments and provide operational best practices.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### OpenTelemetry Distributed Tracing
**What**: Instrument gRPC calls with OpenTelemetry, propagate trace context across node-to-node and gate-to-node calls, export to Jaeger/Tempo.  
**Why**: Multi-hop calls across nodes are hard to debug with logs alone. Distributed tracing shows end-to-end latency breakdown and helps identify bottlenecks.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### Backup Automation
**What**: Add CronJobs for automated etcd snapshots and PostgreSQL dumps, plus documented restore procedures.  
**Why**: Manual backups are error-prone and forgotten. Automated backups with tested restore procedures ensure disaster recovery readiness.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### HorizontalPodAutoscaler for Gates and Nodes
**What**: Add HPA definitions for gates (based on connection count) and nodes (based on CPU/memory or custom metrics like object count).  
**Why**: Manual scaling doesn't respond to load spikes. Autoscaling ensures efficient resource utilization and availability during traffic bursts. Node HPA requires coordination with shard rebalancing.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### Kubernetes Integration Tests
**What**: Add CI job using kind (Kubernetes in Docker) to test cluster bootstrap, node scaling, pod restart recovery, and gate scaling.  
**Why**: Current tests use in-process clusters. Real K8s deployments have different failure modes (pod evictions, DNS delays, volume mounts). Integration tests catch production issues early.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### Chaos Engineering Tests
**What**: Add pod failure injection and network partition tests, documenting expected behavior and recovery times.  
**Why**: Production systems need validated resilience. Chaos tests verify fault tolerance claims and reveal edge cases that unit tests miss.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

### Goverse Operator (Custom Resource Definition)
**What**: Create K8s operator with GoverseCluster CRD, automated shard rebalancing on scale events, and intelligent scaling based on object distribution.  
**Why**: Manual cluster management is complex and error-prone. An operator automates operational tasks, making Goverse a true cloud-native platform.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3 (long-term).

---

## Operational Features

### Graceful Shutdown Mode Configuration
**What**: Add shutdown mode parameter distinguishing temporary shutdown (restart/upgrade, keep shard assignments) from permanent shutdown (scale-down, release shards).  
**Why**: Node shutdown currently keeps shard assignments in etcd, optimizing for restart. During scale-down, proactively releasing shards reduces recovery time for remaining nodes.  
**Reference**: `SHARDING_TODO.md` P1.

### Node Readiness Endpoint with Shard Status
**What**: Add `/ready` endpoint checking etcd connection, cluster state loaded, and shard claim status (claimed N/M targeted shards).  
**Why**: K8s readiness probes fail without `/ready` endpoint. A node that hasn't claimed its shards yet should not receive traffic, preventing failed object calls.  
**Reference**: `SHARDING_TODO.md` P2 and `KUBERNETES_DEPLOYMENT_TODO.md` P0 (already done per K8s TODO).

### Shard Pinning API
**What**: Add public API `PinShard(shardID)` and `UnpinShard(shardID)`, expose in Inspector UI and via gRPC/HTTP endpoint.  
**Why**: Shards can be pinned via etcd `f=pinned` flag, but requires direct etcd manipulation. Public API enables operational workflows like freezing hot shards before maintenance.  
**Reference**: `SHARDING_TODO.md` P3.

### Kustomize Overlays
**What**: Refactor `k8s/` manifests into `deploy/kubernetes/base/` and create overlays for dev (single replica, minimal resources), staging, and production.  
**Why**: Single manifest set doesn't adapt to different environments. Kustomize overlays enable environment-specific customization without duplicating entire manifests.  
**Reference**: `KUBERNETES_DEPLOYMENT_TODO.md` P3.

---

## Testing & Quality

### Shard Migration Under Load Integration Tests
**What**: Add tests for object calls during shard migration, concurrent migrations across node pairs, and cascading failures (node fails mid-migration, then target node fails).  
**Why**: Current tests validate basic migration but not behavior under load or complex failure scenarios. Integration tests ensure migrations don't lose data or block access.  
**Reference**: `SHARDING_TODO.md` Testing Gaps.

### Rebalancing Integration Tests
**What**: Add multi-node cluster tests triggering rebalancing, verifying pinned shards stay put, and validating behavior after node scale-up/scale-down.  
**Why**: Rebalancing is only unit tested on the algorithm. Integration tests ensure rebalancing works end-to-end in real clusters and doesn't violate pin constraints.  
**Reference**: `SHARDING_TODO.md` Testing Gaps.

### Shard Lock Stress Tests
**What**: Add tests for concurrent read/write lock acquisition under high contention, verify sorted ordering prevents deadlocks, benchmark latency at varying contention levels.  
**Why**: Shard locks are critical for correctness. Stress tests ensure lock implementation is deadlock-free and performant under concurrent access.  
**Reference**: `SHARDING_TODO.md` Testing Gaps.

---

## Documentation

### Update API Documentation
**What**: Ensure all public APIs in `goverseapi` have complete godoc comments with examples.  
**Why**: External users rely on API documentation. Incomplete docs increase time-to-productivity and support burden.

### Add Migration Guide for Breaking Changes
**What**: Document migration path when reliable calls, access control, or shard count changes require cluster rebuild or data migration.  
**Why**: Users need clear upgrade paths to adopt new features. Breaking changes without migration guides block adoption.

### Production Deployment Best Practices
**What**: Create guide covering resource sizing, monitoring setup, backup strategies, upgrade procedures, and troubleshooting common issues.  
**Why**: New users struggle with production deployments. Best practices guide reduces trial-and-error and prevents common misconfigurations.

---

## References

- **Sharding**: `SHARDING_TODO.md`
- **Kubernetes Deployment**: `KUBERNETES_DEPLOYMENT_TODO.md`  
- **Roadmap**: `README.md` TODO section
- **Design Documents**: `docs/design/`, `docs/RELIABLE_CALLS_DESIGN.md`, `docs/TIMEOUT_DESIGN.md`
