# Object Access Control Design

> **Status**: Draft  
> This document describes the config-based object access control feature for Goverse, enabling fine-grained control over which clients and objects can call specific methods.

---

## 1. Problem Statement

Currently, when a `CallObject` RPC arrives at a node:
- Objects are auto-created if they don't exist
- Any method can be called on any object

This creates security and resource concerns:

- **Arbitrary object creation**: Clients can trigger creation of objects with any ID
- **Invalid IDs**: No enforcement of naming conventions (e.g., `ChatRoom-{name}`)
- **Unauthorized method calls**: Internal methods can be called by external clients
- **Resource exhaustion**: Attackers could create millions of objects

## 2. Goals

1. **Validate object IDs** against configurable patterns before auto-creation
2. **Control method access** - whitelist which methods clients can call
3. **Gate-side filtering** to reject invalid requests before they reach nodes
4. **Centralized configuration** shared by gates and nodes
5. **Secure by default** with explicit allow-listing
6. **Minimal performance impact** using compiled regex patterns

## 3. Non-Goals

- Complex validation logic (use code-based validation for that)
- Authentication/authorization by user identity (separate concern)
- Rate limiting (future feature)

## 4. Design

### 4.1 Configuration Format

Add `object_access_rules` section to the existing YAML config:

```yaml
# config.yaml
etcd:
  address: localhost:2379
  prefix: /goverse

nodes:
  - id: node1
    grpc_addr: localhost:48000
    advertise_addr: localhost:48000

gates:
  - id: gate1
    grpc_addr: localhost:49000
    http_addr: :8080

# Object access control rules
# Evaluated top-to-bottom, first match wins
# Each rule: type (required), id (optional), method, access
# Pattern matching:
#   - Literal string: exact match (e.g., "ChatRoomMgr")
#   - /regexp/: regex full match (e.g., "/[a-zA-Z0-9_-]+/")
# Access levels: REJECT, INTERNAL, EXTERNAL, ALLOW
#   - REJECT: Deny all access (clients and nodes)
#   - INTERNAL: Allow only node-to-node calls, deny clients
#   - EXTERNAL: Allow only client calls via gate, deny nodes
#   - ALLOW: Allow both clients and nodes
object_access_rules:
  # ChatRoomMgr: singleton, clients can list rooms
  - type: ChatRoomMgr
    id: ChatRoomMgr0
    method: ListChatRooms
    access: ALLOW
  
  # ChatRoomMgr: all other methods internal only
  - type: ChatRoomMgr
    method: /.*/
    access: INTERNAL

  # ChatRoom: clients can interact with these methods
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    method: /(Join|Leave|SendMessage|GetMessages)/
    access: ALLOW
  
  # ChatRoom: other methods (NotifyMembers, etc.) internal only
  - type: ChatRoom
    method: /.*/
    access: INTERNAL

  # InternalScheduler: only accessible by nodes
  - type: InternalScheduler
    access: INTERNAL

  # Counter: clients can increment/decrement/get
  - type: Counter
    id: /[a-zA-Z0-9_-]+/
    method: /(Increment|Decrement|Get)/
    access: ALLOW
  
  # Counter: Reset only from nodes
  - type: Counter
    method: Reset
    access: INTERNAL

  # Default: deny everything else
  - type: /.*/
    access: REJECT
```

### 4.2 Access Control Semantics

**Rule format:**

| Field | Required | Description |
|-------|----------|-------------|
| `type` | Yes | Object type. Literal string for exact match, or `/regexp/` for regex. |
| `id` | No | Object ID pattern. Omit to match all IDs. Literal or `/regexp/`. |
| `method` | No | Method name pattern. Omit to match all methods. Literal or `/regexp/`. |
| `access` | Yes | Access level: `REJECT`, `INTERNAL`, `EXTERNAL`, or `ALLOW` |

**Pattern matching:**
- `ChatRoom` - Literal string, matches exactly `"ChatRoom"`
- `/Chat.*/` - Regex, matches full string against pattern (auto-anchored)
- `/.*/` - Regex, matches any string (wildcard)
- Omitted field - Matches all (equivalent to `/.*/`)

Note: Regex patterns are automatically anchored to match the **full string** (like `^...$`), not a substring.

**Access levels:**

