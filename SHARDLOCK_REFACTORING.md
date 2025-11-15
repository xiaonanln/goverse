# ShardLock Refactoring: Per-Cluster Isolation

## Problem

Multiple clusters in tests were sharing the same global shardlock instance, causing lock collisions when tests ran in parallel. This led to race conditions and test failures in distributed scenarios.

## Solution

Refactored the shardlock package to use per-cluster instances instead of a global variable, ensuring complete isolation between clusters.

## Changes

### 1. ShardLock Package (`cluster/shardlock/shardlock.go`)

**Before:**
```go
var globalKeyLock = keylock.NewKeyLock()

func AcquireRead(objectID string) func() {
    shardID := sharding.GetShardID(objectID)
    return globalKeyLock.RLock(shardLockKey(shardID))
}

func AcquireWrite(shardID int) func() {
    return globalKeyLock.Lock(shardLockKey(shardID))
}
```

**After:**
```go
type ShardLock struct {
    keyLock *keylock.KeyLock
}

func NewShardLock() *ShardLock {
    return &ShardLock{
        keyLock: keylock.NewKeyLock(),
    }
}

func (sl *ShardLock) AcquireRead(objectID string) func() {
    shardID := sharding.GetShardID(objectID)
    return sl.keyLock.RLock(shardLockKey(shardID))
}

func (sl *ShardLock) AcquireWrite(shardID int) func() {
    return sl.keyLock.Lock(shardLockKey(shardID))
}
```

### 2. Cluster Struct (`cluster/cluster.go`)

Added `shardLock` field to the Cluster struct:

```go
type Cluster struct {
    // ... other fields ...
    shardLock *shardlock.ShardLock
    // ... other fields ...
}

func NewCluster(cfg Config, thisNode *node.Node) (*Cluster, error) {
    c := &Cluster{
        // ... other fields ...
        shardLock: shardlock.NewShardLock(),
        // ... other fields ...
    }
    
    // Set the cluster's ShardLock on the node immediately
    if thisNode != nil {
        thisNode.SetShardLock(c.shardLock)
    }
    
    // Pass shardLock to consensus manager
    c.consensusManager = consensusmanager.NewConsensusManager(mgr, c.shardLock)
    
    return c, nil
}

func (c *Cluster) GetShardLock() *shardlock.ShardLock {
    return c.shardLock
}
```

### 3. ConsensusManager (`cluster/consensusmanager/consensusmanager.go`)

Updated to accept and use ShardLock instance:

```go
type ConsensusManager struct {
    // ... other fields ...
    shardLock *shardlock.ShardLock
    // ... other fields ...
}

func NewConsensusManager(etcdMgr *etcdmanager.EtcdManager, shardLock *shardlock.ShardLock) *ConsensusManager {
    return &ConsensusManager{
        // ... other fields ...
        shardLock: shardLock,
        // ... other fields ...
    }
}

// Usage in methods
func (cm *ConsensusManager) ClaimShardsForNode(...) error {
    for shardID := range shardsToUpdate {
        unlock := cm.shardLock.AcquireWrite(shardID)
        unlockFuncs = append(unlockFuncs, unlock)
    }
    // ... rest of method ...
}
```

### 4. Node (`node/node.go`)

Added thread-safe ShardLock management:

```go
type Node struct {
    // ... other fields ...
    shardLock   *shardlock.ShardLock
    shardLockMu sync.RWMutex
    // ... other fields ...
}

func (node *Node) SetShardLock(sl *shardlock.ShardLock) {
    node.shardLockMu.Lock()
    defer node.shardLockMu.Unlock()
    node.shardLock = sl
}

func (node *Node) getShardLock() *shardlock.ShardLock {
    node.shardLockMu.RLock()
    defer node.shardLockMu.RUnlock()
    return node.shardLock
}

func (node *Node) createObject(...) error {
    sl := node.getShardLock()
    if sl != nil {
        unlockShard := sl.AcquireRead(id)
        defer unlockShard()
    }
    // ... rest of method ...
}
```

### 5. Tests

All tests updated to create ShardLock instances:

```go
// Before
func TestSomething(t *testing.T) {
    cm := NewConsensusManager(mgr)
    // ...
}

// After
func TestSomething(t *testing.T) {
    cm := NewConsensusManager(mgr, shardlock.NewShardLock())
    // ...
}
```

## Key Benefits

1. **Test Isolation**: Each cluster has its own ShardLock, preventing interference between parallel tests
2. **Better Encapsulation**: ShardLock ownership is clear and explicit
3. **Thread Safety**: Per-cluster locks ensure operations within a cluster are properly synchronized
4. **Maintainability**: Easier to reason about lock scope and lifetime

## Testing

Added comprehensive tests to verify isolation:

- `TestShardLock_MultipleInstances`: Verifies different ShardLock instances don't interfere
- `TestCluster_ShardLockIsolation`: Verifies clusters have isolated shard locks
- `TestShardLock_MultipleClustersConcurrent`: Verifies parallel cluster creation works correctly

All existing tests updated and passing:
- ✅ `cluster/shardlock` package tests
- ✅ `cluster/consensusmanager` unit tests
- ✅ `cluster` isolation tests
- ✅ `node` package tests

## Migration Notes

For code using the old global functions:

```go
// Old code
unlock := shardlock.AcquireRead(objectID)
defer unlock()

// New code (within cluster/node)
sl := cluster.GetShardLock()  // or node.getShardLock()
unlock := sl.AcquireRead(objectID)
defer unlock()
```

The node and cluster packages handle this internally, so application code using these APIs doesn't need changes.

## Performance Impact

No performance impact - the refactoring maintains the same locking semantics, just with per-cluster instead of global scope.

## Backward Compatibility

This is an internal refactoring that doesn't affect public APIs. The change is fully backward compatible for users of the framework.
