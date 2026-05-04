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

1. Old pod receives `SIGTERM` → GoVerse does nothing → etcd lease expires
   after TTL → other nodes notice the gap → shard reassignment starts →
   all calls to that pod's objects fail for the entire TTL window.

### 2.2 Scale-down / pod eviction

Same as rolling update step 1. When the operator reduces replica count
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

### Item B — Migration-period availability

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

### Item C — Gate connection draining on shutdown

**Priority: medium.**

#### Current behaviour

`GateServer.Stop()` (`gate/gateserver/gateserver.go`):

1. Sets `stopped = true` — new RPCs rejected immediately.
2. Calls `grpcServer.Stop()` — **hard stop**, no GoAway, all transports
   closed immediately. gRPC clients see a broken connection.
3. Calls `httpServer.Shutdown(ctx)` with a 5 s timeout — graceful for
   HTTP, but SSE streams are just dropped when the transport closes.
4. Stops gate and cluster.

Result: every connected gRPC and SSE client sees an abrupt disconnect
with no signal to reconnect.

#### Target behaviour

```
Stop() called (SIGTERM)
  1. stopped = true           → new RPCs rejected (already done)
  2. Notify SSE streams       → each handler sends "event: shutdown\n\n"
                                 then exits; clients know to reconnect
  3. grpcServer.GracefulStop() in goroutine
                              → sends HTTP/2 GoAway; gRPC clients
                                 reconnect to another gate
  4. Wait up to GateDrainTimeout (default 10 s) for streams to close
  5. grpcServer.Stop()        → force-close any remaining streams
  6. httpServer.Shutdown(5 s) → already done; SSE is gone by now
  7. gate.Stop() + cluster.Stop()
```

#### Implementation notes

**GoAway (gRPC):** Replace `grpcServer.Stop()` with a bounded
`GracefulStop()`:

```go
done := make(chan struct{})
go func() { s.grpcServer.GracefulStop(); close(done) }()
select {
case <-done:
case <-time.After(s.config.GateDrainTimeout):
    s.grpcServer.Stop()
}
```

`GracefulStop()` sends GoAway and waits for all RPCs to finish. Without
the timeout it would hang indefinitely on long-lived `Register` streams,
so the force-stop fallback is essential.

**SSE shutdown event:** The SSE handler
(`http_handler.go:handleEventsStream`) loops on
`clientProxy.MessageChan()` and `r.Context().Done()`. Add a
gate-level broadcast channel closed on `Stop()`:

```go
// GateServer
sseShutdown chan struct{} // closed in Stop()

// handleEventsStream
select {
case msg := <-clientProxy.MessageChan(): ...
case <-s.sseShutdown:
    fmt.Fprintf(w, "event: shutdown\ndata: {}\n\n")
    flusher.Flush()
    return
case <-r.Context().Done(): return
}
```

No per-stream registry needed — closing the broadcast channel wakes all
handlers simultaneously.

**Config:**

```go
type GateServerConfig struct {
    ...
    GateDrainTimeout time.Duration // default 10 s
}
```

**Approx LOC:** ~150 (`gate/gateserver/gateserver.go` ~80,
`gate/gateserver/http_handler.go` ~50, config ~20).

---

### Item D — Node version metadata in etcd

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
| B | Migration-period availability             | Medium   | design first    |
| C | Gate connection draining                  | Medium   | ~200            |
| D | Node version metadata in etcd             | Low      | ~100            |

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

1. **A** — closes the shutdown gap; most impactful for rolling updates.
2. **C** — gate draining; completes the user-visible shutdown story.
3. **B** — investigate and design; implement once A is stable.
4. **D** — operational polish; last.