| Level | Client (via Gate) | Node (object-to-object) |
|-------|-------------------|-------------------------|
| `REJECT` | ✗ Denied | ✗ Denied |
| `INTERNAL` | ✗ Denied | ✓ Allowed |
| `EXTERNAL` | ✓ Allowed | ✗ Denied |
| `ALLOW` | ✓ Allowed | ✓ Allowed |

**Evaluation:**
1. Rules are evaluated **top-to-bottom**
2. **First matching rule wins** (type, id, AND method must all match)
3. If no rule matches, access is **denied by default**
4. Order matters - put specific rules before general ones

**Common patterns:**

```yaml
# Allow specific methods to clients, rest to nodes only
- type: MyObject
  method: /(PublicMethod1|PublicMethod2)/
  access: ALLOW
- type: MyObject
  access: INTERNAL

# Internal-only object type (all IDs, all methods)
- type: InternalService
  access: INTERNAL

# Restrict to specific ID pattern
- type: UserSession
  id: /user-[0-9]+/
  access: ALLOW

# Completely block an object type
- type: Deprecated
  access: REJECT

# Singleton object with specific method
- type: ConfigManager
  id: ConfigManager0
  method: GetConfig
  access: ALLOW

# Default deny (should be last rule)
- type: /.*/
  access: REJECT
```

### 4.3 Validation Flow

#### Gate-side (first line of defense)

```
┌─────────┐    CallObject("ChatRoom-General", "SendMessage", ...)
│  Client │ ─────────────────────────────────────────────────────►
└─────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │        Gate         │
                    │                     │
                    │ For each rule:      │
                    │  1. Match object ID │
                    │  2. Match method    │
                    │  3. If both match:  │
                    │     check access    │
                    └──────────┬──────────┘
                               │
       ┌───────────────┬───────┴───────┬───────────────┐
       │               │               │               │
       ▼               ▼               ▼               ▼
    REJECT         INTERNAL        EXTERNAL         ALLOW
       │               │               │               │
       ▼               ▼               ▼               ▼
  Return error    Return error   Forward to Node  Forward to Node
```

#### Node-side (enforces INTERNAL)

```
┌─────────┐    CallObject (from gate, access=ALLOW or EXTERNAL)
│  Gate   │ ─────────────►  ┌─────────────────┐
└─────────┘                 │      Node       │
                            │                 │
                            │ (already        │
                            │  validated)     │
                            │                 │
                            │ Call method     │
                            └─────────────────┘

┌─────────┐    CallObject (object-to-object)
│  Node   │ ─────────────►  ┌─────────────────┐
│ (obj A) │                 │      Node       │
└─────────┘                 │    (obj B)      │
                            │                 │
                            │ For each rule:  │
                            │  match & check  │
                            │  access level   │
                            │                 │
                            │ ALLOW or        │
                            │ INTERNAL:       │
                            │   Call method   │
                            │                 │
                            │ REJECT or       │
                            │ EXTERNAL:       │
                            │   Return error  │
                            └─────────────────┘
```

### 4.4 Implementation Details

#### Config struct additions

