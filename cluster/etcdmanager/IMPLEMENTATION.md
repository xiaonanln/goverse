# Etcd Unreliability Handling Implementation

## Problem Statement

When etcd becomes unreliable (network issues, server restart, etc.), the keep-alive channel for node registration leases would close, causing the node registration to expire. The previous implementation would just log a warning and exit the goroutine, leaving the node unregistered.

## Solution

Implemented an automatic retry mechanism with exponential backoff that seamlessly handles etcd unreliability:

### Key Changes

1. **Keep-Alive Loop** (`keepAliveLoop`)
   - Runs continuously in a background goroutine
   - Calls `maintainLease()` to establish and maintain the lease
   - If `maintainLease()` fails (keep-alive channel closes), automatically retries
   - Uses exponential backoff: starts at 1s, doubles on each retry, max 30s
   - Exits cleanly when context is cancelled or manager is closed

2. **Lease Maintenance** (`maintainLease`)
   - Creates a new lease (15 second TTL)
   - Registers the node with the lease
   - Monitors the keep-alive channel
   - Returns when channel closes (triggering retry) or context is cancelled

3. **Synchronization**
   - Added `keepAliveMu` mutex to protect shared state
   - Added `keepAliveWg` WaitGroup to track goroutine lifecycle
   - Thread-safe access to `leaseID` field
   - Clean shutdown waits for goroutine to exit

4. **Lifecycle Management**
   - `RegisterNode()`: Starts the keep-alive loop
   - `UnregisterNode()`: Stops the loop, waits for exit, revokes lease
   - `Close()`: Stops the loop, waits for exit, closes etcd client

### Architecture

```
RegisterNode()
    └─> keepAliveLoop() [goroutine]
         ├─> maintainLease()
         │    ├─> Grant lease
         │    ├─> Put key with lease
         │    └─> Monitor keep-alive channel
         │         └─> [channel closes]
         ├─> Sleep with exponential backoff
         └─> Retry maintainLease()
```

### Behavior

**Normal Operation:**
1. Node registers successfully
2. Keep-alive messages flow continuously
3. Lease is renewed automatically
4. Node stays registered indefinitely

**Etcd Becomes Unavailable:**
1. Keep-alive channel closes
2. `maintainLease()` returns error
3. Loop waits 1 second, retries
4. Continues retrying with exponential backoff (2s, 4s, 8s, ..., up to 30s)
5. Logs warnings at each retry

**Etcd Becomes Available Again:**
1. Next retry succeeds
2. New lease created
3. Node re-registered
4. Keep-alive resumes
5. Retry delay resets to 1s

**Clean Shutdown:**
1. `UnregisterNode()` or `Close()` called
2. Context cancelled, `keepAliveStopped` set
3. Loop exits on next iteration
4. Caller waits via WaitGroup
5. Lease revoked and key deleted

### Testing

Created comprehensive tests in `keepalive_test.go`:

- **TestKeepAliveRetry**: Verifies node registration persists over time
- **TestKeepAliveContextCancellation**: Tests clean shutdown on unregister
- **TestRegisterNodeIdempotent**: Tests multiple registration calls are safe
- **TestCloseStopsKeepAlive**: Verifies Close() properly stops the loop

### Example

Created `examples/etcd_retry/` with:
- Working example program demonstrating the retry mechanism
- README with instructions for testing
- Shows how to handle etcd unreliability in practice

## Testing the Fix

### Unit Tests
```bash
cd cluster/etcdmanager
go test -v -run "TestKeepAlive"
```

### Manual Testing
1. Start etcd: `etcd`
2. Run example: `cd examples/etcd_retry && go run example_retry.go`
3. Stop etcd (Ctrl+C)
4. Observe retry logs
5. Restart etcd
6. Observe successful re-registration

### Expected Log Output

When etcd stops:
```
[WARN] Keep-alive channel closed for lease 123456, will retry
[WARN] Failed to maintain lease for node localhost:50000: keep-alive channel closed, retrying in 1s
[WARN] Failed to maintain lease for node localhost:50000: failed to grant lease: context deadline exceeded, retrying in 2s
[WARN] Failed to maintain lease for node localhost:50000: failed to grant lease: context deadline exceeded, retrying in 4s
```

When etcd restarts:
```
[INFO] Registered node localhost:50000 with lease ID 123457
[DEBUG] Keep-alive response for lease 123457, TTL: 15
```

## Benefits

1. **Seamless Recovery**: Nodes automatically re-register when etcd becomes available
2. **No Manual Intervention**: The system self-heals without operator action
3. **Resource Efficient**: Exponential backoff prevents overwhelming etcd during outages
4. **Clean Shutdown**: Proper synchronization ensures no resource leaks
5. **Production Ready**: Comprehensive error handling and logging

## Compatibility

- Backward compatible - existing code works unchanged
- No breaking API changes
- All existing tests continue to pass
- Thread-safe implementation

## Future Enhancements

Potential improvements for the future:
- Make retry delays configurable
- Add metrics for retry attempts
- Add health checks to detect prolonged failures
- Support multiple retry strategies (exponential, linear, constant)
