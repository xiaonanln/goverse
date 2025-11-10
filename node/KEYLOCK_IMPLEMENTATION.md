# KeyLock Implementation - Per-Object Read/Write Locking

## Overview

This document describes the implementation of per-object-ID read/write locking in the Goverse Node using the KeyLock manager. This implementation ensures that concurrent operations on the same object are properly serialized to prevent race conditions.

## Problem Statement

Before this implementation, the Node had the following concurrency vulnerabilities:

1. **Create/Delete Race**: Multiple goroutines could attempt to create/delete the same object ID simultaneously
2. **Call/Delete Race**: An object could be deleted while a method call was in progress
3. **Save/Delete Race**: An object could be deleted while being saved to persistence
4. **Create/Create Race**: Multiple goroutines could create duplicate objects with the same ID

These race conditions could lead to:
- Inconsistent state between memory and persistence
- Panics due to accessing deleted objects
- Memory leaks from duplicate object creation
- Data corruption

## Solution: KeyLock Manager

### Architecture

The KeyLock manager provides per-key (object ID) read/write locks with automatic lifecycle management:

```go
type KeyLock struct {
    mu     sync.Mutex
    locks  map[string]*keyLockEntry
}

type keyLockEntry struct {
    mu       sync.RWMutex
    refCount int
}
```

### Key Features

1. **Per-Key Locking**: Each object ID gets its own RWMutex, allowing concurrent operations on different objects
2. **Automatic Lifecycle**: Lock entries are created on demand and cleaned up when no longer needed
3. **Reference Counting**: Tracks active lock holders to determine when to clean up entries
4. **Memory Efficient**: No unbounded memory growth - unused entries are removed

### Lock Types

- **Exclusive Lock (Lock)**: Used for create/delete operations
  - Prevents concurrent creates, deletes, calls, and saves on the same ID
  - Serializes all operations on that object

- **Shared Lock (RLock)**: Used for call/save operations  
  - Allows multiple concurrent readers (callers/savers)
  - Prevents concurrent writes (create/delete)

## Lock Ordering Rules

To prevent deadlocks, locks MUST be acquired in this order:

1. `node.stopMu.RLock()` - Global stop coordination
2. `KeyLock.Lock()` or `KeyLock.RLock()` - Per-object lock
3. `node.objectsMu` - Global objects map lock

**Never acquire locks in a different order!**

## Integration with Node Operations

### createObject

```go
func (node *Node) createObject(ctx context.Context, typ string, id string) (Object, error) {
    // Lock ordering: per-key Lock → objectsMu
    unlockKey := node.keyLock.Lock(id)
    defer unlockKey()
    
    // Check if object exists
    // Initialize object
    // Load from persistence
    // Insert into map
}
```

**Guarantees**:
- Only one create can run for a given ID at a time
- No concurrent delete can occur during create
- No concurrent call/save can occur during initialization

### CallObject

```go
func (node *Node) CallObject(...) (proto.Message, error) {
    // Lock ordering: stopMu.RLock → per-key RLock
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    // Check if object exists (without per-key lock)
    // If not exists, call createObject (which acquires its own exclusive lock)
    
    // Acquire per-key shared lock for method execution
    unlockKey := node.keyLock.RLock(id)
    defer unlockKey()
    
    // Execute method
}
```

**Guarantees**:
- Object cannot be deleted during method execution
- Multiple calls to the same object can run concurrently
- Auto-create case handled without deadlock

**Special Handling for Auto-Create**:
- First checks existence without lock
- If object doesn't exist, calls createObject (which acquires exclusive lock)
- Then acquires shared lock for method execution
- This prevents deadlock between RLock and Lock

### DeleteObject

```go
func (node *Node) DeleteObject(ctx context.Context, id string) error {
    // Lock ordering: stopMu.RLock → per-key Lock → objectsMu
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    unlockKey := node.keyLock.Lock(id)
    defer unlockKey()
    
    // Check if object exists
    // Remove from map FIRST (while holding lock)
    // Delete from persistence
}
```

**Guarantees**:
- No concurrent creates, calls, or saves can occur during deletion
- Object is removed from map before persistence deletion
- New operations cannot find the object after map removal