```go
// config/config.go

type Config struct {
    Etcd        EtcdConfig         `yaml:"etcd"`
    Nodes       []NodeConfig       `yaml:"nodes"`
    Gates       []GateConfig       `yaml:"gates"`
    AccessRules []AccessRule       `yaml:"object_access_rules"`
}

// AccessRule defines a single access control rule
type AccessRule struct {
    Type   string `yaml:"type"`   // Required: object type pattern
    ID     string `yaml:"id"`     // Optional: object ID pattern (default: match all)
    Method string `yaml:"method"` // Optional: method pattern (default: match all)
    Access string `yaml:"access"` // Required: REJECT, INTERNAL, EXTERNAL, or ALLOW
}

// Access level constants
const (
    AccessReject   = "REJECT"
    AccessInternal = "INTERNAL"
    AccessExternal = "EXTERNAL"
    AccessAllow    = "ALLOW"
)

// Compiled rules for runtime use
type AccessValidator struct {
    rules []*CompiledRule
}

type CompiledRule struct {
    typeMatcher   PatternMatcher
    idMatcher     PatternMatcher
    methodMatcher PatternMatcher
    access        string
}

// matchAllMatcher always returns true (for omitted fields)
type matchAllMatcher struct{}

func (m matchAllMatcher) Match(s string) bool {
    return true
}

// PatternMatcher matches strings either exactly or via regexp
type PatternMatcher interface {
    Match(s string) bool
}

type literalMatcher string

func (m literalMatcher) Match(s string) bool {
    return string(m) == s
}

type regexpMatcher struct {
    re *regexp.Regexp
}

func (m *regexpMatcher) Match(s string) bool {
    return m.re.MatchString(s)
}

// parsePattern returns a matcher for literal strings or /regexp/ patterns.
// Regexp patterns are auto-anchored to match the full string.
func parsePattern(pattern string) (PatternMatcher, error) {
    if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 1 {
        // Regexp pattern: /.../ - auto-anchor to match full string
        regexStr := pattern[1 : len(pattern)-1]
        regexStr = "^(?:" + regexStr + ")$"  // Auto-anchor for full match
        re, err := regexp.Compile(regexStr)
        if err != nil {
            return nil, err
        }
        return &regexpMatcher{re: re}, nil
    }
    // Literal string: exact match
    return literalMatcher(pattern), nil
}

// parsePatternOrMatchAll returns a matcher. Empty string matches all.
func parsePatternOrMatchAll(pattern string) (PatternMatcher, error) {
    if pattern == "" {
        return matchAllMatcher{}, nil
    }
    return parsePattern(pattern)
}

func NewAccessValidator(rules []AccessRule) (*AccessValidator, error) {
    v := &AccessValidator{
        rules: make([]*CompiledRule, 0, len(rules)),
    }
    
    for i, rule := range rules {
        compiled := &CompiledRule{access: rule.Access}
        var err error
        
        // Type is required
        if rule.Type == "" {
            return nil, fmt.Errorf("missing type in rule %d", i)
        }
        compiled.typeMatcher, err = parsePattern(rule.Type)
        if err != nil {
            return nil, fmt.Errorf("invalid type pattern in rule %d: %w", i, err)
        }
        
        // ID is optional (empty = match all)
        compiled.idMatcher, err = parsePatternOrMatchAll(rule.ID)
        if err != nil {
            return nil, fmt.Errorf("invalid id pattern in rule %d: %w", i, err)
        }
        
        // Method is optional (empty = match all)
        compiled.methodMatcher, err = parsePatternOrMatchAll(rule.Method)
        if err != nil {
            return nil, fmt.Errorf("invalid method pattern in rule %d: %w", i, err)
        }
        
        // Access is required
        switch rule.Access {
        case AccessReject, AccessInternal, AccessExternal, AccessAllow:
            // OK
        case "":
            return nil, fmt.Errorf("missing access in rule %d", i)
        default:
            return nil, fmt.Errorf("invalid access level in rule %d: %q", i, rule.Access)
        }
        
        v.rules = append(v.rules, compiled)
    }
    
    return v, nil
}

// CheckClientAccess checks if a client (via Gate) can access the object.
// Returns nil if allowed, error if denied.
func (v *AccessValidator) CheckClientAccess(objectType, objectID, method string) error {
    access := v.findAccess(objectType, objectID, method)
    
    if access != AccessAllow && access != AccessExternal {
        return fmt.Errorf("access denied for %s/%s method %q", objectType, objectID, method)
    }
    return nil
}

// CheckNodeAccess checks if a node (object-to-object) can access the object.
// Returns nil if allowed, error if denied.
func (v *AccessValidator) CheckNodeAccess(objectType, objectID, method string) error {
    access := v.findAccess(objectType, objectID, method)
    
    if access == AccessReject || access == AccessExternal {
        return fmt.Errorf("access denied for %s/%s method %q", objectType, objectID, method)
    }
    return nil
}

// findAccess evaluates rules top-to-bottom and returns the access level.
// Returns REJECT if no rule matches (default deny).
func (v *AccessValidator) findAccess(objectType, objectID, method string) string {
    for _, rule := range v.rules {
        if rule.typeMatcher.Match(objectType) && 
           rule.idMatcher.Match(objectID) &&
           rule.methodMatcher.Match(method) {
            return rule.access
        }
    }
    // Default: deny
    return AccessReject
}
```

#### Gate integration

