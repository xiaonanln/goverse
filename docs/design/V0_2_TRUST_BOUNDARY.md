# Goverse v0.2 — Trust-Boundary Design

> **Status**: Draft. Open decisions marked **`[DECIDE]`**.
>
> See [CHANGELOG.md](../../CHANGELOG.md) v0.1.0 "Known Issues" for what
> this release is meant to close.

---

## 1. Goal

v0.1 ships with the documented caveat: *"No built-in access control /
TLS: deploy in a private network or behind a service mesh until v0.2."*
Every gate-side validation gap we've found in v0.1 (HTTP `CallObject`
skipping `CheckClientAccess`, HTTP `CreateObject` skipping
`CheckClientCreate`, `DeleteObject` carrying no type, ghost queue
entries surviving tab close) traces back to one thing: **the gate is
not yet a real trust boundary**. It routes traffic but doesn't
authenticate or distinguish "this came from a client" from "this came
from another node" once the call enters the cluster.

v0.2's job: turn the gate into a trust boundary, *assuming a
TLS-terminating proxy (nginx / cloud LB / service mesh) sits in front
of it*. Native gate TLS is explicitly out of scope — see §2.

## 2. Non-goals (parked for v0.3 or beyond)

- **Native TLS termination on the gate.** v0.2 assumes operators put
  a TLS-terminating proxy (nginx / cloud LB / service mesh) in front
  of the gate; auth tokens (item 2) are protected at that layer. Most
  production deployments already run such a proxy, so a built-in
  `tls:` block would be redundant. Revisit in v0.3 if a "drop one
  binary on a public IP, no proxy" use case shows up.
- Distributed tracing (OpenTelemetry context propagation through
  reliable calls).
- Audit log of state-changing reliable calls (best after auth lands so
  it can record `caller_user_id`).
- Schema migration tool for Postgres-backed objects.
- Subscribe-permission model for SSE / `Register` stream (refinement
  of auth middleware).
- mTLS gate↔node intra-cluster (waits on native gate TLS first).
- Anti-cheat hooks beyond what `caller_user_id` enables.

## 3. Scope summary

**Tier 1 — must ship in v0.2** (the "Lean v0.2"):

| # | Item                                  | Approx LOC |
| - | ------------------------------------- | ---------- |
| 2 | Auth middleware + caller-identity ctx | ~900       |
| 4 | DeleteObject gate-side access check   | ~300       |

**Tier 2 — ship if Tier 1 lands smoothly**:

| #  | Item                                                | Approx LOC      |
| -- | --------------------------------------------------- | --------------- |
| 3  | `OnClientDisconnect` framework hook                 | ~700            |
| 5  | Migration-period unavailability fix                 | depends — design first |
| 6  | Proactive shard release on graceful shutdown        | ~500            |
| 7  | Per-call rate limiting at gate                      | ~300            |

Lean v0.2 is the recommended cut. Items 3–7 each have full sections
below so the work is queued and reviewable independently.

---

## 4. Item 2 — Auth middleware + caller identity

### 4.1 Motivation

Today every method body that takes a `player_id` is implicitly
saying "I trust the caller to tell me who they are." That breaks the
moment the gate is publicly reachable. Apps need:

- **Authentication** — "is this client really alice?"
- **Authorization at the row level** — "alice can call
  `HandleInput` for player_id=alice, not for bob".

Goverse can't (and shouldn't) own the auth provider — but it can own
the *plumbing* so apps don't reinvent it.

### 4.2 API shape

A two-layer design.

#### 4.2.1 Framework-level: pluggable validator

```go
package goverseapi

// CallerIdentity is the result of a successful auth check. The gate
// stamps this onto the call context; objects read it via
// CallerUserID(ctx) / CallerRoles(ctx).
type CallerIdentity struct {
    UserID string   // Stable per-user opaque id (e.g. JWT 'sub')
    Roles  []string // Optional, app-defined
}

// AuthValidator validates client credentials and returns the
// authenticated identity. Pluggable: callers bring their own JWT lib,
// OAuth provider, custom token system. See goverseapi/authjwt for the
// shipped happy-path implementation.
type AuthValidator interface {
    // Validate returns the identity for the request, or an error if
    // it should be rejected. transport is "http" | "grpc". headers
    // is the union of request headers (HTTP) or gRPC metadata.
    Validate(ctx context.Context, transport string, headers map[string][]string) (*CallerIdentity, error)
}
```

Wired into the gate via `GateServerConfig.AuthValidator`:

```go
gwServerConfig := &gateserver.GateServerConfig{
    // ...
    AuthValidator: authjwt.New(authjwt.Options{
        SigningKey: jwtSecret,
        Issuer:     "https://my-auth.example.com",
    }),
}
```

