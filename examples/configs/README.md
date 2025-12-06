# GoVerse Configuration Examples

This directory contains example configuration files for GoVerse clusters.

## Available Examples

### cluster.yml
Basic cluster configuration with multiple nodes and gates.

### cluster-with-autoload.yml
Demonstrates the auto-load objects feature, including:
- Single global objects
- Per-shard objects

## Auto-Load Objects

Auto-load objects are automatically created when nodes start and claim their shards. This is useful for:
- Global singleton services (e.g., matchmaking, leaderboards)
- Per-shard managers or coordinators
- System objects that should always exist

### Single Object Mode

```yaml
auto_load_objects:
  - type: "MatchmakingService"
    id: "GlobalMatchmaker"
    per_shard: false  # Optional, defaults to false
```

Creates one object with ID `GlobalMatchmaker` on whichever node owns that shard.

### Per-Shard Mode

```yaml
auto_load_objects:
  - type: "ShardManager"
    id: "ShardManager"
    per_shard: true
```

Creates one object per shard using fixed-shard IDs:
- `shard#0/ShardManager`
- `shard#1/ShardManager`
- ... up to `shard#8191/ShardManager`

Each node creates objects only for the shards it owns. This ensures:
- Even distribution across nodes
- Co-location with other objects on the same shard
- Automatic rebalancing when nodes join or leave

### Use Cases for Per-Shard Objects

- **Shard-local coordinators**: Manage resources or state within a shard
- **Per-shard aggregators**: Collect metrics or statistics for a shard
- **Shard-specific services**: Provide functionality that needs to be co-located with other objects

## Configuration Requirements

All auto-load objects must specify:
- `type`: The registered object type name
- `id`: Object ID (for single objects) or base name (for per-shard objects)
- `per_shard`: Optional boolean (defaults to false)

The cluster will:
1. Wait for cluster state to stabilize
2. Check which shards each node owns
3. Create the configured objects
4. Log the results

Objects are created asynchronously and errors are logged but don't prevent cluster startup.