```go
// gate/gateserver/gate_server.go

type GateServer struct {
    // ... existing fields
    accessValidator *config.AccessValidator
}

func (g *GateServer) handleCallObject(ctx context.Context, req *CallObjectRequest) (*CallObjectResponse, error) {
    if g.accessValidator != nil {
        // Check client access - requires ALLOW or EXTERNAL
        if err := g.accessValidator.CheckClientAccess(req.ObjectType, req.ObjectID, req.Method); err != nil {
            return nil, status.Errorf(codes.PermissionDenied, "%v", err)
        }
    }
    
    // Forward to node...
}
```

#### Node integration

```go
// node/node.go

// For requests coming from gate (already validated by gate)
func (n *Node) handleGateRequest(ctx context.Context, objectType, objectID, method string) error {
    // Gate already validated client access
    // Node can optionally re-validate as defense in depth
    // ...
}

// For requests coming from other nodes (object-to-object calls)
func (n *Node) handleNodeRequest(ctx context.Context, objectType, objectID, method string) error {
    if n.accessValidator != nil {
        // Check node access - allows ALLOW or INTERNAL
        if err := n.accessValidator.CheckNodeAccess(objectType, objectID, method); err != nil {
            return fmt.Errorf("%w", err)
        }
    }
    // ...
}
```

### 4.5 CLI Mode (No Config File)

When running without a config file (CLI mode), access rules can be:

1. **Disabled** (default) - allow all objects (backward compatible)
2. **Specified via flag** - simple patterns only

```bash
# No validation (backward compatible)
go run . --listen :48000 --advertise localhost:48000

# With basic pattern (optional enhancement)
go run . --listen :48000 --access-rules "ChatRoom-.*:.*:ALLOW"
```

For production, using a config file is recommended.

## 5. Error Messages

Clear error messages help developers understand access control failures:

| Scenario | Error Message |
|----------|---------------|
| No rule matches | `access denied for Foo/123 method "Bar"` |
| Rule matches with REJECT | `access denied for Internal/scheduler-1 method "Run"` |
| Rule matches with INTERNAL (client) | `access denied for ChatRoom/General method "NotifyMembers"` |

## 6. Examples

### Chat Application

```yaml
object_access_rules:
  # ChatRoomMgr singleton: clients can list rooms
  - type: ChatRoomMgr
    id: ChatRoomMgr0
    method: ListChatRooms
    access: ALLOW
  
  # ChatRoomMgr: all other methods internal only
  - type: ChatRoomMgr
    access: INTERNAL

  # ChatRoom: clients can interact with these methods
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    method: /(Join|Leave|SendMessage|GetMessages)/
    access: ALLOW
  
  # ChatRoom: other methods (NotifyMembers, etc.) internal only
  - type: ChatRoom
    access: INTERNAL

  # Default: deny everything else
  - type: /.*/
    access: REJECT
```

**Client (via Gate) can:**
- `CallObject("ChatRoomMgr", "ChatRoomMgr0", "ListChatRooms", ...)` ✓
- `CallObject("ChatRoom", "General", "Join", ...)` ✓
- `CallObject("ChatRoom", "General", "SendMessage", ...)` ✓

**Client cannot:**
- `CallObject("ChatRoom", "General", "NotifyMembers", ...)` ✗ (INTERNAL)
- `CallObject("InternalScheduler", "sched-1", "Run", ...)` ✗ (no matching rule → REJECT)

**Node (object-to-object) can:**
- `CallObject("ChatRoom", "General", "NotifyMembers", ...)` ✓ (INTERNAL)
- `CallObject("ChatRoom", "General", "SendMessage", ...)` ✓ (ALLOW)

### Internal-only Objects

```yaml
object_access_rules:
  # Scheduler: only accessible by other objects, not clients
  - type: InternalScheduler
    access: INTERNAL
  
  # Metrics collector: read-only for clients
  - type: MetricsCollector
    method: GetMetrics
    access: ALLOW
  
  # Metrics collector: write methods for nodes only
  - type: MetricsCollector
    method: /(RecordMetric|Reset)/
    access: INTERNAL

  # Default deny
  - type: /.*/
    access: REJECT
```

### Development Mode (Allow All)

```yaml
object_access_rules:
  - type: /.*/
    access: ALLOW
```

**Warning**: Only use in development. All types and methods are allowed.

## 7. Object Lifecycle Rules

### 7.1 Problem Statement

The `object_access_rules` section controls **method access** - which clients and nodes can call specific methods on objects. However, lifecycle operations (CREATE and DELETE) are fundamentally different:

