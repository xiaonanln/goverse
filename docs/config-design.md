# GoVerse Configuration File — Design Document

Status: Draft  
Author: @copilot (for xiaonanln/goverse)  
Date: 2025-11-28

## Overview

This document describes the design and rationale of the GoVerse configuration file format, validation rules, runtime behavior, and operational considerations. The configuration file is the authoritative source used by the GoVerse binary to configure cluster topology (nodes and gates), etcd connectivity, and cluster-wide options such as number of shards.

Goals:
- Provide a simple, human-editable YAML format.
- Validate early and fail-fast on invalid configuration.
- Map directly to the Go structs implemented in the repository.
- Be extensible and versioned to allow future changes without breaking existing deployments.
- Keep secrets out of committed config files (support separate secrets management).

Scope:
- File format and schema for the primary cluster configuration used by the GoVerse runtime.
- Validation rules and runtime mapping implemented by config.LoadConfig and Config.Validate.
- Operational recommendations (secrets, versioning, reloads).

References:
- Primary config code: [config/config.go](../config/config.go)
- Cluster-level defaults: [cluster/config.go](../cluster/config.go)
- Postgres helper config (separate): [util/postgres/config.go](../util/postgres/config.go)

## File format and top-level schema

Use YAML. Top-level fields:
- version (int) — configuration schema version
- cluster (object) — cluster-level configuration
- nodes (array) — node definitions
- gates (array) — gate definitions

Canonical example:
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
  - id: "node-1"
    grpc_addr: "127.0.0.1:50051"
    advertise_addr: "node-1.local:50051"
    http_addr: "127.0.0.1:8080"

gates:
  - id: "gate-1"
    grpc_addr: "127.0.0.1:60051"
```

Note: Use the YAML mapping keys exactly as shown to match the tags in config/config.go.

## Field-level description, types, defaults and validation

### Top-level

- **version** (int)
  - Required.
  - Current supported value: 1.
  - Validation: config.Validate returns an error if version != 1.

### cluster (object)

- **shards** (int)
  - Required and must be > 0.
  - Semantic: total number of shards used for distributing actor/grain identity.
  - Practical default used in other code: 8192 (see cluster.DefaultConfig). Although config/config.go requires a positive value, if omitted the operator should set it; recommended default in example: 8192.
  - Validation error message: "cluster shards must be specified and positive"

- **provider** (string)
  - Required.
  - Currently only "etcd" is supported.
  - Validation error message if empty: "cluster provider is required"
  - Validation error for unsupported: "unsupported cluster provider: %s (only 'etcd' is supported)"

- **etcd** (object)
  - **endpoints** (array[string])
    - Required: at least one entry.
    - Validation error: "at least one etcd endpoint is required"
    - The helper Config.GetEtcdAddress() uses the first endpoint if present.
  - **prefix** (string)
    - Required, non-empty.
    - Validation error: "etcd prefix is required"

### NodeConfig (nodes array elements)

- **id** (string)
  - Required and must be unique across nodes.
  - Validation error: "node %d: id is required" or "duplicate node id: %s"

- **grpc_addr** (string)
  - Required. The gRPC listen address used by the node for internal RPCs.
  - Validation error: "node %s: grpc_addr is required"

- **advertise_addr** (string)
  - Required. The external address other nodes will use to reach this node.
  - Validation error: "node %s: advertise_addr is required"

- **http_addr** (string)
  - Optional. For admin/metrics endpoints.

### GateConfig (gates array elements)

- **id** (string)
  - Required and must be unique across gates.
  - Validation error: "gate %d: id is required" or "duplicate gate id: %s"

- **grpc_addr** (string)
  - Required. gRPC address of the gate component.
  - Validation error: "gate %s: grpc_addr is required"

## Validation behavior

- All validation is performed by Config.Validate() called by LoadConfig(path).
- LoadConfig:
  - Reads file, unmarshals with yaml.v3, runs Validate, and returns error-wrapped messages to the caller.

## Runtime helper functions (mapping)

- **GetEtcdAddress()** returns first configured endpoint or empty string.
- **GetEtcdPrefix()** returns cluster.etcd.prefix.
- **GetNumShards()** returns cluster.shards.

## Cluster-level runtime defaults (separate cluster package)

cluster.Config defines additional cluster runtime parameters (MinQuorum, ClusterStateStabilityDuration, ShardMappingCheckInterval, NumShards). See cluster/config.go:
- Defaults: MinQuorum=1, ClusterStateStabilityDuration=10s, ShardMappingCheckInterval=5s, NumShards=8192.
- Note: the YAML config's cluster.shards should align with cluster.Config.NumShards. If you want runtime-global defaults to be applied, the calling code should merge values or use DefaultConfig() from cluster when YAML omits equivalents.

## Postgres config (separate utility)

util/postgres/config.go contains a separate Config struct and defaults (host, port, user, password, dbname, sslmode) and validation utilities. If database connectivity is added to the main runtime, consider referencing or reusing this helper.

## Versioning and compatibility

- The config file has a "version" integer. Current supported value: 1.
- When adding new top-level or nested fields:
  - Keep addition backward compatible by making new fields optional with sensible defaults.
  - For breaking changes, increase the config.version and add logic in LoadConfig/Validate to accept older versions or provide a migration tool.
- Document any new fields in this design doc and tests.

## Security and secrets

- Secrets (database passwords, certificate private keys, tokens) must not be committed into version control.
- Recommended approaches:
  - Keep a separate secrets file outside the repo and load it at runtime; or
  - Use environment variables for secrets and treat YAML only for non-secret configuration; or
  - Integrate with a secrets manager (Vault, AWS Secrets Manager) and inject at runtime.
- If secrets must appear in YAML, enforce file system permissions and consider encrypting the file (or specific values).

## Operational recommendations

- Put config files under /etc/goverse, /opt/goverse/config, or a container volume. Document chosen location in run scripts and the CLI.
- Use process supervisor or systemd to restart program on config changes if a config reload workflow is not implemented.
- Prefer IPs or resolvable hostnames for all addresses; advertise_addr must be reachable from other nodes.
- Ensure the etcd endpoints are highly available and prefixed for multi-tenant use.

## Runtime behavior and merging defaults

- LoadConfig(path) performs strict validation and returns an error which should cause process exit at startup.
- The cluster package defines runtime defaults (NumShards = 8192, MinQuorum = 1, timeouts) that are intended for production. The program should:
  - Read YAML into config.Config.
  - Validate YAML.
  - Build cluster.Config by taking values from YAML where present, or using cluster.DefaultConfig() for any runtime-only parameters not represented in YAML.
- Align cluster.shards in YAML with runtime cluster.NumShards to avoid mismatches.

## Extensibility patterns

- To add new configuration sections:
  - Add fields to the Go structs with yaml struct tags.
  - Update Validate to check constraints.
  - Provide example config(s) in docs/ and tests in config package.
- For optional components (e.g., Postgres persistence), add a top-level optional block:
  - postgres: { host, port, user, ... } and validate only when that component is enabled.

Example extension:
```yaml
postgres:
  host: "db.internal.local"
  port: 5432
  user: "goverse"
  password: "" # recommend environment-supplied
  database: "goverse"
  sslmode: "disable"
