package node

import (
	"sync"
)

// keyLockEntry represents a per-key lock with reference counting
type keyLockEntry struct {
	mu      sync.RWMutex
	refCount int
}

// KeyLock manages per-key read/write locks with automatic cleanup
// It provides thread-safe RW locking per object ID to prevent concurrent
// creation/deletion while allowing concurrent reads (calls/saves).
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
// Returns an unlock function that MUST be called to release the lock
func (kl *KeyLock) Lock(key string) func() {
	kl.mu.Lock()
	entry, exists := kl.locks[key]
	if !exists {
		entry = &keyLockEntry{refCount: 0}
		kl.locks[key] = entry
	}
	entry.refCount++
	kl.mu.Unlock()

	// Acquire exclusive lock on the entry
	entry.mu.Lock()

	// Return unlock function
	return func() {
		entry.mu.Unlock()
		kl.release(key)
	}
}

// RLock acquires a shared (read) lock for the given key
// Returns an unlock function that MUST be called to release the lock
func (kl *KeyLock) RLock(key string) func() {
	kl.mu.Lock()
	entry, exists := kl.locks[key]
	if !exists {
		entry = &keyLockEntry{refCount: 0}
		kl.locks[key] = entry
	}
	entry.refCount++
	kl.mu.Unlock()

	// Acquire shared lock on the entry
	entry.mu.RLock()

	// Return unlock function
	return func() {
		entry.mu.RUnlock()
		kl.release(key)
	}
}

// release decrements the reference count and removes the entry if no longer needed
func (kl *KeyLock) release(key string) {
	kl.mu.Lock()
	defer kl.mu.Unlock()

	entry, exists := kl.locks[key]
	if !exists {
		return
	}

	entry.refCount--
	if entry.refCount == 0 {
		delete(kl.locks, key)
	}
}

// Len returns the number of currently tracked keys (for testing)
func (kl *KeyLock) Len() int {
	kl.mu.Lock()
	defer kl.mu.Unlock()
	return len(kl.locks)
}
