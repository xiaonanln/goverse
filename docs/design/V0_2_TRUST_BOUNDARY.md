# Goverse v0.2 — Trust-Boundary Design

> **Status**: Items 2 and 4 (Lean v0.2 Tier 1) shipped. Tier 2 items analysed and deferred — see NODE_SHUTDOWN.md for shutdown work.
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

Wired into the gate via `GateConfig.AuthValidator` (see chat sample for
a concrete example):

```go
// Implement the interface with whatever credential check you need.
type MyAuthValidator struct{}

func (v *MyAuthValidator) Validate(_ context.Context, headers map[string][]string) (*goverseapi.CallerIdentity, error) {
    token := headers["authorization"]
    if len(token) == 0 {
        return nil, fmt.Errorf("missing authorization header")
    }
    userID, err := verifyToken(token[0]) // your JWT/OAuth/API-key logic
    if err != nil {
        return nil, err
    }
    return &goverseapi.CallerIdentity{UserID: userID}, nil
}

// Wire it in:
cfg.AuthValidator = &MyAuthValidator{}
```

When `AuthValidator == nil` (default), no validation runs and
`CallerUserID(ctx)` returns `""` — preserving v0.1 behaviour. Apps
that opt in to auth get a non-empty identity on every successful
request.

#### 4.2.2 Built-in implementations

v0.2 ships **no** built-in implementations. JWT, OAuth, session
cookies, API-key validation — all live as application code or
third-party packages. Bundling a specific JWT library into the
framework would force that dependency on every user of goverse.

The chat sample (`samples/chat/gate/main.go`) demonstrates how to
write a concrete `AuthValidator` (username + password header check).
Apps that need JWT bring their own library and implement the
interface themselves.

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

**Shipped (PRs #554–#561, #565):**

- `goverseapi/auth.go`: `CallerIdentity`, `AuthValidator` interface,
  `CallerUserID(ctx)` / `CallerRoles(ctx)` / `CallerHasRole(ctx)`
  helpers. ✅
- `util/callcontext/`: extended with `CallerIdentity`, context
  injection/extraction helpers, and gRPC metadata
  `InjectCallerToOutgoing` / `ExtractCallerFromIncoming`. ✅
- `gate/gateserver/`: HTTP + gRPC handlers call
  `validator.Validate(...)` if configured, attach `CallerIdentity`
  to context. ✅
- `cluster/cluster.go`: `CallObject` calls `InjectCallerToOutgoing`
  before remote RPCs so the identity reaches objects on any node. ✅
- `server/server.go`: `CallObject` handler calls
  `ExtractCallerFromIncoming` to restore the identity from gRPC
  metadata. ✅

### 4.4 Migration

- All v0.1 code keeps working unchanged when no validator is
  configured.
- Apps that want auth: configure a validator + check
  `CallerUserID(ctx)` in handler bodies. Backwards compatible —
  unmodified handlers ignore identity.

### 4.5 Test strategy

**Shipped:**

- `gate/gateserver/gateserver_auth_test.go`: unit tests — AuthValidator
  rejection returns `Unauthenticated`, successful validation stores
  identity on Register. ✅
- `gate/gateserver/gateserver_calleridentity_integration_test.go`:
  full-stack test — identity flows gate → node → object method via
  real gRPC. ✅
- `cluster/cluster_calleridentity_integration_test.go`: cross-node
  propagation via gRPC metadata. ✅
- `util/callcontext/callcontext_grpc_test.go`: bufconn round-trip for
  Inject/Extract helpers. ✅

### 4.6 Decisions made

- **Identity propagation across nodes**: propagated via gRPC metadata
  (`x-caller-user-id` / `x-caller-roles`) on the `CallObject` path.
  `ReliableCallObject` is internal cross-node infrastructure (not
  client-initiated), so CallerIdentity does not apply there. ✅
- **Reject vs. anonymous**: `AuthValidator` returns error to reject;
  apps that want anonymous access simply don't set an `AuthValidator`
  (v0.1 behaviour). ✅
- **Header conventions**: `authorization` (lowercase) for gRPC
  metadata; the chat sample uses custom `x-username`/`x-password`
  keys to show that the interface is fully pluggable. ✅
- **No built-in JWT**: apps bring their own token library and
  implement `AuthValidator` themselves. ✅

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

## 6. Rollout plan

1. **Land item 4 (DeleteObject)** ✅ Done (all layers including node
   type-spoof check and gate lifecycle enforcement).
2. **Land item 2 (auth)** ✅ Done (PRs #554–#561, #565).
   No built-in JWT — apps bring their own `AuthValidator`.
3. Tier 2 items (migration-period availability, proactive shard release) analysed and deferred — implementation cost outweighs benefit at current scale.

Each item is its own PR with its own design doc / API stability
note. This file is the *index*.

## 7. CHANGELOG drafting

Once items 2, 4 land, draft `## [0.2.0]` with:

- Pluggable auth middleware + `goverseapi.CallerUserID(ctx)`.
- DeleteObject gate-side access checks (with type in the request).
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

## 8. Pre-implementation checklist

- [x] **Lean v0.2 vs full v0.2**: items 2, 4 only; Tier 2 deferred.
- [x] **Auth model**: pluggable interface only — no bundled JWT.
- [x] **Identity propagation across nodes**: yes, via gRPC metadata.
- [x] **DeleteObject migration**: empty type rejected (no warn-and-allow).
- [x] **TLS deferred to v0.3**: confirmed.
- [x] **Implementation order**: 4 → 2 → Tier 2.

Once these are settled, each item gets its own implementation PR
with linked design doc updates.
