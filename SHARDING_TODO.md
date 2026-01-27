# Sharding TODO

Tasks for improving the sharding system in Goverse, sorted by priority.

**Current state**: Sharding works end-to-end with 8192 fixed shards, FNV-1a hashing, etcd-backed shard mapping, shard locking, basic rebalancing, and shard migration. Metrics cover shard counts, claims, releases, migrations, and per-shard method calls.

---

## P0 — Correctness & Reliability

(No critical correctness issues identified)

---

## P1 — Observability

### Add optional shard release for permanent shutdown

**Problem**: `Cluster.Stop()` does not distinguish between temporary shutdown (restart/upgrade) and permanent shutdown (scale-down). For temporary shutdowns, keeping shard assignments in etcd allows fast reclaim on restart. For permanent shutdowns, proactively releasing shards would reduce recovery time. Currently all shutdowns rely on leader detection and reassignment.

- [ ] Add a shutdown mode parameter: `graceful` (release shards) vs `fast` (keep assignments for restart)
- [ ] For graceful shutdown, release shards before unregistering, with configurable timeout
- [ ] Ensure no new objects can be created on this node during graceful shutdown
- [ ] Document when to use each shutdown mode (K8s rolling updates use fast, scale-down uses graceful)

### Detect and monitor stuck shard migrations

**Problem**: While the leader automatically reassigns shards from dead nodes, there is no visibility into how long shards spend in migration state or alerting when migrations take abnormally long. This makes it difficult to detect slow migrations or debug migration issues.

- [ ] Add a metric tracking how long each shard has been in migration state (`TargetNode != CurrentNode`)
- [ ] Add a configurable timeout after which stuck migrations are logged at error level
- [ ] Add histogram tracking migration duration distribution across all shards

### Add shard operation latency metrics

**Problem**: Existing metrics track counts (claims, releases, migrations) but not durations. There is no way to observe how long shard claim or release operations take, making performance debugging difficult.

- [ ] Add histogram for shard claim duration (time to acquire locks + write to etcd)
- [ ] Add histogram for shard release duration
- [ ] Add histogram for shard migration end-to-end duration (from `TargetNode` change to `CurrentNode` matching)

### Add shard lock contention metrics

**Problem**: The shard lock (`cluster/shardlock/`) has no instrumentation. Lock contention during shard transitions is invisible.

- [ ] Add counter for shard lock acquisition attempts (read vs write)
- [ ] Add histogram for shard lock wait time
- [ ] Add gauge for currently held write locks

### Add per-shard object count metric

**Problem**: `goverse_objects_total` includes a `shard` label, but there is no aggregate gauge showing how many objects each shard holds. This makes it hard to identify hot shards.

- [ ] Add a gauge or periodic summary of object count per shard
- [ ] Consider adding this to the Inspector UI for visual shard load analysis

### Add rebalance decision metrics

**Problem**: When rebalancing is skipped (balanced, not all assigned, not stable), the reason is only logged at debug level. Operators cannot easily determine why rebalancing is or is not happening.

- [ ] Add counter for rebalance triggers (attempted, skipped with reason label)
- [ ] Add gauge for current max/min shard counts and imbalance ratio
- [ ] Add gauge for pinned shard count

---

## P2 — Feature Improvements

### Load-aware shard rebalancing

**Problem**: Rebalancing only considers shard count per node. All shards are treated as equal load. A node with 100 shards each holding 1 object is treated the same as a node with 100 shards each holding 1000 objects.

Referenced in README.md roadmap: "Shard rebalancing based on actual node load."

- [ ] Track per-shard object count and method call rate
- [ ] Factor object count or call rate into rebalance decisions alongside shard count
- [ ] Consider CPU and memory usage as rebalancing signals (requires node-reported metrics)
- [ ] Make the rebalancing strategy configurable (count-based vs load-based)

### Environment variable overrides for shard configuration

**Problem**: Shard configuration (`NumShards`, `RebalanceShardsBatchSize`, `ImbalanceThreshold`, etc.) can only be set via config file or code. Kubernetes deployments typically inject config via environment variables.

Referenced in KUBERNETES_DEPLOYMENT_TODO.md P1.

- [ ] Support env var overrides: `GOVERSE_NUM_SHARDS`, `GOVERSE_REBALANCE_BATCH_SIZE`, `GOVERSE_IMBALANCE_THRESHOLD`, `GOVERSE_STABILITY_DURATION`
- [ ] Document the env var to config mapping

### Node readiness endpoint with shard status