When `AuthValidator == nil` (default), no validation runs and
`CallerUserID(ctx)` returns `""` — preserving v0.1 behaviour. Apps
that opt in to auth get a non-empty identity on every successful
request.

#### 4.2.2 Built-in implementations

- **`authjwt.New(opts)`** — verifies HS256 / RS256 / ES256 bearer
  tokens from the `Authorization: Bearer <token>` header (HTTP) or
  `authorization` metadata (gRPC). Extracts `sub` as `UserID`,
  optional `roles` claim as `Roles`. Configurable issuer / audience /
  clock skew.
- **`authnone.New()`** — explicit "no auth, on purpose". Useful for
  local dev so the gate plumbing path is exercised without a real
  token provider.

That's it for v0.2. OAuth, sessions, mTLS-derived identity, etc. each
live as third-party packages or v0.3 work.

#### 4.2.3 App-level: row-level checks

```go
// In Match.HandleInput:
func (m *Match) HandleInput(ctx context.Context, req *pb.PlayerInputRequest) (*pb.PlayerInputResponse, error) {
    callerID := goverseapi.CallerUserID(ctx)
    if callerID == "" || callerID != req.PlayerId {
        return &pb.PlayerInputResponse{Ok: false, Reason: "unauthorized"}, nil
    }
    // ...
}
```

Apps that don't care (demos, internal services) ignore
`CallerUserID(ctx)` and behave like v0.1.

### 4.3 Implementation

- `goverseapi/auth.go`: `CallerIdentity`, `AuthValidator` interface,
  `CallerUserID(ctx)` / `CallerRoles(ctx)` helpers.
- `goverseapi/authjwt/`: shipped JWT implementation.
- `gate/gateserver/`: HTTP + gRPC handlers call
  `validator.Validate(...)` if configured, attach `CallerIdentity`
  to context via `callcontext.WithCallerIdentity(ctx, ident)`.
- `util/callcontext/`: extend the existing call-context plumbing
  (already carries `client_id`) to also carry `CallerIdentity`.
- `cluster/`: ensure `CallerIdentity` propagates through cross-node
  RPCs (CallObject, ReliableCallObject) so an object on node B can
  see the original caller's identity even if the request entered via
  node A's gate. **`[DECIDE]`** Can be metadata in the inter-node
  proto, or skipped in v0.2 (caller identity only available at the
  entry node — apps that need it cluster-wide work around). Default:
  metadata-in-proto, propagate it. Cleaner long-term.

### 4.4 Migration

- All v0.1 code keeps working unchanged when no validator is
  configured.
- Apps that want auth: configure a validator + check
  `CallerUserID(ctx)` in handler bodies. Backwards compatible —
  unmodified handlers ignore identity.

### 4.5 Test strategy

- New `gate/gateserver/gate_auth_integration_test.go`: gate with
  `BearerJWTValidator`, signed tokens accepted, unsigned/expired
  rejected, `CallerUserID(ctx)` reaches an in-node test object.
- `goverseapi/authjwt/jwt_test.go`: token parsing edge cases (alg
  confusion, kid handling, clock skew).

### 4.6 Open decisions

- **`[DECIDE]`** Identity propagation across nodes — propagate or
  not? Default: propagate.
- **`[DECIDE]`** Should `AuthValidator` reject the call (return error)
  or stamp an "unauthenticated" identity? Default: reject. Apps that
  want to allow anonymous use `authnone.New()` (which always succeeds
  with `UserID == ""`).
- **`[DECIDE]`** Header name conventions — `Authorization: Bearer
  <token>` (HTTP) is universal. gRPC: use `authorization` metadata
  (also conventional)?

---

## 5. Item 4 — DeleteObject gate-side access check

### 5.1 Motivation

`LifecycleValidator.CheckClientDelete(type, id)` exists in v0.1, but
neither the gate's gRPC nor HTTP delete handlers can call it: the
`DeleteObjectRequest` carries only `id`, no type. As a result a client
calling `DeleteObject` is treated by the receiving node as a node-
to-node hop and gets the INTERNAL pass — closing this is the last
gate-enforcement gap.

### 5.2 API shape (path 1: extend the proto)

We picked path 1 in PR #549's deferred-work note. It's the cleanest
and the least surprising for callers.

```proto
// gate/proto/gate.proto
message DeleteObjectRequest {
    string id = 1;
    // type is required so the gate can run an advisory
    // CheckClientDelete early-reject. Empty type is rejected.
    string type = 2;
}
```

HTTP route: `POST /api/v1/objects/delete/{type}/{id}`. There is no
legacy untyped fallback — the single-segment path is rejected with
400.