- **CREATE**: Triggered implicitly when calling an object that doesn't exist (auto-creation) or explicitly via `CreateObject` calls
- **DELETE**: Explicitly requested to remove an object from the system

These operations need **separate control** because:

1. **Different security model**: Creating objects may be permissive (clients can create game sessions), while deleting should be restrictive (only internal cleanup processes)
2. **Inheritance issues**: Method rules shouldn't automatically grant lifecycle permissions - a client allowed to call `Join()` shouldn't automatically be able to delete the object
3. **Resource protection**: Explicit lifecycle rules prevent accidental or malicious object creation/deletion
4. **Audit clarity**: Separate rules make it clear which entities can affect object existence

### 7.2 Configuration Format

Add `object_lifecycle_rules` section alongside `object_access_rules`:

```yaml
# Object lifecycle rules (CREATE/DELETE operations)
# Evaluated top-to-bottom, first match wins
# Each rule: type (required), id (optional), lifecycle (required), access (required)
# Pattern matching: same as object_access_rules (literal or /regexp/)
# Access levels: REJECT, INTERNAL, EXTERNAL, ALLOW
#
# Default behavior (if no rule matches):
#   - CREATE: ALLOW (objects can be created by anyone)
#   - DELETE: INTERNAL (only nodes can delete objects)
object_lifecycle_rules:
  # ChatRoom: clients can create rooms with valid IDs
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    lifecycle: CREATE
    access: ALLOW

  # ChatRoom: only nodes can delete rooms (cleanup processes)
  - type: ChatRoom
    lifecycle: DELETE
    access: INTERNAL

  # InternalScheduler: only nodes can create/delete (using ALL)
  - type: InternalScheduler
    lifecycle: ALL
    access: INTERNAL

  # Singleton objects: reject creation and deletion (using ALL)
  - type: ConfigManager
    id: ConfigManager0
    lifecycle: ALL
    access: REJECT

  # Temporary objects: clients can create and delete (using ALL)
  - type: TempSession
    id: /session-[a-zA-Z0-9]+/
    lifecycle: ALL
    access: ALLOW
```

### 7.3 Rule Format

| Field | Required | Description |
|-------|----------|-------------|
| `type` | Yes | Object type. Literal string for exact match, or `/regexp/` for regex. |
| `id` | No | Object ID pattern. Omit to match all IDs. Literal or `/regexp/`. |
| `lifecycle` | Yes | Lifecycle operation: `CREATE`, `DELETE`, or `ALL` |
| `access` | Yes | Access level: `REJECT`, `INTERNAL`, `EXTERNAL`, or `ALLOW` |

**Lifecycle operations:**

| Operation | Description | Trigger |
|-----------|-------------|---------|
| `CREATE` | Object instantiation | `CallObject` to non-existent object (auto-creation) or explicit `CreateObject` |
| `DELETE` | Object removal | Explicit `DeleteObject` call |
| `ALL` | Both CREATE and DELETE | Applies to both object creation and deletion |

### 7.4 Access Level Semantics

Access levels work the same as method rules:

| Level | Client (via Gate) | Node (object-to-object) |
|-------|-------------------|-------------------------|
| `REJECT` | ✗ Denied | ✗ Denied |
| `INTERNAL` | ✗ Denied | ✓ Allowed |
| `EXTERNAL` | ✓ Allowed | ✗ Denied |
| `ALLOW` | ✓ Allowed | ✓ Allowed |

### 7.5 Default Behavior

**If no lifecycle rule matches:**

| Operation | Default Access | Rationale |
|-----------|---------------|-----------|
| `CREATE` | `ALLOW` | Permissive for backward compatibility; objects can be created freely |
| `DELETE` | `INTERNAL` | Restrictive for safety; only nodes can delete objects |

This default ensures:
- Existing deployments continue to work without lifecycle rules
- Object deletion is protected by default (requires explicit rules to allow client deletion)
- Gradual adoption is possible

### 7.6 Evaluation Order

1. Rules are evaluated **top-to-bottom**
2. **First matching rule wins** (type, id, AND lifecycle must all match)
3. If no rule matches, **default behavior applies** (CREATE=ALLOW, DELETE=INTERNAL)
4. Order matters - put specific rules before general ones

### 7.7 Examples

#### Allow client CREATE, deny client DELETE (common pattern)

