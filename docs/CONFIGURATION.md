# GoVerse Configuration File Reference

This document provides a complete reference for the GoVerse configuration file, including all configuration options for cluster setup, node and gate definitions, object access control, and lifecycle rules.

## Table of Contents

- [Overview](#overview)
- [File Format](#file-format)
- [Configuration Schema](#configuration-schema)
  - [version](#version)
  - [cluster](#cluster)
  - [postgres](#postgres)
  - [inspector](#inspector)
  - [nodes](#nodes)
  - [gates](#gates)
  - [object_access_rules](#object_access_rules)
  - [object_lifecycle_rules](#object_lifecycle_rules)
- [Pattern Matching](#pattern-matching)
- [Access Levels](#access-levels)
- [Complete Examples](#complete-examples)
- [Validation](#validation)
- [Best Practices](#best-practices)

---

## Overview

GoVerse uses YAML configuration files to define:

- **Cluster topology**: etcd connectivity and shard configuration
- **Node definitions**: gRPC servers that host objects
- **Gate definitions**: Entry points for client connections
- **Inspector integration**: Optional monitoring and debugging service
- **Object access control**: Fine-grained control over method access
- **Lifecycle rules**: Control over object creation and deletion

Configuration is loaded at startup via `config.LoadConfig(path)` and validated before use.

---

## File Format

Configuration files use YAML format. A minimal configuration requires:

```yaml
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

# Optional: Inspector service configuration
# inspector:
#   grpc_addr: "127.0.0.1:8081"
#   http_addr: "127.0.0.1:8080"
#   advertise_addr: "localhost:8081"

nodes:
  - id: "node-1"
    grpc_addr: "127.0.0.1:50051"
    advertise_addr: "127.0.0.1:50051"

gates: []
```

---

## Configuration Schema

### version

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | int | Yes | Configuration schema version. Must be `1`. |

```yaml
version: 1
```

---

### cluster

Cluster-level configuration for distributed coordination.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `shards` | int | Yes | Number of shards for object distribution. Recommended: `8192`. |
| `provider` | string | Yes | Cluster coordination provider. Currently only `"etcd"` is supported. |
| `etcd` | object | Yes | etcd-specific configuration. |

#### cluster.etcd

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `endpoints` | array[string] | Yes | List of etcd endpoints. At least one required. |
| `prefix` | string | Yes | Key prefix for all GoVerse keys in etcd. |

```yaml
cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-1.local:2379"
      - "etcd-2.local:2379"
      - "etcd-3.local:2379"
    prefix: "/goverse/production"
```

---

### postgres

Optional PostgreSQL configuration for persistence.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `host` | string | No | - | Database host |
| `port` | int | No | - | Database port |
| `user` | string | No | - | Database user |
| `password` | string | No | - | Database password (use environment variables for secrets) |
| `database` | string | No | - | Database name |
| `sslmode` | string | No | - | SSL mode (`"disable"`, `"require"`, etc.) |

```yaml
postgres:
  host: "db.internal.local"
  port: 5432
  user: "goverse"
  password: ""  # Use environment variable
  database: "goverse"
  sslmode: "require"
```

---

### inspector

Optional inspector service configuration for monitoring and debugging.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `grpc_addr` | string | No | - | gRPC server address for inspector API |
| `http_addr` | string | No | - | HTTP server address for inspector web UI |
| `advertise_addr` | string | No | - | Address that nodes and gates use to connect to inspector gRPC service |

```yaml
inspector:
  grpc_addr: "10.0.3.10:8081"
  http_addr: "10.0.3.10:8080"
  advertise_addr: "inspector.cluster.example.com:8081"
```

**Notes:**
- The `inspector` section is entirely optional. Omit it to disable inspector integration.
- `grpc_addr`: Address where the inspector gRPC server listens (used for serving the inspector API)
- `http_addr`: Address where the inspector HTTP server listens (used for the web UI)
- `advertise_addr`: Address that nodes and gates use to connect to the inspector service. This is typically an advertised address that is reachable from all nodes and gates.
- When using config files with `--config`, nodes and gates automatically read `advertise_addr` from the inspector section.
- In CLI-only mode (without `--config`), use `--inspector-address` flag to specify the inspector connection address.

---

### nodes

Array of node configurations. Each node hosts objects and handles RPCs.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique node identifier |
| `grpc_addr` | string | Yes | gRPC listen address (e.g., `"0.0.0.0:50051"`) |
| `advertise_addr` | string | Yes | Address other nodes use to reach this node |
| `http_addr` | string | No | HTTP address for admin/metrics endpoints |

```yaml
nodes:
  - id: "node-a"
    grpc_addr: "10.0.1.10:50051"
    advertise_addr: "node-a.cluster.example.com:50051"
    http_addr: "10.0.1.10:8080"

  - id: "node-b"
    grpc_addr: "10.0.1.11:50051"
    advertise_addr: "node-b.cluster.example.com:50051"
    http_addr: "10.0.1.11:8080"
```

**Validation rules:**
- Node IDs must be unique
- `id`, `grpc_addr`, and `advertise_addr` are required

---

### gates

Array of gate configurations. Gates are entry points for client connections.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique gate identifier |
| `grpc_addr` | string | Yes | gRPC listen address for client connections |
| `advertise_addr` | string | No | Advertised address for the gate |
| `http_addr` | string | No | HTTP address for admin/metrics endpoints |

```yaml
gates:
  - id: "gate-1"
    grpc_addr: "10.0.2.10:60051"
    advertise_addr: "gate-1.cluster.example.com:60051"
    http_addr: "10.0.2.10:8081"
```

**Validation rules:**
- Gate IDs must be unique
- `id` and `grpc_addr` are required

---

### object_access_rules

Controls which clients and nodes can call specific methods on objects. Rules are evaluated **top-to-bottom**, and the **first matching rule wins**.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Object type pattern (literal or `/regexp/`) |
| `id` | string | No | Object ID pattern. Omit to match all IDs. |
| `method` | string | No | Method name pattern. Omit to match all methods. |
| `access` | string | Yes | Access level: `REJECT`, `INTERNAL`, `EXTERNAL`, or `ALLOW` |

**Default behavior:** If no rule matches, access is **DENIED** (secure by default).

#### Example: Chat Application Access Rules

```yaml
object_access_rules:
  # ChatRoomMgr singleton: clients can list rooms
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

---

### object_lifecycle_rules

Controls which clients and nodes can create or delete objects. Separate from method access rules.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Object type pattern (literal or `/regexp/`) |
| `id` | string | No | Object ID pattern. Omit to match all IDs. |
| `lifecycle` | string | Yes | Lifecycle operation: `CREATE`, `DELETE`, or `ALL` |
| `access` | string | Yes | Access level: `REJECT`, `INTERNAL`, `EXTERNAL`, or `ALLOW` |

**Default behavior (if no rule matches):**
- `CREATE`: **ALLOW** (objects can be created by anyone)
- `DELETE`: **INTERNAL** (only nodes can delete objects)

#### Lifecycle Operations

| Operation | Description | Triggers |
|-----------|-------------|----------|
| `CREATE` | Object instantiation | Auto-creation on `CallObject` to non-existent object, or explicit `CreateObject` |
| `DELETE` | Object removal | Explicit `DeleteObject` call |
| `ALL` | Both CREATE and DELETE | Applies to both operations |

#### Example: Lifecycle Rules

```yaml
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

  # InternalScheduler: only nodes can create/delete
  - type: InternalScheduler
    lifecycle: ALL
    access: INTERNAL

  # Singleton objects: reject creation and deletion
  - type: ConfigManager
    id: ConfigManager0
    lifecycle: ALL
    access: REJECT

  # Temporary objects: clients can create and delete
  - type: TempSession
    id: /session-[a-zA-Z0-9]+/
    lifecycle: ALL
    access: ALLOW
```

---

## Pattern Matching

Both access rules and lifecycle rules support two pattern matching modes:

### Literal Matching

Exact string match. Use for specific values.

```yaml
- type: ChatRoomMgr        # Matches exactly "ChatRoomMgr"
  id: ChatRoomMgr0         # Matches exactly "ChatRoomMgr0"
  method: ListChatRooms    # Matches exactly "ListChatRooms"
```

### Regex Matching

Patterns enclosed in `/.../' are treated as regular expressions. **Patterns are automatically anchored** to match the full string (equivalent to `^...$`).

```yaml
- type: /Chat.*/              # Matches "ChatRoom", "ChatRoomMgr", etc.
  id: /[a-zA-Z0-9_-]{1,50}/   # Alphanumeric IDs, 1-50 chars
  method: /(Join|Leave)/      # Matches "Join" or "Leave"
```

### Match All (Omit Field)

Omitting an optional field (`id` or `method`) matches all values:

```yaml
- type: InternalScheduler     # Matches all IDs
  access: INTERNAL            # Matches all methods
```

Equivalent to:

```yaml
- type: InternalScheduler
  id: /.*/
  method: /.*/
  access: INTERNAL
```

### Common Patterns

| Pattern | Description | Examples |
|---------|-------------|----------|
| `/.*/` | Match any string | Any value |
| `/[a-zA-Z0-9_-]+/` | Alphanumeric with underscore/dash | `user-123`, `room_abc` |
| `/[a-zA-Z0-9_-]{1,50}/` | Same, limited to 1-50 chars | Valid IDs with length check |
| `/user-[0-9]+/` | Prefix with numeric suffix | `user-123`, `user-9999` |
| `/(Get|Set|Delete)/` | Specific methods | `Get`, `Set`, or `Delete` |

---

## Access Levels

Four access levels control who can perform operations:

| Level | Client (via Gate) | Node (object-to-object) | Use Case |
|-------|-------------------|-------------------------|----------|
| `REJECT` | ✗ Denied | ✗ Denied | Block all access |
| `INTERNAL` | ✗ Denied | ✓ Allowed | Internal-only methods/objects |
| `EXTERNAL` | ✓ Allowed | ✗ Denied | Client-only methods (rare) |
| `ALLOW` | ✓ Allowed | ✓ Allowed | Public methods |

### Access Level Decision Matrix

**For method access (`object_access_rules`):**

| Rule Access | Client Request | Node Request |
|-------------|----------------|--------------|
| REJECT | Denied | Denied |
| INTERNAL | Denied | Allowed |
| EXTERNAL | Allowed | Denied |
| ALLOW | Allowed | Allowed |
| (No match) | **Denied** | **Denied** |

**For lifecycle operations (`object_lifecycle_rules`):**

| Rule Access | Client Request | Node Request |
|-------------|----------------|--------------|
| REJECT | Denied | Denied |
| INTERNAL | Denied | Allowed |
| EXTERNAL | Allowed | Denied |
| ALLOW | Allowed | Allowed |
| (No CREATE match) | **Allowed** | **Allowed** |
| (No DELETE match) | **Denied** | **Allowed** |

---

## Complete Examples

### Single-Node Development

```yaml
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

nodes:
  - id: "dev-node"
    grpc_addr: "127.0.0.1:50051"
    advertise_addr: "127.0.0.1:50051"
    http_addr: "127.0.0.1:8080"

gates: []

# Development: allow all access
object_access_rules:
  - type: /.*/
    access: ALLOW
```

### Production Multi-Node with Full Access Control

```yaml
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-1.prod.local:2379"
      - "etcd-2.prod.local:2379"
      - "etcd-3.prod.local:2379"
    prefix: "/goverse/production"

nodes:
  - id: "node-a"
    grpc_addr: "10.0.1.10:50051"
    advertise_addr: "node-a.cluster.example.com:50051"
    http_addr: "10.0.1.10:8080"

  - id: "node-b"
    grpc_addr: "10.0.1.11:50051"
    advertise_addr: "node-b.cluster.example.com:50051"
    http_addr: "10.0.1.11:8080"

gates:
  - id: "gate-1"
    grpc_addr: "10.0.2.10:60051"

# Object access control rules
object_access_rules:
  # Public API methods
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    method: /(Join|Leave|SendMessage|GetMessages)/
    access: ALLOW

  # Internal methods
  - type: ChatRoom
    method: /.*/
    access: INTERNAL

  # Internal-only objects
  - type: InternalScheduler
    access: INTERNAL

  # Default deny
  - type: /.*/
    access: REJECT

# Object lifecycle rules
object_lifecycle_rules:
  # ChatRoom: clients can create, only nodes can delete
  - type: ChatRoom
    id: /[a-zA-Z0-9_-]{1,50}/
    lifecycle: CREATE
    access: ALLOW

  - type: ChatRoom
    lifecycle: DELETE
    access: INTERNAL

  # Internal objects: only nodes can manage
  - type: InternalScheduler
    lifecycle: ALL
    access: INTERNAL

  # Singletons: cannot be created or deleted dynamically
  - type: ConfigManager
    lifecycle: ALL
    access: REJECT
```

---

## Validation

Configuration is validated at load time. Common validation errors:

| Error | Cause | Fix |
|-------|-------|-----|
| `unsupported config version` | Version is not 1 | Set `version: 1` |
| `cluster provider is required` | Missing provider | Add `provider: "etcd"` |
| `cluster shards must be specified and positive` | Missing or zero shards | Set `shards: 8192` |
| `at least one etcd endpoint is required` | Empty endpoints list | Add etcd endpoints |
| `etcd prefix is required` | Missing prefix | Add `prefix: "/goverse"` |
| `node X: id is required` | Missing node ID | Add unique `id` |
| `node X: grpc_addr is required` | Missing gRPC address | Add `grpc_addr` |
| `node X: advertise_addr is required` | Missing advertise address | Add `advertise_addr` |
| `duplicate node id: X` | Non-unique node ID | Use unique IDs |
| `gate X: id is required` | Missing gate ID | Add unique `id` |
| `gate X: grpc_addr is required` | Missing gRPC address | Add `grpc_addr` |
| `duplicate gate id: X` | Non-unique gate ID | Use unique IDs |

For access/lifecycle rules:

| Error | Cause | Fix |
|-------|-------|-----|
| `missing type in rule N` | Rule without type | Add `type` field |
| `invalid type pattern in rule N` | Invalid regex | Fix regex syntax |
| `missing access in rule N` | Rule without access | Add `access` field |
| `invalid access level in rule N` | Unknown access value | Use REJECT/INTERNAL/EXTERNAL/ALLOW |
| `missing lifecycle in rule N` | Lifecycle rule without operation | Add `lifecycle` field |
| `invalid lifecycle in rule N` | Unknown lifecycle value | Use CREATE/DELETE/ALL |

---

## Best Practices

### Security

1. **End with default deny**: Always end access rules with:
   ```yaml
   - type: /.*/
     access: REJECT
   ```

2. **Put specific rules first**: Rules are evaluated top-to-bottom; first match wins.

3. **Restrict ID patterns**: Use length limits and character restrictions:
   ```yaml
   id: /[a-zA-Z0-9_-]{1,50}/  # Better than /.*/
   ```

4. **Use INTERNAL for system objects**: Keep internal methods/objects protected:
   ```yaml
   - type: InternalScheduler
     access: INTERNAL
   ```

5. **Control lifecycle separately**: Don't assume method access implies lifecycle control:
   ```yaml
   object_lifecycle_rules:
     - type: ChatRoom
       lifecycle: DELETE
       access: INTERNAL  # Even if clients can call methods
   ```

### Operations

1. **Use multiple etcd endpoints**: For high availability:
   ```yaml
   etcd:
     endpoints:
       - "etcd-1:2379"
       - "etcd-2:2379"
       - "etcd-3:2379"
   ```

2. **Separate advertise addresses**: Use resolvable hostnames:
   ```yaml
   grpc_addr: "0.0.0.0:50051"           # Bind to all interfaces
   advertise_addr: "node-1.local:50051"  # Resolvable by other nodes
   ```

3. **Use unique prefixes**: For multi-tenant etcd:
   ```yaml
   prefix: "/goverse/production"  # vs "/goverse/staging"
   ```

4. **Keep secrets separate**: Use environment variables for sensitive data:
   ```yaml
   postgres:
     password: ""  # Set via GOVERSE_DB_PASSWORD env var
   ```

### Testing

1. **Development mode**: Allow all for local testing:
   ```yaml
   object_access_rules:
     - type: /.*/
       access: ALLOW
   ```

2. **Use fewer shards in tests**: For faster test execution:
   ```yaml
   cluster:
     shards: 64  # Test only; use 8192 in production
   ```

---

## See Also

- [Object Access Control Design](design/OBJECT_ACCESS_CONTROL.md) - Detailed design document
- [Config Design](config-design.md) - Design rationale and schema details
- [Example Configs](../config/examples/) - Working configuration examples
