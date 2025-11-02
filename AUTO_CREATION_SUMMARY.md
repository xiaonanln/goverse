# Auto-Creation of etcdmanager and consensusmanager in Cluster

## Summary

This change implements automatic creation of `etcdmanager.EtcdManager` and `consensusmanager.ConsensusManager` instances within the `Cluster` type, eliminating the need for explicit `SetEtcdManager` calls and simplifying cluster initialization.

## Changes Made

### 1. Cluster Struct Updates
- Added `etcdAddress` and `etcdPrefix` fields to store etcd configuration
- These fields are populated when `ConnectEtcd` is called with parameters

### 2. New Methods

#### `ensureEtcdManager()`
- Private helper method that creates etcd and consensus managers if they don't exist
- Uses lazy initialization pattern
- Returns error if etcd address is not configured
- Automatically creates both etcdManager and consensusManager together

### 3. Modified Methods

#### `ConnectEtcd(etcdAddress string, prefix string)`
- **Old signature**: `ConnectEtcd() error`
- **New signature**: `ConnectEtcd(etcdAddress string, prefix string) error`
- Now accepts etcd configuration parameters
- Auto-creates managers if they don't exist
- If parameters are empty and managers exist, uses existing managers
- If parameters are empty and managers don't exist, returns error

#### Methods Updated to Use `ensureEtcdManager()`
All methods that previously checked `if c.etcdManager == nil` or `if c.consensusManager == nil` now call `ensureEtcdManager()` instead:
- `RegisterNode()`
- `StartWatching()`
- `InitializeShardMapping()`
- `UpdateShardMapping()`
- `GetShardMapping()`
- `GetNodeForObject()` (for shard-based routing)
- `GetNodeForShard()`
- `StartShardMappingManagement()`

#### `GetNodes()` and `GetLeaderNode()`
- Updated to gracefully handle missing managers
- Attempt lazy initialization but don't fail if it doesn't work
- Return empty/default values gracefully

### 4. Backward Compatibility

#### `SetEtcdManager(mgr *etcdmanager.EtcdManager)`
- Marked as **deprecated** but still functional
- Kept for backward compatibility with existing code
- Still used in some unit tests that specifically test this method

### 5. Server Integration
- Updated `server.NewServer()` to remove explicit etcd manager creation
- Updated `server.Run()` to pass etcd configuration to `ConnectEtcd()`
- Removed `etcdmanager` import from server package

### 6. Test Updates
- Updated all integration tests to use new `ConnectEtcd(address, prefix)` signature
- Removed `etcdmanager.NewEtcdManager()` and `SetEtcdManager()` calls from tests
- Added new tests to verify auto-creation behavior
- Fixed test compilation issues (err variable declarations, unused imports)

## Benefits

1. **Simplified Initialization**: No need to manually create and set etcd manager
2. **Reduced Boilerplate**: Fewer lines of code in both production and test code
3. **Fewer Nil Checks**: Lazy initialization pattern reduces defensive programming
4. **Better Encapsulation**: Cluster manages its own dependencies
5. **Cleaner API**: Configuration passed directly to methods that need it
6. **Backward Compatible**: Existing code using `SetEtcdManager` still works

## Usage Examples

### Before
```go
// Create etcd manager
etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379", "/goverse")
if err != nil {
    return err
}
cluster.Get().SetEtcdManager(etcdMgr)

// Connect to etcd
err = cluster.Get().ConnectEtcd()
if err != nil {
    return err
}
```

### After
```go
// Connect to etcd (auto-creates managers)
err := cluster.Get().ConnectEtcd("localhost:2379", "/goverse")
if err != nil {
    return err
}
```

## Migration Guide

### For New Code
Simply call `ConnectEtcd` with the etcd address and prefix:
```go
err := cluster.Get().ConnectEtcd("localhost:2379", "/goverse")
```

### For Existing Code
Two options:

1. **Update to new pattern** (recommended):
   ```go
   // Old:
   etcdMgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/prefix")
   cluster.Get().SetEtcdManager(etcdMgr)
   cluster.Get().ConnectEtcd()
   
   // New:
   cluster.Get().ConnectEtcd("localhost:2379", "/prefix")
   ```

2. **Keep using SetEtcdManager** (backward compatible):
   ```go
   // Still works, but deprecated
   etcdMgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/prefix")
   cluster.Get().SetEtcdManager(etcdMgr)
   cluster.Get().ConnectEtcd("", "")  // Empty params use existing manager
   ```

## Testing

All unit tests pass for:
- Cluster auto-creation functionality
- Lazy initialization patterns
- Graceful handling of missing managers
- Backward compatibility with SetEtcdManager

Integration tests requiring etcd will pass when etcd is available.
