package node

import (
	"sync"
)

// KeyLock Manager - Lock Ordering Documentation
//
// The KeyLock manager provides per-object-ID read/write locking to ensure safe concurrent
// access to objects. To prevent deadlocks, the following lock ordering MUST be followed:
//
// LOCK ORDERING RULES (must be acquired in this order):
//   1. node.stopMu.RLock() - Global stop coordination (if needed)
//   2. KeyLock per-key Lock() or RLock() - Per-object exclusive or shared lock
//   3. node.objectsMu - Global objects map lock (if needed)
//
// IMPORTANT: Never acquire locks in a different order!
//
// Lock Usage by Operation:
//
//   CreateObject:
//     - Acquires: stopMu.RLock → per-key Lock → objectsMu (for map insertion)
//     - Purpose: Exclusive lock prevents concurrent creates/deletes on same ID
//     - Lifecycle: Holds per-key lock across initialization, persistence load, and map insertion
//
//   CallObject:
//     - Acquires: stopMu.RLock → per-key RLock (during method execution)
//     - Purpose: Shared lock prevents concurrent delete during method call
//     - Lifecycle: Acquires per-key lock AFTER object existence check to avoid deadlock with auto-create
//
//   DeleteObject:
//     - Acquires: stopMu.RLock → per-key Lock → objectsMu (for map removal)
//     - Purpose: Exclusive lock prevents concurrent creates/calls/saves during deletion
//     - Lifecycle: Removes from map FIRST (while holding per-key lock), then deletes from persistence
//
//   SaveAllObjects:
//     - Acquires: stopMu.RLock → per-key RLock (for each object being saved)
//     - Purpose: Shared lock prevents concurrent delete during save
//     - Lifecycle: Acquires per-key lock separately for each object in the snapshot
//
// Memory Management:
//   - Lock entries are automatically created on first use
//   - Lock entries are automatically cleaned up when refCount reaches 0
//   - No manual cleanup required
//   - Safe for high concurrency across many different object IDs

// keyLockEntry represents a per-key RW lock with reference counting
type keyLockEntry struct {
	mu      sync.RWMutex
	refCount int // Number of active holders (both readers and writers)
}

// KeyLock manages per-key read/write locks with automatic cleanup
// It provides a way to lock individual keys (object IDs) without blocking operations on other keys
type KeyLock struct {
	mu     sync.Mutex
	locks  map[string]*keyLockEntry
}

// NewKeyLock creates a new KeyLock manager
func NewKeyLock() *KeyLock {
	return &KeyLock{
		locks: make(map[string]*keyLockEntry),
	}
}

// Lock acquires an exclusive lock for the given key
// Returns a function that must be called to release the lock
func (kl *KeyLock) Lock(key string) func() {
	kl.mu.Lock()
	entry, exists := kl.locks[key]
	if !exists {
		entry = &keyLockEntry{}
		kl.locks[key] = entry
	}
	entry.refCount++
	kl.mu.Unlock()

	// Acquire the actual lock (outside the manager's lock)
	entry.mu.Lock()

	// Return unlock function
	return func() {
		entry.mu.Unlock()

		// Clean up if no longer needed
		kl.mu.Lock()
		entry.refCount--
		if entry.refCount == 0 {
			delete(kl.locks, key)
		}
		kl.mu.Unlock()
	}
}

// RLock acquires a shared (read) lock for the given key
// Returns a function that must be called to release the lock
func (kl *KeyLock) RLock(key string) func() {
	kl.mu.Lock()
	entry, exists := kl.locks[key]
	if !exists {
		entry = &keyLockEntry{}
		kl.locks[key] = entry
	}
	entry.refCount++
	kl.mu.Unlock()

	// Acquire the actual read lock (outside the manager's lock)
	entry.mu.RLock()

	// Return unlock function
	return func() {
		entry.mu.RUnlock()

		// Clean up if no longer needed
		kl.mu.Lock()
		entry.refCount--
		if entry.refCount == 0 {
			delete(kl.locks, key)
		}
		kl.mu.Unlock()
	}
}