Authorization mirrors `CreateObject`: the gate runs the client-side
check, the node runs the node-side check. Layered:

- **Gate** runs `CheckClientDelete` on the client-supplied type
  before forwarding. A request the gate rejects never reaches the
  node.
- **Node** verifies the supplied type matches the object's real
  type from its registry, rejecting mismatches (closes the spoof
  gap). On match it then runs `CheckNodeDelete` on the (now
  trustworthy) type — symmetric with `node.createObject` running
  `CheckNodeCreate`.

Note: `EXTERNAL DELETE` rules have the same known limitation as
`EXTERNAL CREATE` — client-originated calls pass the gate's
`CheckClient*` but get re-checked against `CheckNode*` at the
destination, which fails for `EXTERNAL`. Future work can plumb a
caller-origin signal symmetrically for both create and delete; out
of scope for v0.2.

```proto
// proto/goverse.proto (inter-node service)
message DeleteObjectRequest {
    string id = 1;
    string type = 2; // forwarded to the node for spoof verification
}
```

### 5.3 Implementation

- Regenerate `gate/proto/gate.pb.go` and `proto/goverse.pb.go`.
- `gate/gateserver/gateserver.go:DeleteObject`: reject empty
  `req.Type` with `InvalidArgument`; otherwise run
  `CheckClientDelete(req.Type, req.Id)` and forward via
  `cluster.DeleteObject`.
- `gate/gateserver/http_handler.go:handleDeleteObject`: require the
  two-segment path; reject `/api/v1/objects/delete/{id}` with 400.
  Same authorization check.
- `cluster.DeleteObject(ctx, type, id)` is the only cluster-side
  delete entry point. It just routes; lifecycle authorization lives
  at the gate (for client deletes) or at the node (for both).
- `node.DeleteObject(ctx, type, id)` verifies `obj.Type() == type`,
  rejects mismatches, then runs `CheckNodeDelete` on the matched
  type — mirroring `createObject`'s `CheckNodeCreate`.
- `server.go:DeleteObject` forwards `req.GetType()` and
  `req.GetId()` straight to `node.DeleteObject`.
- `goverseapi.DeleteObject(ctx, type, id)` takes `type` as a new
  positional argument; calls `cluster.DeleteObject`.
- `client/goverseclient/client.go` + Python client: `DeleteObject`
  takes `type` (positional). v0.2 minor bump.

### 5.4 Migration

Both `DeleteObjectRequest` messages (gate-facing and inter-node)
gain a `type` field. The gate-facing `goverseapi.DeleteObject`,
`Client.DeleteObject` (Go), and `Client.delete_object` (Python)
each gain a `type` argument as well.

v0.1 callers that omit type get rejected at the gate (gRPC
`InvalidArgument`, HTTP 400) — they need to pass type when they
upgrade.

### 5.5 Test strategy

- `TestHandleDeleteObject_LifecycleRejected` — typed HTTP path with
  INTERNAL DELETE rule returns 403.
- `TestHandleDeleteObject_RejectsUntypedPath` — single-segment path
  returns 400.
- `TestDeleteObject_gRPC_RejectsEmptyType` — empty `req.Type` returns
  `InvalidArgument`.
- `TestDeleteObject_gRPC_LifecycleRejected` — gRPC handler returns
  `PermissionDenied` for a denied lifecycle rule.
- `TestNode_DeleteObject_RejectsTypeSpoof` — node-level pin that a
  claimed type which doesn't match the registry type is rejected,
  even if the gate would have authorized it.
- `TestNode_DeleteObject_RequiresType` — pins that node-side delete
  always requires a non-empty type (programming-contract error to
  omit it).

---

## 6. Item 3 — `OnClientDisconnect` (Tier 2)

Promoted from "future framework feature" because items 2 + 4 by
themselves leave the bomberman ghost-queue class of bug open: even
with auth, `pagehide` doesn't fire on browser crash / network drop /
mobile freeze, so the queue still accumulates dead entries.

### 6.1 API shape

Opt-in interface; mirrors existing lifecycle hooks (`OnCreated`,
`Destroy`).

```go
// In object/object.go:
type ClientDisconnectHandler interface {
    OnClientDisconnect(ctx context.Context, clientID string)
}
```

### 6.2 Wire delivery

1. **Gate side**: when `Gate.Unregister(clientID)` runs, gate sends
   `NotifyClientDisconnect(clientID)` to every connected node via a
   new node-side RPC.
2. **Node side**: receives the RPC, looks up types registered as
   implementing `ClientDisconnectHandler`, iterates live objects of
   those types, calls `OnClientDisconnect(ctx, clientID)` on each.
