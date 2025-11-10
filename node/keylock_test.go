package node

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestKeyLock_BasicLock tests basic exclusive locking functionality
func TestKeyLock_BasicLock(t *testing.T) {
	kl := NewKeyLock()

	unlock := kl.Lock("key1")
	// Lock is held, we can do work here
	unlock()

	// After unlock, the entry should be cleaned up
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty after unlock, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_BasicRLock tests basic shared locking functionality
func TestKeyLock_BasicRLock(t *testing.T) {
	kl := NewKeyLock()

	unlock := kl.RLock("key1")
	// Read lock is held
	unlock()

	// After unlock, the entry should be cleaned up
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty after unlock, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_MultipleLocks tests locking different keys doesn't block
func TestKeyLock_MultipleLocks(t *testing.T) {
	kl := NewKeyLock()

	unlock1 := kl.Lock("key1")
	unlock2 := kl.Lock("key2")

	// Both locks should be held
	kl.mu.Lock()
	if len(kl.locks) != 2 {
		t.Errorf("Expected 2 lock entries, got %d", len(kl.locks))
	}
	kl.mu.Unlock()

	unlock1()
	unlock2()

	// After unlocking both, map should be empty
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_ExclusiveLockBlocks tests that exclusive locks block each other
func TestKeyLock_ExclusiveLockBlocks(t *testing.T) {
	kl := NewKeyLock()

	unlock1 := kl.Lock("key1")

	// Try to acquire the same lock from another goroutine
	var locked2 atomic.Bool
	go func() {
		unlock2 := kl.Lock("key1")
		locked2.Store(true)
		unlock2()
	}()

	// Give the goroutine time to try to acquire the lock
	time.Sleep(50 * time.Millisecond)

	// The second lock should not be acquired yet
	if locked2.Load() {
		t.Error("Second exclusive lock should be blocked while first is held")
	}

	// Release first lock
	unlock1()

	// Wait for second lock to be acquired
	time.Sleep(50 * time.Millisecond)

	// Now the second lock should be acquired
	if !locked2.Load() {
		t.Error("Second exclusive lock should be acquired after first is released")
	}
}

// TestKeyLock_MultipleReadLocks tests that multiple read locks can be held simultaneously
func TestKeyLock_MultipleReadLocks(t *testing.T) {
	kl := NewKeyLock()

	const numReaders = 5
	var wg sync.WaitGroup
	var readersHolding atomic.Int32

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := kl.RLock("key1")
			readersHolding.Add(1)
			time.Sleep(50 * time.Millisecond)
			readersHolding.Add(-1)
			unlock()
		}()
	}

	// Wait a bit for all readers to acquire locks
	time.Sleep(30 * time.Millisecond)

	// All readers should be holding locks simultaneously
	if readersHolding.Load() != numReaders {
		t.Errorf("Expected %d readers holding locks simultaneously, got %d", numReaders, readersHolding.Load())
	}

	wg.Wait()

	// After all readers release, map should be empty
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty after all readers release, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_ReadWriteMutualExclusion tests that read and write locks are mutually exclusive
func TestKeyLock_ReadWriteMutualExclusion(t *testing.T) {
	kl := NewKeyLock()

	// Acquire write lock
	unlockWrite := kl.Lock("key1")

	// Try to acquire read lock from another goroutine
	var readAcquired atomic.Bool
	go func() {
		unlockRead := kl.RLock("key1")
		readAcquired.Store(true)
		unlockRead()
	}()

	// Give the goroutine time to try to acquire the read lock
	time.Sleep(50 * time.Millisecond)

	// Read lock should be blocked
	if readAcquired.Load() {
		t.Error("Read lock should be blocked while write lock is held")
	}

	// Release write lock
	unlockWrite()

	// Wait for read lock to be acquired
	time.Sleep(50 * time.Millisecond)

	// Now read lock should be acquired
	if !readAcquired.Load() {
		t.Error("Read lock should be acquired after write lock is released")
	}
}

// TestKeyLock_WriteLockBlockedByRead tests that write lock is blocked by read locks
func TestKeyLock_WriteLockBlockedByRead(t *testing.T) {
	kl := NewKeyLock()

	// Acquire read lock
	unlockRead := kl.RLock("key1")

	// Try to acquire write lock from another goroutine
	var writeAcquired atomic.Bool
	go func() {
		unlockWrite := kl.Lock("key1")
		writeAcquired.Store(true)
		unlockWrite()
	}()

	// Give the goroutine time to try to acquire the write lock
	time.Sleep(50 * time.Millisecond)

	// Write lock should be blocked
	if writeAcquired.Load() {
		t.Error("Write lock should be blocked while read lock is held")
	}

	// Release read lock
	unlockRead()

	// Wait for write lock to be acquired
	time.Sleep(50 * time.Millisecond)

	// Now write lock should be acquired
	if !writeAcquired.Load() {
		t.Error("Write lock should be acquired after read lock is released")
	}
}

// TestKeyLock_NoMemoryLeak tests that lock entries are properly cleaned up
func TestKeyLock_NoMemoryLeak(t *testing.T) {
	kl := NewKeyLock()

	// Acquire and release many locks
	for i := 0; i < 1000; i++ {
		key := "key"
		unlock := kl.Lock(key)
		unlock()
	}

	// Map should be empty
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty, got %d entries (memory leak)", len(kl.locks))
	}
	kl.mu.Unlock()

	// Do the same with read locks
	for i := 0; i < 1000; i++ {
		key := "key"
		unlock := kl.RLock(key)
		unlock()
	}

	// Map should still be empty
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty, got %d entries (memory leak)", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_ConcurrentAccessDifferentKeys tests concurrent access to different keys
func TestKeyLock_ConcurrentAccessDifferentKeys(t *testing.T) {
	kl := NewKeyLock()

	const numKeys = 100
	const opsPerKey = 10

	var wg sync.WaitGroup

	for i := 0; i < numKeys; i++ {
		key := string(rune('a' + i%26))
		for j := 0; j < opsPerKey; j++ {
			wg.Add(1)
			go func(k string) {
				defer wg.Done()
				if j%2 == 0 {
					unlock := kl.Lock(k)
					time.Sleep(time.Microsecond)
					unlock()
				} else {
					unlock := kl.RLock(k)
					time.Sleep(time.Microsecond)
					unlock()
				}
			}(key)
		}
	}

	wg.Wait()

	// All locks should be released
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty after all operations, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}

// TestKeyLock_RefCountingWithMultipleHolders tests reference counting works correctly
func TestKeyLock_RefCountingWithMultipleHolders(t *testing.T) {
	kl := NewKeyLock()

	// Acquire multiple read locks on the same key
	unlock1 := kl.RLock("key1")
	unlock2 := kl.RLock("key1")
	unlock3 := kl.RLock("key1")

	// Entry should exist with refCount=3
	kl.mu.Lock()
	entry, exists := kl.locks["key1"]
	if !exists {
		t.Fatal("Expected entry to exist for key1")
	}
	if entry.refCount != 3 {
		t.Errorf("Expected refCount=3, got %d", entry.refCount)
	}
	kl.mu.Unlock()

	// Release one lock
	unlock1()

	// Entry should still exist with refCount=2
	kl.mu.Lock()
	entry, exists = kl.locks["key1"]
	if !exists {
		t.Fatal("Expected entry to still exist for key1 after one unlock")
	}
	if entry.refCount != 2 {
		t.Errorf("Expected refCount=2, got %d", entry.refCount)
	}
	kl.mu.Unlock()

	// Release second lock
	unlock2()

	// Entry should still exist with refCount=1
	kl.mu.Lock()
	entry, exists = kl.locks["key1"]
	if !exists {
		t.Fatal("Expected entry to still exist for key1 after two unlocks")
	}
	if entry.refCount != 1 {
		t.Errorf("Expected refCount=1, got %d", entry.refCount)
	}
	kl.mu.Unlock()

	// Release third lock
	unlock3()

	// Entry should be removed
	kl.mu.Lock()
	if len(kl.locks) != 0 {
		t.Errorf("Expected locks map to be empty after all unlocks, got %d entries", len(kl.locks))
	}
	kl.mu.Unlock()
}
