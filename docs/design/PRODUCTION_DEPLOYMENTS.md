# Production Deployments & Kubernetes Support

> **Status**: Not started. Items ranked by impact; implementation order TBD.

---

## 1. Goal

GoVerse nodes and gates are deployed as pods in Kubernetes (or as VMs in
production). Common operations — rolling updates, rollbacks, scale-up,
scale-down — all require the cluster to hand off shard ownership cleanly.
Today every one of these operations causes a window of visible errors.
This document identifies what needs to change to make Goverse first-class
in a k8s environment.

## 2. Problem scenarios

### 2.1 Rolling update (most common)

k8s terminates old pods one at a time while bringing up new ones:

1. New pod starts → `/ready` returns 200 immediately → k8s routes traffic
   to it → shards not yet claimed → requests fail.
2. Old pod receives `SIGTERM` → GoVerse does nothing → etcd lease expires
   after TTL → other nodes notice the gap → shard reassignment starts →
   all calls to that pod's objects fail for the entire TTL window.

### 2.2 Scale-down / pod eviction

Same as rolling update step 2. When the operator reduces replica count
or a node is evicted, there is no graceful handoff. Objects are
unavailable until etcd lease expiry.

### 2.3 Shard handoff during rebalance

When a new node joins (scale-up) or after any reassignment, the cluster
moves shards from the old owner to the new one. During this transition,
`GetCurrentNodeForObject` errors even though the old node is still alive
and serving. Callers get errors they didn't need to get.

---

## 3. Items

### Item A — Graceful shutdown: proactive shard release

**Priority: highest.**

When a node receives `SIGTERM`, it should explicitly release its shard
ownership back to the consensus layer before stopping, allowing the
leader to reassign them immediately rather than waiting for lease expiry.

**Behaviour:**

```
SIGTERM received
  → node calls cluster.PrepareShutdown(ctx)
  → for each owned shard: write TargetNode="" to etcd (release)
  → leader picks up the vacated shards, assigns them to remaining nodes
  → in-flight requests complete (bounded by terminationGracePeriodSeconds)
  → node stops
```

The node should stop accepting new calls as soon as `PrepareShutdown`
begins. In-flight calls are allowed to drain within a configurable
`ShutdownDrainTimeout` (default: 15 s, must be < `terminationGracePeriodSeconds`).

**k8s wiring:**

```yaml
# Deployment spec
terminationGracePeriodSeconds: 60

lifecycle:
  preStop:
    exec:
      command: ["/bin/sh", "-c", "sleep 2"]   # give k8s time to deregister the pod from LB
```

GoVerse handles the rest via `SIGTERM`.

**Approx LOC:** ~500 (`cluster/`, `server/server.go`, `consensusmanager/`).

---

### Item B — Readiness probe reflects connectivity and shard map sync

**Priority: high.** Small change, high impact on rolling updates.

The existing `/ready` endpoint returns 200 as soon as the process starts,
before the node or gate has done any meaningful cluster work.

A node having no shards allocated to it is fine — the leader may simply
not have assigned any yet, and that is not a reason to be unready. The
meaningful readiness signal is whether the component can correctly handle
traffic *right now*.

**Gate is ready when:**
- The full shard map has been synced from etcd, **and**
- A gRPC connection to every known node has been established.
- Nodes that fail to connect (unreachable / not yet started) do not block
  readiness — their shards will simply return errors as they do today.

**Node is ready when:**
- The full shard map has been synced from etcd, **and**
- At least one gate has connected to it (i.e. it is reachable from the
  cluster's perspective).
- Gates that have not connected yet do not block readiness.

**Approx LOC:** ~100 (`server/server.go` + `gate/gateserver/` readiness
handlers; consensus manager needs to expose "shard map synced" signal).

---

### Item C — Migration-period availability

**Priority: medium.** Needs design before implementation.

During a shard handoff, `GetCurrentNodeForObject` returns an error for
the brief window between when the old node releases the shard and the
new node claims it. The old node is still alive and capable of serving
the call during this window.

**Proposed fix:** During handoff, route to `CurrentNode` until
`TargetNode` confirms claim. The shard-mapping watch should serve
`CurrentNode` as a stale-but-valid answer rather than returning an error
when `TargetNode != CurrentNode`.

This requires investigation in `cluster/consensusmanager/` to understand
exactly when the error fires and whether a fallback read is safe.

**Approx LOC:** depends on investigation.

---

### Item D — Gate connection draining on shutdown

**Priority: medium.**

When the gate pod receives `SIGTERM`, it currently shuts down abruptly,
breaking all active client connections. For HTTP long-lived connections
(SSE streams) and gRPC streams, this is visible to end users.

**Change:** On `SIGTERM`, the gate should:
1. Stop accepting new TCP connections (close the listener).
2. Send a `GoAway` frame on active gRPC connections so clients reconnect
   to another gate.
3. Close SSE streams with a final `event: shutdown` so web clients can
   reconnect gracefully.
4. Wait up to `GateDrainTimeout` (default: 10 s) for open streams to
   close, then force-close.

**Approx LOC:** ~200 (`gate/gateserver/`).

---

### Item E — Node version metadata in etcd

**Priority: low.**

During a rolling update, old and new node versions coexist. There is no
way to tell which version each node is running, making it impossible to:
- Verify a rollout is complete.
- Implement "drain old nodes first" strategies.
- Correlate errors to a specific version in observability tools.

**Change:** Each node writes its binary version string to etcd alongside
its shard ownership record. The Inspector UI and `/healthz` output expose
per-node versions.

**Approx LOC:** ~100.

---

## 4. Scope summary

| # | Item                                      | Priority | Approx LOC      |
| - | ----------------------------------------- | -------- | --------------- |
| A | Graceful shutdown / proactive shard release | Highest | ~500            |
| B | Readiness probe reflects shard claims     | High     | ~50             |
| C | Migration-period availability             | Medium   | design first    |
| D | Gate connection draining                  | Medium   | ~200            |
| E | Node version metadata in etcd             | Low      | ~100            |

## 5. Non-goals

- **Automatic rolling update orchestration**: GoVerse does not replace
  the k8s deployment controller. These items make Goverse *safe* for
  k8s to orchestrate, not self-orchestrating.
- **Pod auto-scaling based on shard load**: out of scope; use k8s HPA
  on CPU/memory metrics exposed via Prometheus.
- **mTLS intra-cluster**: tracked separately.
- **Zero-downtime schema migrations for Postgres-backed objects**: tracked
  separately.

## 6. Recommended implementation order

1. **B** — small, high-leverage, unblocks everything else (no rolling
   update testing is reliable until readiness is correct).
2. **A** — closes the shutdown gap; together with B, rolling updates
   become zero-error.
3. **D** — gate draining; completes the user-visible shutdown story.
4. **C** — investigate and design; implement once A+B are stable.
5. **E** — operational polish; last.
