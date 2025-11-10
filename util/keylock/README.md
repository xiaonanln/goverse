# KeyLock - Per-Key Locking Utility

## Overview

The `keylock` package provides a scalable per-key read/write locking mechanism with automatic cleanup. It allows fine-grained coordination of operations on individual keys (such as object IDs) without requiring a global lock.

## Features

- **Per-key exclusive locks** (`Lock`) for operations that modify state
- **Per-key shared locks** (`RLock`) for operations that read state  
- **Automatic cleanup** via reference counting when no goroutines hold locks
- **Thread-safe** under high concurrency
- **No global lock contention** - operations on different keys proceed in parallel

## Usage

```go
import "github.com/xiaonanln/goverse/util/keylock"

// Create a new KeyLock manager
kl := keylock.NewKeyLock()

// Acquire an exclusive lock for a key
unlock := kl.Lock("object-123")
defer unlock()
// ... perform exclusive operations on object-123 ...

// Or acquire a shared (read) lock
unlock := kl.RLock("object-456")
defer unlock()
// ... perform read operations on object-456 ...
```

## Lock Types

### Exclusive Lock (Lock)

Use `Lock(key)` when you need exclusive access to a key. This prevents any other goroutine from acquiring either a read or write lock on the same key until the lock is released.

```go
unlock := kl.Lock("my-key")
defer unlock()
// Exclusive access - no other goroutines can read or write
```

### Shared Lock (RLock)

Use `RLock(key)` when you only need read access. Multiple goroutines can hold shared locks on the same key simultaneously, but exclusive locks will block until all shared locks are released.

```go
unlock := kl.RLock("my-key")
defer unlock()
// Shared read access - other readers can proceed concurrently
```

## Implementation Details

- Each key gets its own `sync.RWMutex` created on demand
- Reference counting ensures locks are garbage collected when no longer in use
- The unlock function returned by `Lock()` and `RLock()` must be called to release the lock
- A global mutex protects the lock registry itself (brief locks only)

## Best Practices

1. **Always defer unlock**: Ensure locks are released even if a panic occurs
   ```go
   unlock := kl.Lock(key)
   defer unlock()
   ```

2. **Keep critical sections short**: Release locks as soon as possible to maximize concurrency

3. **Use RLock for read-only operations**: Allows multiple concurrent readers

4. **Avoid nested locks on the same key**: This will cause a deadlock in the same goroutine

## Thread Safety

The KeyLock implementation is fully thread-safe and tested under high concurrency. All internal operations use appropriate locking to prevent race conditions.

## Testing

The package includes comprehensive tests covering:
- Basic locking behavior
- Concurrent access patterns
- Reference counting
- Lock cleanup
- Race detection
- High concurrency scenarios

Run tests with:
```bash
go test ./util/keylock/
```
