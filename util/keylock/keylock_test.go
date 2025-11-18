package keylock

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestKeyLock_BasicLock tests basic exclusive locking
func TestKeyLock_BasicLock(t *testing.T) {
	kl := NewKeyLock()

	unlock := kl.Lock("key1")
	defer unlock()

	// Verify entry exists
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry, got %d", kl.Len())
	}
}

// TestKeyLock_BasicRLock tests basic shared locking
func TestKeyLock_BasicRLock(t *testing.T) {
	kl := NewKeyLock()

	unlock := kl.RLock("key1")
	defer unlock()

	// Verify entry exists
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry, got %d", kl.Len())
	}
}

// TestKeyLock_AutoCleanup tests that entries are removed when no longer in use
func TestKeyLock_AutoCleanup(t *testing.T) {
	kl := NewKeyLock()

	// Acquire and release lock
	unlock := kl.Lock("key1")
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry, got %d", kl.Len())
	}
	unlock()

	// Entry should be cleaned up
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after unlock, got %d", kl.Len())
	}
}

// TestKeyLock_MultipleKeys tests locking different keys simultaneously
func TestKeyLock_MultipleKeys(t *testing.T) {
	kl := NewKeyLock()

	unlock1 := kl.Lock("key1")
	unlock2 := kl.Lock("key2")
	unlock3 := kl.RLock("key3")

	if kl.Len() != 3 {
		t.Fatalf("Expected 3 lock entries, got %d", kl.Len())
	}

	unlock1()
	if kl.Len() != 2 {
		t.Fatalf("Expected 2 lock entries after unlock1, got %d", kl.Len())
	}

	unlock2()
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry after unlock2, got %d", kl.Len())
	}

	unlock3()
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after unlock3, got %d", kl.Len())
	}
}

// TestKeyLock_ConcurrentReaders tests multiple concurrent readers
func TestKeyLock_ConcurrentReaders(t *testing.T) {
	kl := NewKeyLock()
	const numReaders = 100

	var wg sync.WaitGroup
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			unlock := kl.RLock("key1")
			defer unlock()
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
		}()
	}

	wg.Wait()

	// All readers should have finished and cleaned up
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after all readers finished, got %d", kl.Len())
	}
}

// TestKeyLock_ExclusionBetweenWriters tests that writers exclude each other
func TestKeyLock_ExclusionBetweenWriters(t *testing.T) {
	kl := NewKeyLock()
	var counter int32

	const numWriters = 50
	var wg sync.WaitGroup
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func() {
			defer wg.Done()
			unlock := kl.Lock("key1")
			defer unlock()

			// Critical section - increment counter
			current := atomic.LoadInt32(&counter)
			time.Sleep(5 * time.Millisecond) // Simulate work
			atomic.StoreInt32(&counter, current+1)
		}()
	}

	wg.Wait()

	// All writers should have completed
	if counter != int32(numWriters) {
		t.Fatalf("Expected counter to be %d, got %d", numWriters, counter)
	}

	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after cleanup, got %d", kl.Len())
	}
}

// TestKeyLock_WriterExcludesReaders tests that a writer excludes readers
func TestKeyLock_WriterExcludesReaders(t *testing.T) {
	kl := NewKeyLock()
	var writerInCritical atomic.Bool
	var readerSeenWriterInCritical atomic.Bool

	const numReaders = 20
	var wg sync.WaitGroup
	wg.Add(numReaders + 1)

	// Writer goroutine
	go func() {
		defer wg.Done()
		unlock := kl.Lock("key1")
		defer unlock()

		writerInCritical.Store(true)
		time.Sleep(50 * time.Millisecond)
		writerInCritical.Store(false)
	}()

	// Wait a bit for writer to start
	time.Sleep(10 * time.Millisecond)

	// Multiple reader goroutines
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			unlock := kl.RLock("key1")
			defer unlock()

			// If writer was in critical section, we failed exclusion
			if writerInCritical.Load() {
				readerSeenWriterInCritical.Store(true)
			}
		}()
	}

	wg.Wait()

	if readerSeenWriterInCritical.Load() {
		t.Fatal("Reader was able to enter critical section while writer held lock")
	}
}

// TestKeyLock_HighConcurrency tests with many goroutines and keys
func TestKeyLock_HighConcurrency(t *testing.T) {
	kl := NewKeyLock()
	const numGoroutines = 100
	const numKeys = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			key := "key" + string(rune('0'+(id%numKeys)))

			if id%3 == 0 {
				// Writer
				unlock := kl.Lock(key)
				time.Sleep(time.Microsecond)
				unlock()
			} else {
				// Reader
				unlock := kl.RLock(key)
				time.Sleep(time.Microsecond)
				unlock()
			}
		}(i)
	}

	wg.Wait()

	// All locks should be cleaned up
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after cleanup, got %d", kl.Len())
	}
}

// TestKeyLock_NoDeadlock tests that the same goroutine can acquire locks on different keys
func TestKeyLock_NoDeadlock(t *testing.T) {
	kl := NewKeyLock()

	done := make(chan bool, 1)

	go func() {
		unlock1 := kl.Lock("key1")
		defer unlock1()

		unlock2 := kl.Lock("key2")
		defer unlock2()

		done <- true
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("Deadlock detected: goroutine did not complete")
	}
}

// TestKeyLock_ReferenceCountingUnderConcurrency tests ref counting with concurrent operations
func TestKeyLock_ReferenceCountingUnderConcurrency(t *testing.T) {
	kl := NewKeyLock()
	const numGoroutines = 50

	// Use a channel to synchronize start
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// All goroutines lock the same key
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			<-start // Wait for signal to start
			unlock := kl.RLock("shared-key")
			time.Sleep(20 * time.Millisecond)
			unlock()
		}()
	}

	// Signal all goroutines to start
	close(start)
	time.Sleep(10 * time.Millisecond) // Give them time to acquire locks

	// While goroutines are running, should have 1 entry
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry while goroutines are running, got %d", kl.Len())
	}

	wg.Wait()

	// After all unlock, should be cleaned up
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after all goroutines finished, got %d", kl.Len())
	}
}

// TestKeyLock_NestedLocks tests that acquiring the same lock twice in the same goroutine works
// Note: This tests the unlock function behavior - each Lock() call gets its own unlock
func TestKeyLock_NestedLocks(t *testing.T) {
	kl := NewKeyLock()

	// First lock
	unlock1 := kl.Lock("key1")
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry after first lock, got %d", kl.Len())
	}

	// Nested lock (same goroutine, different invocation)
	// This will block if we try to acquire the same lock again
	// So we test with a different key to ensure no deadlock in the manager itself
	unlock2 := kl.Lock("key2")
	if kl.Len() != 2 {
		t.Fatalf("Expected 2 lock entries after second lock, got %d", kl.Len())
	}

	unlock2()
	if kl.Len() != 1 {
		t.Fatalf("Expected 1 lock entry after unlock2, got %d", kl.Len())
	}

	unlock1()
	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after unlock1, got %d", kl.Len())
	}
}

// TestKeyLock_RaceDetector runs with -race flag to detect any data races
func TestKeyLock_RaceDetector(t *testing.T) {
	kl := NewKeyLock()
	const numGoroutines = 20
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune('0'+(j%5)))
				if (id+j)%2 == 0 {
					unlock := kl.Lock(key)
					unlock()
				} else {
					unlock := kl.RLock(key)
					unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	if kl.Len() != 0 {
		t.Fatalf("Expected 0 lock entries after cleanup, got %d", kl.Len())
	}
}