```yaml
object_lifecycle_rules:
  # GameSession: clients can create new sessions
  - type: GameSession
    id: /game-[a-zA-Z0-9]+/
    lifecycle: CREATE
    access: ALLOW

  # GameSession: only internal cleanup can delete
  - type: GameSession
    lifecycle: DELETE
    access: INTERNAL
```

#### Internal-only objects

```yaml
object_lifecycle_rules:
  # Scheduler: only nodes can create and delete (using ALL)
  - type: InternalScheduler
    lifecycle: ALL
    access: INTERNAL
```

#### Singleton objects (pre-created, no dynamic creation/deletion)

```yaml
object_lifecycle_rules:
  # ConfigManager singleton: cannot be created or deleted dynamically (using ALL)
  - type: ConfigManager
    lifecycle: ALL
    access: REJECT
```

#### Temporary objects (clients can fully manage)

```yaml
object_lifecycle_rules:
  # TempSession: clients can create and delete their sessions (using ALL)
  - type: TempSession
    id: /temp-[a-zA-Z0-9]+/
    lifecycle: ALL
    access: ALLOW
```

#### Block all creation except whitelisted types

```yaml
object_lifecycle_rules:
  # Allow specific types
  - type: ChatRoom
    lifecycle: CREATE
    access: ALLOW
  - type: Counter
    lifecycle: CREATE
    access: ALLOW

  # Block all other creation
  - type: /.*/
    lifecycle: CREATE
    access: REJECT
```

### 7.8 Enforcement Flow

#### Gate-side (CREATE via auto-creation)

```
┌─────────┐    CallObject("ChatRoom-General", "Join", ...)
│  Client │ ────────────────────────────────────────────────►
└─────────┘         (object doesn't exist → auto-create)
                              │
                              ▼
                    ┌─────────────────────┐
                    │        Gate         │
                    │                     │
                    │ 1. Check method     │
                    │    access rules     │
                    │                     │
                    │ 2. If object will   │
                    │    be created:      │
                    │    Check lifecycle  │
                    │    CREATE rules     │
                    └──────────┬──────────┘
                               │
       ┌───────────────┬───────┴───────┬───────────────┐
       │               │               │               │
       ▼               ▼               ▼               ▼
    REJECT         INTERNAL        EXTERNAL         ALLOW
       │               │               │               │
       ▼               ▼               ▼               ▼
  Return error    Return error   Forward to Node  Forward to Node
```

#### Gate-side (explicit DELETE)

```
┌─────────┐    DeleteObject("ChatRoom-General")
│  Client │ ──────────────────────────────────────►
└─────────┘
                              │
                              ▼
                    ┌─────────────────────┐
                    │        Gate         │
                    │                     │
                    │ Check lifecycle     │
                    │ DELETE rules        │
                    └──────────┬──────────┘
                               │
       ┌───────────────┬───────┴───────┬───────────────┐
       │               │               │               │
       ▼               ▼               ▼               ▼
    REJECT         INTERNAL        EXTERNAL         ALLOW
       │               │               │               │
       ▼               ▼               ▼               ▼
  Return error    Return error   Forward to Node  Forward to Node
```

#### Node-side (object-to-object CREATE/DELETE)

```
┌─────────┐    CreateObject/DeleteObject (object-to-object)
│  Node   │ ─────────────►  ┌─────────────────┐
│ (obj A) │                 │      Node       │
└─────────┘                 │    (target)     │
                            │                 │
                            │ Check lifecycle │
                            │ rules for       │
                            │ CREATE/DELETE   │
                            │                 │
                            │ ALLOW or        │
                            │ INTERNAL:       │
                            │   Proceed       │
                            │                 │
                            │ REJECT or       │
                            │ EXTERNAL:       │
                            │   Return error  │
                            └─────────────────┘
```

### 7.9 Integration with Method Rules

Lifecycle rules and method rules are **evaluated independently**:

1. **Method call to existing object**: Only method rules apply
2. **Method call triggering auto-creation**: Both lifecycle (CREATE) and method rules apply
3. **Explicit CreateObject**: Only lifecycle (CREATE) rules apply
4. **Explicit DeleteObject**: Only lifecycle (DELETE) rules apply

