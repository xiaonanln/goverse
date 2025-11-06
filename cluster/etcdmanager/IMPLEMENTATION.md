# Etcd Manager Implementation

## Overview

The EtcdManager provides a generic shared lease API for registering arbitrary key-value pairs with etcd. Node registration is now handled at the cluster layer using this shared lease API.

## Shared Lease API

### Design Principles

The shared lease API provides a generic mechanism for registering multiple key-value pairs under a single shared lease per EtcdManager instance:

- **Single Shared Lease**: All keys registered via `RegisterKeyLease()` use the same lease
- **No Per-Key Ownership**: Multiple calls with the same key simply overwrite the value
- **Automatic Lifecycle Management**: The shared lease is created on first use and revoked when no keys remain
- **Resilient Recovery**: If the keepalive fails or connection is lost, the manager automatically recreates the lease and re-registers all keys with exponential backoff

### Methods

#### RegisterKeyLease(ctx, key, value, ttl) (LeaseID, error)

Registers a key-value pair with the shared lease:
- If the shared lease is not running, it starts a background goroutine to maintain it
- The key is added to the in-memory key map and put to etcd with the current lease
- Multiple calls with the same key overwrite the value
- Returns the shared lease ID

#### UnregisterKeyLease(ctx, key) error

Removes a key from etcd and the shared lease:
- Deletes the key from etcd and from the in-memory map
- If this is the last key, stops the keepalive goroutine and revokes the shared lease
- Clean shutdown with proper synchronization

### Architecture

```
RegisterKeyLease()
    └─> sharedLeaseLoop() [goroutine, started on first call]
         ├─> maintainSharedLease()
         │    ├─> Grant lease
         │    ├─> Put all keys with lease
         │    └─> Monitor keep-alive channel
         │         └─> [channel closes]
         ├─> Sleep with exponential backoff (1s, 2s, 4s, 8s max)
         └─> Retry maintainSharedLease() with all keys
```

### Behavior

**Normal Operation:**
1. First `RegisterKeyLease()` call starts the shared lease loop
2. Lease is created and all keys are registered
3. Keep-alive messages flow continuously
4. Lease is renewed automatically
5. Keys stay registered indefinitely

**Adding More Keys:**
1. `RegisterKeyLease()` adds key to in-memory map
2. Key is immediately put to etcd with current lease
3. On next lease recreation, all keys (including new ones) are re-registered

**Etcd Becomes Unavailable:**
1. Keep-alive channel closes
2. `maintainSharedLease()` returns error
3. Loop waits 1 second, retries
4. Continues retrying with exponential backoff (2s, 4s, 8s max)
5. Logs warnings at each retry

**Etcd Becomes Available Again:**
1. Next retry succeeds
2. New lease created
3. All keys from in-memory map re-registered
4. Keep-alive resumes
5. Retry delay resets to 1s

**Removing Keys:**
1. `UnregisterKeyLease()` removes key from etcd and map
2. If last key removed, shared lease loop stops
3. Lease is revoked cleanly
4. Manager ready to start new lease on next `RegisterKeyLease()`

## Node Registration at Cluster Layer

Node registration is now implemented at the cluster layer (`cluster/cluster.go`) using the shared lease API:

### cluster.registerNode()

```go
func (c *Cluster) registerNode(ctx context.Context) error {
    key := c.etcdManager.GetNodesPrefix() + c.thisNode.GetAdvertiseAddress()
    value := c.thisNode.GetAdvertiseAddress()
    _, err := c.etcdManager.RegisterKeyLease(ctx, key, value, etcdmanager.NodeLeaseTTL)
    return err
}
```

### cluster.unregisterNode()

```go
func (c *Cluster) unregisterNode(ctx context.Context) error {
    key := c.etcdManager.GetNodesPrefix() + c.thisNode.GetAdvertiseAddress()
    return c.etcdManager.UnregisterKeyLease(ctx, key)
}
```

This design:
- Removes node-specific logic from EtcdManager
- Makes EtcdManager a generic key-value lease manager
- Allows cluster layer to control node registration semantics
- Enables multiple nodes to be registered from different cluster instances using the same etcd prefix

## Synchronization

### Thread Safety

- `sharedKeysMu`: Protects the shared keys map and lease state
- `sharedLeaseWg`: Tracks the shared lease goroutine lifecycle

### Clean Shutdown

1. `Close()` or final `UnregisterKeyLease()` cancels the shared lease context
2. `sharedLeaseLoop()` exits on context cancellation
3. Caller waits via `sharedLeaseWg.Wait()`
4. Lease is revoked in `maintainSharedLease()` defer
5. No resource leaks

## Testing

Comprehensive tests in `etcdmanager_test.go` and `keepalive_test.go`:

- **TestRegisterKeyLeaseAndUnregisterKey**: Tests basic shared lease functionality with multiple keys
- **TestSharedLeaseResilience**: Tests that keys remain registered over time with keepalive
- **TestKeepAliveRetry**: Tests keep-alive retry mechanism
- **TestKeepAliveContextCancellation**: Tests clean shutdown
- **TestRegisterKeyLeaseIdempotent**: Tests key overwrite behavior
- **TestCloseStopsSharedLease**: Tests that Close() stops the shared lease loop

### Running Tests

```bash
cd cluster/etcdmanager
go test -v
```

## Benefits

1. **Generic API**: Can register any key-value pairs, not just nodes
2. **Efficient**: Single lease for all keys reduces etcd overhead
3. **Resilient**: Automatic recovery from etcd failures
4. **Clean Separation**: Generic shared lease API decoupled from node-specific logic
5. **Flexible**: Cluster layer controls node registration semantics
6. **Production Ready**: Comprehensive error handling, logging, and synchronization

## Migration Guide

### Before (Old API - Removed)

```go
// Old node-specific API - NO LONGER AVAILABLE
mgr.RegisterNode(ctx, nodeAddress)
mgr.UnregisterNode(ctx, nodeAddress)
```

### After (New Shared Lease API)

```go
// New generic shared lease API
key := mgr.GetNodesPrefix() + nodeAddress
value := nodeAddress
mgr.RegisterKeyLease(ctx, key, value, etcdmanager.NodeLeaseTTL)
mgr.UnregisterKeyLease(ctx, key)
```

### For Cluster Usage

The cluster layer (`cluster/cluster.go`) now handles node registration using the shared lease API internally. No changes needed for code using the Cluster API.

## Future Enhancements

Potential improvements:
- Make retry delays configurable
- Add metrics for lease operations
- Support different TTLs per key (currently uses first registered TTL)
- Add health checks to detect prolonged failures
- Support multiple retry strategies