```

## Dynamic reloads

- Current implementation is static: config is loaded once at startup via LoadConfig(path).
- If dynamic config reloading is desired:
  - Implement a watcher (fsnotify) to re-read and validate file on change.
  - Ensure reconfiguration is applied atomically or that components handle partial updates and rollbacks.
  - Add a safe mode: write new config to a temp file, validate, then replace and signal process.

## Tests and CI

Add unit tests for:
- Loading valid example configs (single-node, multi-node).
- Detecting invalid configs: missing provider, invalid provider, zero shards, missing etcd endpoints, duplicate node/gate ids, missing node fields.
- Round-trip serialization: marshal/unmarshal to ensure tags are correct.
- If dynamic reload implemented: tests for atomic reload and rollback on failed validation.

CI:
- Add small test fixtures under config/testdata/ (valid.yaml, missing-fields.yaml, duplicate-ids.yaml) used by unit tests.
- Lint YAML examples with a schema validator or a simple test that loads them via LoadConfig.

## Acceptance criteria

- README includes at least one canonical YAML example and tells how to start the binary with a config file path.
- config.LoadConfig enforces the validation rules listed above and returns helpful error messages.
- Unit tests cover both positive and negative cases. CI runs these tests on PRs.
- Secrets are never committed to the repository; CI checks can fail a commit that includes obvious secrets.

## Migration and backward-compatibility notes

- If future changes require breaking renames, increment `version` and:
  - Provide a migration tool to convert v1 -> v2 configs.
  - Keep LoadConfig aware of older versions and provide a helpful error telling operators to run a migration tool if required.

## Implementation checklist (suggested next steps)

- [x] Add docs/config-design.md (this document) to the repository docs.
- [x] Add example config files to config/examples/ and update README with usage.
- [x] Add unit tests in package config that load the example configs and validate negative cases.
- [ ] Add a small notes section in README describing secret handling and recommended file locations.
- [ ] Consider adding env-var overrides for the most common fields (e.g., ETCD endpoints) and document precedence.

## Appendix — Example configs

### 1) Single-node development

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
```

### 2) Production multi-node (skeleton)

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
```

---

See the example config files in [config/examples/](../config/examples/) for complete working configurations.