**Key Design Decision**:
- Object is removed from memory **before** persistence deletion
- This ensures no new operations can access the object
- Even if persistence deletion fails, object is gone from memory

### SaveAllObjects

```go
func (node *Node) saveAllObjectsInternal(ctx context.Context) error {
    // Get snapshot of objects
    // For each object:
    unlockKey := node.keyLock.RLock(objID)
    
    // Verify object still exists
    // Get object data
    // Save to persistence
    
    unlockKey()
}
```

**Guarantees**:
- Object cannot be deleted during save
- Multiple saves can run concurrently
- Handles objects deleted after snapshot

## Memory Management

### Reference Counting

Each lock entry maintains a `refCount`:
- Incremented when a Lock/RLock is acquired
- Decremented when unlocked
- Entry is deleted when refCount reaches 0

### Cleanup Process

```go
func (kl *KeyLock) Lock(key string) func() {
    // Acquire lock
    kl.mu.Lock()
    entry.refCount++
    kl.mu.Unlock()
    
    // Return unlock function
    return func() {
        entry.mu.Unlock()
        
        kl.mu.Lock()
        entry.refCount--
        if entry.refCount == 0 {
            delete(kl.locks, key)  // Cleanup
        }
        kl.mu.Unlock()
    }
}
```

### Memory Leak Prevention

- Tested with 1000+ create/delete cycles
- Verified map is empty after operations complete
- No unbounded growth even under high concurrency

## Testing

### Unit Tests (keylock_test.go)

- Basic lock/unlock functionality
- Multiple readers scenario
- Read/write mutual exclusion
- Reference counting correctness
- Memory leak prevention
- Concurrent access to different keys

### Integration Tests (node_keylock_integration_test.go)

- Create/delete race conditions
- Call/delete race conditions  
- Save/delete race conditions
- Concurrent creates of same ID
- Memory leak under repeated operations
- Concurrent calls to same object

### All Tests Pass

- ✅ All existing tests pass
- ✅ Race detector enabled (`go test -race`)
- ✅ No race conditions detected
- ✅ No deadlocks observed
- ✅ No memory leaks detected

## Performance Characteristics

### Lock Acquisition

- **Different Keys**: O(1) - No contention, fully concurrent
- **Same Key**: Serialized by per-key lock
- **Lock Creation**: O(1) amortized (map insertion)
- **Lock Cleanup**: O(1) (map deletion)

### Memory Overhead

- Per active key: ~48 bytes (RWMutex + refcount + map entry)
- Only active keys consume memory
- Automatic cleanup prevents accumulation

### Scalability

- Highly scalable for operations on different object IDs
- No global bottleneck for different keys
- Only serializes operations on the same object ID (which is desired)

## Backward Compatibility

### No API Changes

All public method signatures remain unchanged:
- `CreateObject(ctx, typ, id) (string, error)`
- `CallObject(ctx, typ, id, method, request) (proto.Message, error)`
- `DeleteObject(ctx, id) error`
- `SaveAllObjects(ctx) error`

### Behavioral Changes

- **More Correct**: Eliminates race conditions
- **Idempotent**: Concurrent creates of same ID return same object
- **Safer**: Prevents access to deleted objects
- **Consistent**: Ensures memory and persistence stay in sync

### Migration

No migration needed - changes are internal to Node implementation.

## Future Enhancements

Potential improvements (not currently needed):

1. **Configurable Cleanup**: Allow tuning of cleanup timing
2. **Lock Statistics**: Track lock contention metrics
3. **Timeout Support**: Add timeout for lock acquisition
4. **Priority Locks**: Allow priority-based lock acquisition
5. **Lock Monitoring**: Expose lock state for debugging

## Summary

The KeyLock implementation provides:

✅ **Correctness**: Eliminates race conditions  
✅ **Safety**: Prevents concurrent create/delete issues  
✅ **Performance**: Minimal overhead, highly scalable  
✅ **Memory Efficiency**: Automatic cleanup, no leaks  
✅ **Maintainability**: Clear lock ordering, well-documented  
✅ **Compatibility**: No API changes, fully backward compatible  

This implementation ensures the Node can safely handle high-concurrency workloads with many objects and frequent create/delete/call/save operations.
