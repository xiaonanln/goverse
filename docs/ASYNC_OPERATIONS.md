# Async CreateObject and DeleteObject Implementation

## Overview

As of this update, `Cluster.CreateObject()` and `Cluster.DeleteObject()` execute asynchronously to prevent deadlocks when called from within object methods or OnCreated callbacks.

## Problem Statement

Previously, these operations were synchronous and could cause deadlocks in several scenarios:

### 1. Same-Node Deadlock
When an object method holds a read lock and tries to create another object on the same node:
```go
func (obj *MyObject) MyMethod(ctx context.Context, req *Request) (*Response, error) {
    // This method holds a read lock on obj
    // Trying to create another object needs a write lock
    _, err := goverseapi.CreateObject(ctx, "AnotherObject", "another-id")
    // DEADLOCK: Waiting for write lock while holding read lock
    return &Response{}, err
}
```

### 2. Cross-Node Circular Deadlock
When objects on different nodes create/call each other:
```
Node A: Object1.Method() → CreateObject("obj2") on Node B → waits (holding lock)
Node B: obj2.OnCreated() → CallObject("obj1") on Node A → waits
Node A: CallObject("obj1") → tries to acquire lock already held → DEADLOCK
```

## Solution

Both `CreateObject` and `DeleteObject` now execute their operations asynchronously:

1. **Synchronous steps** (fast, no locks):
   - Generate object ID if not provided
   - Determine target node for the object

2. **Asynchronous execution** (in goroutine):
   - Create/delete object on local node OR
   - Route request to remote node via gRPC
   - Log success or error messages
   - Uses the original context for proper cancellation and tracing

3. **Immediate return**:
   - `CreateObject` returns the object ID immediately
   - `DeleteObject` returns nil immediately

## Usage Examples

### Creating Objects from Methods

```go
type MyObject struct {
    goverseapi.BaseObject
}

func (obj *MyObject) CreateChild(ctx context.Context, req *CreateChildRequest) (*Response, error) {
    // This returns immediately - object creation happens asynchronously
    childID, err := goverseapi.CreateObject(ctx, "ChildObject", "")
    if err != nil {
        // Only errors in ID generation or node routing are returned
        return nil, err
    }
    
    // Child object will be created asynchronously
    // Check logs to verify creation completed successfully
    return &Response{ChildId: childID}, nil
}
```

### Deleting Objects from Methods

```go
func (obj *MyObject) DeleteChild(ctx context.Context, req *DeleteChildRequest) (*Response, error) {
    // This returns immediately - object deletion happens asynchronously
    err := goverseapi.DeleteObject(ctx, req.ChildId)
    if err != nil {
        // Only errors in node routing are returned
        return nil, err
    }
    
    // Child object will be deleted asynchronously
    // Check logs to verify deletion completed successfully
    return &Response{Success: true}, nil
}
```

### OnCreated Callbacks

```go
func (obj *MyObject) OnCreated() {
    // Safe to call CreateObject from OnCreated - no deadlock risk
    ctx := context.Background()
    _, _ = goverseapi.CreateObject(ctx, "RelatedObject", "")
    
    // Operation happens asynchronously, OnCreated completes immediately
}
```

## Behavior Changes

### Before (Synchronous)

```go
// Blocks until operation completes
id, err := cluster.CreateObject(ctx, "MyObject", "obj-1")
if err != nil {
    // Error indicates operation failed
    log.Printf("Create failed: %v", err)
}
// Object definitely exists at this point (if err == nil)
```

### After (Asynchronous)

```go
// Returns immediately
id, err := cluster.CreateObject(ctx, "MyObject", "obj-1")
if err != nil {
    // Error only indicates routing/ID generation failure
    log.Printf("Create initiation failed: %v", err)
}
// Object may not exist yet - creation is in progress
// Check logs for "Async CreateObject obj-1 completed successfully"
```

## Context Handling

The async goroutines use the **original context** passed to `CreateObject` or `DeleteObject`. This means:

- Context cancellation is respected (if context is cancelled, async operations will fail)
- Context values (like tracing IDs) are preserved in async operations
- Context deadlines are honored in async gRPC calls

This is important for distributed tracing and proper cancellation propagation.

## Error Handling

Since operations are asynchronous, errors during actual creation/deletion are **logged** but **not returned** to the caller.

**Errors returned to caller** (synchronous errors):
- Empty object ID in `DeleteObject`
- Failed to determine target node (shard mapping issues)

**Errors logged but not returned** (asynchronous errors):
- Failed to get connection to remote node
- Remote CreateObject/DeleteObject gRPC call failed
- Local node object creation/deletion failed
- Context cancelled or deadline exceeded in async operation

**Monitoring async operations:**
```
[INFO] Cluster<localhost:47001,leader,quorum=1> - CreateObject obj-1 initiated asynchronously
[INFO] Cluster<localhost:47001,leader,quorum=1> - Async CreateObject obj-1 completed successfully

// Or on error:
[ERROR] Cluster<localhost:47001,leader,quorum=1> - Async CreateObject obj-1 failed: object type not registered
```

## Testing

Tests in `cluster/cluster_async_operations_test.go` verify:
1. CreateObject can be called from within object methods without deadlock
2. DeleteObject can be called from within object methods without deadlock  
3. Both operations return immediately (< 100ms typically)

Run with:
```bash
go test -v ./cluster/ -run TestAsync
```

Note: These tests require etcd to be running at localhost:2379.

## Migration Guide

### Existing Code Compatibility

Most existing code will continue to work without changes, but be aware of the behavioral differences:

**If your code expects immediate completion:**
```go
// OLD: Object exists immediately after CreateObject
id, _ := cluster.CreateObject(ctx, "MyObject", "obj-1")
_, err := cluster.CallObject(ctx, "MyObject", id, "SomeMethod", req)
// MAY FAIL: Object might not exist yet
```

**Updated approach:**
```go
// Wait a bit for async creation to complete
id, _ := cluster.CreateObject(ctx, "MyObject", "obj-1")
time.Sleep(100 * time.Millisecond) // Or use retry logic
_, err := cluster.CallObject(ctx, "MyObject", id, "SomeMethod", req)
```

**If your code relies on error handling:**
```go
// OLD: err indicates if object was created
_, err := cluster.CreateObject(ctx, "MyObject", "obj-1")
if err != nil {
    return fmt.Errorf("failed to create object: %w", err)
}

// NEW: err only indicates routing/ID issues, not creation failure
// Check logs for actual creation errors
_, err := cluster.CreateObject(ctx, "MyObject", "obj-1") 
if err != nil {
    return fmt.Errorf("failed to initiate object creation: %w", err)
}
// Add monitoring/logging to track creation completion
```

## Benefits

1. **No Deadlocks**: Operations never wait while holding locks
2. **Simple Implementation**: All operations are async - no complex conditional logic
3. **Consistent Behavior**: Same async behavior regardless of how the operation is called
4. **Better Scalability**: Non-blocking operations improve throughput

## Trade-offs

1. **Eventually Consistent**: Objects may not exist immediately after CreateObject returns
2. **Limited Error Feedback**: Callers don't get immediate notification of creation/deletion failures
3. **Debugging Complexity**: Must check logs to verify operation completion
4. **Testing Considerations**: Tests need to account for async completion (add delays or polling)

## Future Enhancements

Possible improvements for async operations:

1. **Completion Callbacks**: Allow callers to provide callbacks for success/failure
2. **Promise/Future Pattern**: Return a future that can be awaited
3. **Status Query API**: Add methods to check operation completion status
4. **Synchronous Option**: Add a parameter to force synchronous execution when needed