**Problem**: The node server has no `/ready` endpoint. Kubernetes readiness probes fail. Shard ownership status should be part of readiness: a node that hasn't claimed any shards yet should not receive traffic.

Referenced in KUBERNETES_DEPLOYMENT_TODO.md P0.

- [ ] Add `/ready` endpoint that checks: etcd connected, cluster state loaded, at least some shards claimed
- [ ] Include shard claim ratio in readiness response (e.g., claimed 2048/2048 targeted shards)

### Shard lock timeout

**Problem**: Shard lock acquisition has no timeout. If a goroutine holding a write lock hangs (e.g., stuck etcd write), all object operations on that shard block indefinitely.

- [ ] Add context-aware lock acquisition that respects cancellation/deadlines
- [ ] Add a maximum lock hold duration with forced release and error logging
- [ ] Log warnings when lock acquisition exceeds a configurable threshold

---

## P3 — Scalability & Advanced Features

### Runtime shard count reconfiguration

**Problem**: `NumShards` is fixed at 8192 and set at cluster creation. Changing it requires a full cluster rebuild. For clusters that grow significantly, 8192 shards may become a bottleneck or too coarse; for small clusters, 8192 may be excessive.

Referenced in README.md roadmap: "Runtime shard count reconfiguration."

- [ ] Design a shard split/merge protocol that can change shard count without downtime
- [ ] Handle the mapping of old shard IDs to new shard IDs during transition
- [ ] Ensure object ID to shard mapping remains consistent during reconfiguration
- [ ] Consider supporting power-of-2 shard counts only to simplify split/merge

### Consistent hashing for shard-to-node assignment

**Problem**: Initial shard assignment uses round-robin over sorted node addresses. When nodes are added or removed, all shard assignments can change. Consistent hashing would minimize shard movement.

- [ ] Evaluate consistent hashing (e.g., jump hash, rendezvous hashing) for initial shard assignment
- [ ] Measure shard movement on node add/remove vs current round-robin approach
- [ ] Ensure backward compatibility with existing clusters

### Shard pinning API

**Problem**: Shards can be pinned via the `f=pinned` flag in etcd, but there is no public API to pin or unpin shards. This requires direct etcd manipulation.

- [ ] Add `PinShard(shardID)` and `UnpinShard(shardID)` to the cluster API
- [ ] Expose pin/unpin in the Inspector UI
- [ ] Add a gRPC/HTTP endpoint for shard pinning

### Inspector shard visualization

**Problem**: The Inspector UI shows nodes and objects but has no dedicated shard distribution view. Operators cannot easily see which shards are on which nodes, which are migrating, or where hotspots are.

Referenced in README.md roadmap: "Inspector UI enhancements - shard distribution graphs."

- [ ] Add shard distribution view showing shard-to-node mapping
- [ ] Highlight shards in migration state
- [ ] Show per-shard object count and call rate heatmap
- [ ] Add shard rebalancing history timeline

### Weighted node capacity for shard assignment

**Problem**: All nodes are treated as equal capacity during shard assignment and rebalancing. Heterogeneous clusters (nodes with different CPU/memory) get equal shard counts regardless of capacity.

- [ ] Add configurable node weight/capacity to cluster config
- [ ] Factor node weight into initial shard assignment (weighted round-robin)
- [ ] Factor node weight into rebalance target calculation (weighted ideal load)

---

## Testing Gaps

### Integration tests for shard migration under load

- [ ] Test object calls during shard migration (calls should fail and succeed after migration completes)
- [ ] Test concurrent shard migrations (multiple shards migrating between different node pairs)
- [ ] Test cascading failure: node fails mid-migration, then target node also fails

### Rebalancing integration tests

- [ ] Test rebalancing triggers with actual multi-node clusters (not just unit tests on the algorithm)
- [ ] Test that pinned shards are never moved during rebalancing
- [ ] Test rebalancing after node scale-up and scale-down

### Shard lock stress tests

- [ ] Test concurrent read and write lock acquisition under high contention
- [ ] Test that `AcquireWriteMultiple` sorted ordering prevents deadlocks under concurrent access
- [ ] Benchmark lock acquisition latency under varying contention levels

---

## References

- Sharding implementation: `cluster/sharding/sharding.go`
- Shard locking: `cluster/shardlock/shardlock.go`
- Shard mapping management: `cluster/consensusmanager/consensusmanager.go`
- Cluster integration: `cluster/cluster.go`
- Configuration: `cluster/config.go`
- Metrics: `util/metrics/metrics.go`
- Sharding reference doc: `docs/SHARDING.md`
- Kubernetes deployment TODO: `KUBERNETES_DEPLOYMENT_TODO.md`