3. **Type discovery**: at `RegisterObjectType`, runtime checks if the
   type implements the interface and adds to a per-node index. No
   reflection at dispatch time.

### 6.3 Semantics

- **At-least-once, best-effort.** Gate crash mid-disconnect can lose
  the signal; apps that need a strong guarantee should layer a TTL
  backstop on top.
- Order of dispatch within a node is unspecified. Cross-node: every
  node that received the broadcast dispatches independently in
  parallel.

### 6.4 Sample integration

Bomberman's `MatchmakingQueue.OnClientDisconnect` would prune queue
entries with the matching `client_id`, replacing the
`pagehide`-only fix from PR #547 with one that also handles crashes.

### 6.5 Open decisions

- **`[DECIDE]`** Broadcast to all nodes vs. targeted (gate maintains
  client_id → node map for objects that subscribed). Default:
  broadcast. Simpler, fewer moving parts; targeted is a v0.3
  refinement once we have data.

---

## 7. Items 5 + 6 — Migration unavailability + graceful shutdown (Tier 2)

These are the two known issues already called out in v0.1's
`CHANGELOG.md`:

> - **Migration-period unavailability**: during shard handoff,
>   `GetCurrentNodeForObject` errors even though the current node is
>   still alive — causes brief unavailability during rebalance.
> - **Graceful shutdown**: scale-down relies on etcd lease expiry
>   rather than a proactive shard release.

Both warrant their own follow-up design docs (separate PRs in the
`docs/design/` tree), not a single shared section. Skeleton:

- **Item 5**: investigate whether the shard-mapping watch can serve
  stale-but-ok answers during handoff so `GetCurrentNodeForObject`
  doesn't error. Probably a small fix in
  `cluster/consensusmanager/`. Needs a load test demonstrating the
  current behaviour first.
- **Item 6**: add a `Cluster.GracefulShutdown(ctx)` that explicitly
  releases shard ownership to a successor before lease expiry.
  Coordinates with `consensusmanager`'s leader election.

## 8. Item 7 — Per-call rate limiting (Tier 2)

Optional, configurable token bucket per `client_id` (and optionally
per `caller_user_id` once item 2 lands). Lives in gate handlers
alongside the access check.

```yaml
gate_rate_limits:
  per_client:
    requests_per_second: 100
    burst: 200
  per_user:                          # only effective with auth
    requests_per_second: 50
    burst: 100
```

Implementation: `golang.org/x/time/rate.Limiter` per key, expired by
LRU. Tested with a high-concurrency stress run.

---

## 9. Rollout plan

1. **Land item 4 (DeleteObject)** first — smallest, mostly mechanical.
   Establishes the v0.2 minor-bump pattern.
2. **Land item 2 (auth)** — biggest single piece. Block on it until
   the API shape is reviewed; once landed, write a `samples/wallet`
   variant that requires JWTs as a worked example.
3. **Land item 3 (`OnClientDisconnect`)** if time permits before
   v0.2 cut. Otherwise slip to v0.3.
4. **Tier 2 items 5–7** based on remaining capacity.

Each item is its own PR with its own design doc / API stability
note. This file is the *index*.

## 10. CHANGELOG drafting

Once items 2, 4 land, draft `## [0.2.0]` with:

- Pluggable auth middleware + `goverseapi.CallerUserID(ctx)`.
- DeleteObject gate-side access checks (with type in the request).
- Whatever Tier 2 items made it.
- Documented expectation that the gate is deployed behind a
  TLS-terminating proxy (no built-in TLS in v0.2).

Updated "Known issues deferred to v0.3.0":
- Native TLS termination on the gate.
- Distributed tracing.
- Audit log.
- Schema migration tool.
- Subscribe-permission model for SSE.
- mTLS gate↔node intra-cluster.
- Anti-cheat / replay validation primitives.

## 11. Pre-implementation checklist

Items I need from a human reviewer before committing code:

- [ ] **Lean v0.2 vs full v0.2**: confirm we ship items 2, 4 only,
      with 3 / 5 / 6 / 7 stretch.
- [ ] **Auth model**: confirm pluggable interface + shipped JWT happy
      path, not opinionated default.
- [ ] **Identity propagation across nodes**: confirm "yes, propagate".
- [ ] **DeleteObject migration**: confirm warn-and-allow on empty
      type for v0.2; reject in v0.3.
- [ ] **TLS deferred to v0.3**: confirm v0.2 ships without native gate
      TLS; operators put a proxy in front.
- [ ] **Implementation order**: confirm 4 → 2 → 3 → Tier 2.

Once these are settled, each item gets its own implementation PR
with linked design doc updates.
