# Concurrent CreateObject Race Condition Fix

## Problem

A critical race condition existed in `node.go`'s `createObject` method where concurrent calls to create the same object ID could result in one object being created and initialized, then immediately overwritten by another object created by a different goroutine.

### Race Scenario

```
Time  Thread A                          Thread B
----  --------------------------------  --------------------------------
T1    Check if object exists (RLock)
T2    Not found → release lock
T3                                      Check if object exists (RLock)
T4                                      Not found → release lock
T5    Create new object instance
T6    Call OnInit()
T7                                      Create new object instance
T8                                      Call OnInit()
T9    Load from persistence (if any)
T10                                     Load from persistence (if any)
T11   Call FromData()
T12                                     Call FromData()
T13   Acquire WLock
T14   objects[id] = objA                ← Insert first object
T15   Release WLock
T16   Call OnCreated()                  ← First object fully initialized
T17                                     Acquire WLock
T18                                     objects[id] = objB  ← OVERWRITES!
T19                                     Release WLock
T20                                     Call OnCreated()  ← Both called!
```

### Issues

1. **Object Lost**: Thread A's object is overwritten and lost
2. **Duplicate OnCreated()**: Both objects call `OnCreated()` but only one remains
3. **State Inconsistency**: If objects registered with external services in `OnCreated()`, now have dangling registrations
4. **Resource Leaks**: First object's resources may not be cleaned up properly

## Solution

Implemented the **double-checked locking pattern** to ensure atomicity of the check-and-insert operation:

```go
func (node *Node) createObject(ctx context.Context, typ string, id string) (Object, error) {
    // First check (fast path with read lock)
    node.objectsMu.RLock()
    existingObj := node.objects[id]
    node.objectsMu.RUnlock()
    if existingObj != nil {
        if existingObj.Type() == typ {
            node.logger.Infof("Object %s of type %s already exists, returning existing object", id, typ)
            return existingObj, nil
        }
        return nil, fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
    }

    // ... expensive initialization work ...
    // Create object, OnInit, load persistence, FromData, etc.

    // Second check (under write lock) - prevents race
    node.objectsMu.Lock()
    if existingObj := node.objects[id]; existingObj != nil {
        node.objectsMu.Unlock()
        // Another goroutine created the object while we were initializing
        if existingObj.Type() == typ {
            node.logger.Infof("Object %s already created by another goroutine, returning existing object", id)
            return existingObj, nil
        }
        return nil, fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
    }
    node.objects[id] = obj  // Safe to insert
    node.objectsMu.Unlock()

    obj.OnCreated()
    return obj, nil
}
```

## How It Works

1. **First Check (Read Lock)**: Quick check if object exists - handles the common case where object already exists without blocking other readers

2. **Expensive Initialization**: If object doesn't exist, perform all expensive operations (creation, initialization, persistence loading) WITHOUT holding any locks

3. **Second Check (Write Lock)**: Before inserting into the map, acquire write lock and check again:
   - If another thread won the race and created the object: return their object, discard ours
   - If still doesn't exist: safe to insert

## Benefits

- ✅ **Atomicity**: Check-and-insert is now atomic under the write lock
- ✅ **No Duplicates**: Only one object per ID is ever inserted into the map
- ✅ **Idempotent**: Concurrent CreateObject calls return the same object instance
- ✅ **Type Safety**: Type mismatch still properly detected and reported
- ✅ **Performance**: Fast path (existing object) uses only read lock
- ✅ **Clean State**: Only winning thread calls `OnCreated()`, no duplicate side effects

## Testing

All existing tests continue to pass, including:
- `TestCreateObject_DuplicateID` - Verifies idempotent behavior
- `TestCreateObject_DuplicateID_DifferentType` - Verifies type checking
- `TestCallObject_AutoCreate_MultipleCallsIdempotent` - Verifies auto-creation idempotency

### New Concurrent Tests

Two new tests specifically verify the race condition fix:

1. **`TestCreateObject_ConcurrentCalls`** - Launches 50 concurrent goroutines all attempting to create the same object ID
   - Verifies all calls succeed without errors
   - Verifies all calls return the same object instance (pointer equality)
   - Verifies only one object exists in the registry
   - Critical validation that the double-checked locking prevents race conditions

2. **`TestCreateObject_ConcurrentDifferentObjects`** - Creates 20 different objects concurrently
   - Verifies concurrent creation of different objects works correctly
   - Ensures no interference between different object creations

Run the concurrent tests:
```bash
go test ./node/ -v -run TestCreateObject_Concurrent
```

Run with race detector (requires Docker on Windows):
```bash
.\script\win\test-go.cmd -race
```

The fix maintains backward compatibility while eliminating the race condition.

## Related Files

- `node/node.go` - Fixed `createObject` method (lines 415-428)
- `node/node_create_test.go` - Tests for CreateObject functionality
- `node/node_call_auto_create_test.go` - Tests for auto-creation via CallObject
