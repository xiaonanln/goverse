# Node Graceful Shutdown

> **Status**: Core fix done (#581). Leader resignation pending.

---

## 1. Goal

When a node shuts down (planned or via SIGTERM), it should leave the cluster in
a clean state: objects persisted, shards immediately available to other nodes,
no lease-TTL wait.

---

## 2. Current behaviour (before fix)

`server.Run()` shutdown sequence on SIGTERM:

```
1. grpcServer.Stop()     — stop new RPCs
2. cluster.Stop()        — revoke etcd lease  ← too early
3. node.Stop()           — save objects to Postgres  ← too late
```

**Problem:** `cluster.Stop()` revokes the etcd lease immediately, so other
nodes start claiming this node's shards right away. But objects haven't been
saved yet — other nodes load stale state from Postgres.

`cluster.Stop()` already calls `Revoke()` on the lease
(`etcdmanager.UnregisterKeyLease` → `client.Revoke()`), so proactive removal
is already in place. The only bug was the ordering.

---

## 3. Scenarios

| Scenario | Before fix | After fix |
| --- | --- | --- |
| **Planned restart (same node ID)** | Lease revoked → 2 rounds of shard migration | Same. Accepted: consistent behaviour is simpler than conditional revocation. |
| **Version upgrade / downgrade** | Stale Postgres state race | ✅ Fixed: state saved before lease revoked |
| **Scale down (permanent)** | Stale Postgres state race | ✅ Fixed |
| **Hard crash** | No SIGTERM → lease TTL wait (~15s) | Unchanged — cannot fix with shutdown code |
| **Scale up (add node)** | Not related to shutdown | Unaffected |

---

## 4. Design

### 4.1 Corrected shutdown order ✅ Done — #581 (P0)

```
SIGTERM received
  1. grpcServer.Stop()        — reject new RPCs; in-flight forwarded calls cancelled via ctx
  2. node.Stop()              — wait for handler goroutines to exit (stopMu.Lock),
                                save all objects to Postgres, destroy objects
  3. cluster.Stop()           — revoke etcd lease (immediate removal from cluster)
  4. process exits
```

The fix: **save before revoke** (`node.Stop` and `cluster.Stop` swapped in `server/server.go`).

### 4.2 In-flight call behaviour

`grpcServer.Stop()` cancels handler contexts immediately. Two cases:

- **Forwarded calls** (gRPC to another node): context cancellation causes the
  call to return an error. Caller receives an error and can retry. `ReliableCallObject`
  handles this transparently.
- **Local object operations** (in-process): do not check context; run to
  completion. `stopMu.Lock()` in `node.Stop()` waits for all handler goroutines
  to exit before saving, so saved state is always self-consistent.

### 4.3 Object persistence

`node.Stop()` already calls `saveAllObjectsLocked()` which writes every active
object's state to Postgres via `provider.SaveObject()` (upsert on `goverse_objects`
table). Covered by `TestStop_ObjectsPersistedBeforeClearing`.

### 4.4 Etcd removal

`cluster.Stop()` calls `unregisterNode()` → `UnregisterKeyLease()` →
`client.Delete(key)` + `client.Revoke(leaseID)`. This is an immediate removal —
no waiting for the 15s `NodeLeaseTTL`. The leader's etcd watch fires and
`calcReassignShardTargetNodes` redistributes the released shards right away.

---

## 5. Known limitations / TODOs

### 5.1 Leader resignation — P2, not done

If the shutting-down node is the consensus leader, it should resign before
`cluster.Stop()` removes itself from etcd. Without this, there is a brief
window where no node is running the shard-assignment watch loop, potentially
delaying shard redistribution.

Requires investigation in `consensusmanager/` to find the resign hook. Low
urgency — the cluster self-heals when another node wins the election, and the
window is typically sub-second.

### 5.2 Hard crash — out of scope

Hard crashes (OOM, kernel kill, `SIGKILL`) bypass all shutdown code. The only
recovery mechanism is the etcd lease TTL (15s by default). Not fixable at the
application level; a shorter TTL reduces the window at the cost of more
aggressive keepalives.

### 5.3 Planned restart with same node ID — P3, accepted tradeoff

If a node restarts with the same ID and comes back within the lease TTL,
revoking the lease causes two unnecessary rounds of shard migration. Not
revoking would be zero-migration on fast restarts but is harder to reason
about. Current design accepts the trade-off in favour of simplicity.

---

## 6. Implementation summary

| Item | File | LOC | Status |
| --- | --- | --- | --- |
| Swap shutdown order (§4.1) | `server/server.go` | ~5 | ✅ Done — #581 |
| Leader resignation (§5.1) | `consensusmanager/` | TBD | ⏳ Not started |
