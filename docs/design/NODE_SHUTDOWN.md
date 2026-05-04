# Node Graceful Shutdown

> **Status**: Not started.

---

## 1. Goal

When a node shuts down (planned or via SIGTERM), it should leave the cluster in
a clean state: objects persisted, shards immediately available to other nodes,
no lease-TTL wait.

---

## 2. Current behaviour

`server.Run()` shutdown sequence on SIGTERM:

```
1. grpcServer.Stop()     — stop new RPCs
2. cluster.Stop()        — revoke etcd lease  ← too early
3. node.Stop()           — save objects to Postgres  ← too late
```

**Problem:** `cluster.Stop()` revokes the etcd lease immediately, so other
nodes start claiming this node's shards right away. But objects haven't been
saved yet — other nodes load stale state from Postgres.

In addition, other nodes must wait for the etcd lease TTL before seeing the
node as gone... Actually no — `cluster.Stop()` already calls `Revoke()` on
the lease (`etcdmanager.UnregisterKeyLease` → `client.Revoke()`), so
**proactive removal is already in place**. The only bug is the ordering.

---

## 3. Scenarios

| Scenario | Current issue | After fix |
| --- | --- | --- |
| **Planned restart (same node ID)** | Lease revoked → 2 rounds of shard migration | Same. Downside accepted: if node restarts fast enough (<TTL), not revoking would be zero-migration. But consistent behaviour is simpler. |
| **Version upgrade / downgrade** | Stale Postgres state race | Fixed: state saved before lease revoked |
| **Scale down (permanent)** | Stale Postgres state race | Fixed |
| **Hard crash** | No SIGTERM → lease TTL wait (~15s) | Unchanged — cannot fix with shutdown code |
| **Scale up (add node)** | Not related to shutdown | Unaffected |

---

## 4. Design

### 4.1 Corrected shutdown order

```
SIGTERM received
  1. grpcServer.Stop()        — reject new RPCs; in-flight complete via stopMu
  2. node.Stop()              — wait for in-flight, save all objects to Postgres,
                                destroy objects
  3. cluster.Stop()           — revoke etcd lease (immediate removal from cluster)
  4. process exits
```

The fix is moving step 2 and 3: **save before revoke**.

### 4.2 Why in-flight calls are safe

`node.Stop()` sets `stopped = true` and then acquires `stopMu` as a write lock.
All RPC handlers hold `stopMu` as a read lock while running. So `node.Stop()`
waits for all in-flight handlers to return before proceeding to `saveAllObjectsLocked`.
No call is aborted mid-flight.

### 4.3 Object persistence

`node.Stop()` already calls `saveAllObjectsLocked()` which writes every active
object's state to Postgres via `provider.SaveObject()` (upsert on `goverse_objects`
table). This is covered by `TestStop_ObjectsPersistedBeforeClearing`.

### 4.4 Etcd removal

`cluster.Stop()` calls `unregisterNode()` → `UnregisterKeyLease()` →
`client.Delete(key)` + `client.Revoke(leaseID)`. This is an immediate removal —
no waiting for the 15s `NodeLeaseTTL`. The leader's etcd watch fires and
`calcReassignShardTargetNodes` redistributes the released shards right away.

---

## 5. Known limitations / TODOs

### 5.1 Leader resignation (TODO)

If the shutting-down node is the consensus leader, it should resign before
removing itself from etcd to ensure another node cleanly takes over the watch
loop. Without this, there is a brief window where leadership is in flux.

This is deferred — the current fix is still a net improvement even without it.

### 5.2 Hard crash

Hard crashes (OOM, kernel kill, `SIGKILL`) bypass all shutdown code. The only
recovery mechanism is the etcd lease TTL (15s by default). This is not fixable
at the application level; a shorter TTL reduces the window at the cost of more
aggressive keepalives. Out of scope here.

### 5.3 Planned restart with same node ID

If a node restarts with the same ID and comes back within the lease TTL,
revoking the lease causes two unnecessary rounds of shard migration. The
alternative (not revoking) would be zero-migration on fast restarts but is
harder to reason about. The current design accepts the trade-off in favour of
simplicity and consistency.

---

## 6. Implementation

**File:** `server/server.go`

Swap `cluster.Stop()` and `node.Stop()` call order. No other changes needed —
all the individual mechanisms (object save, lease revoke) are already
implemented.

**Approx LOC:** ~5 (reorder + comments).

**Tests:** Existing `TestStop_ObjectsPersistedBeforeClearing` covers the node
persistence invariant. An integration test verifying the full ordering under
real etcd is desirable but not required for the initial fix.