Example: A client calling `ChatRoom.Join()` on a new room:
1. Check CREATE lifecycle rule → client access requires ALLOW or EXTERNAL
2. Check method access rule for `Join` → client access requires ALLOW or EXTERNAL
3. Both checks must individually pass for the client request to succeed

### 7.10 Migration Path

#### For existing deployments (no lifecycle rules)

Without `object_lifecycle_rules`, the default behavior applies:
- CREATE: ALLOW (any entity can create objects)
- DELETE: INTERNAL (only nodes can delete objects)

This provides backward compatibility - existing applications continue to work.

#### Gradual adoption steps

1. **Phase 1**: Deploy without lifecycle rules (default behavior)
2. **Phase 2**: Add lifecycle rules for sensitive object types (internal-only)
3. **Phase 3**: Add explicit CREATE rules for client-facing types
4. **Phase 4**: Add explicit DELETE rules where needed
5. **Phase 5**: Optionally add catch-all rules to enforce explicit configuration

```yaml
# Phase 5: Explicit configuration for all types
object_lifecycle_rules:
  # Specific rules first...
  - type: ChatRoom
    lifecycle: CREATE
    access: ALLOW
  # ...

  # Catch-all: use defaults explicitly
  - type: /.*/
    lifecycle: CREATE
    access: ALLOW      # Same as default
  - type: /.*/
    lifecycle: DELETE
    access: INTERNAL   # Same as default
```

### 7.11 Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| No `object_lifecycle_rules` section | Defaults apply: CREATE=ALLOW, DELETE=INTERNAL |
| Empty `object_lifecycle_rules` list | Defaults apply: CREATE=ALLOW, DELETE=INTERNAL |
| Partial rules (some types only) | Specified types use rules, others use defaults |
| Full rules with catch-all | All operations governed by explicit rules |

This ensures:
- Zero disruption for existing deployments
- Opt-in security hardening
- Clear path to full lifecycle control

## 8. Security Considerations

1. **Default deny**: End the rule list with `type: /.*/`, `access: REJECT`
2. **Specific rules first**: Put narrow rules before broad ones (first match wins)
3. **Minimal client access**: Use `ALLOW` only for methods clients truly need
4. **Internal objects**: Use `INTERNAL` for internal-only object types
5. **Restrictive ID patterns**: Be specific with ID patterns (include length limits)
6. **Rule ordering**: Review rule order carefully - a broad early rule can override later specific rules
7. **Audit regularly**: Review access rules for overly permissive config
8. **Lifecycle protection**: Use explicit lifecycle rules for sensitive object types to control creation/deletion separately from method access

## 9. Migration Path

1. **Phase 1**: Add config parsing, no enforcement (logging only)
2. **Phase 2**: Enforce on gates (client access)
3. **Phase 3**: Enforce on nodes (node access, defense in depth)

For existing deployments:
- Start with a single rule: `type: /.*/`, `access: ALLOW`
- Add specific `INTERNAL` rules for internal-only object types
- Add specific `ALLOW` rules for client-accessible methods
- Remove the catch-all ALLOW rule and add `access: REJECT` at the end
- Test thoroughly at each step

## 10. Future Enhancements

- **Rate limiting**: Limit requests per client/type/method
- **Dynamic reload**: Update rules without restart
- **Metrics**: Track access successes/failures per type/method/access-level
- **Per-client rules**: Different access based on client identity
- **Argument validation**: Validate method arguments

## 11. Summary

Config-based object access control provides:

- **Linear rule list**: Rules evaluated top-to-bottom, first match wins
- **Separate type/id/method**: Match object type (required), ID (optional), and method (optional)
- **Four access levels**: `REJECT`, `INTERNAL`, `EXTERNAL`, `ALLOW`
- **Flexible patterns**: Literal strings for exact match, `/regexp/` for regex (auto-anchored)
- **Optional fields**: Omit `id` or `method` to match all
- **Client access control**: Gate enforces `ALLOW` or `EXTERNAL` for client requests
- **Node access control**: Nodes enforce `ALLOW` or `INTERNAL` for object-to-object calls
- **Gate filtering**: Reject invalid requests before they reach nodes
- **Secure by default**: No matching rule = deny (for method rules)
- **Lifecycle control**: Separate `object_lifecycle_rules` for CREATE/DELETE operations
- **Lifecycle defaults**: CREATE=ALLOW, DELETE=INTERNAL (for backward compatibility)
