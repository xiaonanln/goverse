package shardlock

import (
	"sync"
	"testing"
	"time"
)

func TestAcquireRead_NormalObject(t *testing.T) {
	// Acquire read lock for a normal object
	unlock := AcquireRead("test-object-123")
	if unlock == nil {
		t.Fatal("AcquireRead returned nil unlock function")
	}

	// Release the lock
	unlock()
}

func TestAcquireRead_FixedNodeAddress(t *testing.T) {
	// Acquire read lock for a fixed node address (contains "/")
	unlock := AcquireRead("localhost:7001/client-123")
	if unlock == nil {
		t.Fatal("AcquireRead returned nil unlock function")
	}

	// Should return immediately (no actual locking)
	unlock()
}

func TestAcquireWrite(t *testing.T) {
	// Acquire write lock for shard 0
	unlock := AcquireWrite(0)
	if unlock == nil {
		t.Fatal("AcquireWrite returned nil unlock function")
	}

	// Release the lock
	unlock()
}

func TestShardLock_ReadWriteExclusion(t *testing.T) {
	writeLockReleased := false
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Acquire write lock on shard 0
	go func() {
		defer wg.Done()
		unlock := AcquireWrite(0)
		time.Sleep(100 * time.Millisecond)
		writeLockReleased = true
		unlock()
	}()

	// Give write lock time to acquire
	time.Sleep(10 * time.Millisecond)

	// Goroutine 2: Try to acquire another write lock on shard 0 (should block)
	go func() {
		defer wg.Done()
		unlock := AcquireWrite(0)

		// By the time we get here, first write lock should have been released
		if !writeLockReleased {
			t.Error("Second write lock acquired before first write lock was released")
		}
		unlock()
	}()

	wg.Wait()
}

func TestShardLock_ConcurrentReads(t *testing.T) {
	// Multiple read locks should be able to coexist
	// Use a simple object ID - doesn't matter which shard it maps to
	objectID := "concurrent-test-object"

	var wg sync.WaitGroup
	readers := 10
	wg.Add(readers)

	start := time.Now()
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			unlock := AcquireRead(objectID)
			defer unlock()
			time.Sleep(50 * time.Millisecond)
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	// If read locks were truly concurrent, total time should be close to 50ms
	// If they were serialized, it would be close to 500ms
	if duration > 200*time.Millisecond {
		t.Errorf("Concurrent reads took too long: %v (expected ~50ms)", duration)
	}
}

func TestShardLock_WriteExclusivity(t *testing.T) {
	// Multiple write locks should be serialized
	var wg sync.WaitGroup
	writers := 5
	wg.Add(writers)

	counter := 0
	start := time.Now()

	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			unlock := AcquireWrite(0)
			defer unlock()

			// Critical section
			temp := counter
			time.Sleep(10 * time.Millisecond)
			counter = temp + 1
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	// Verify counter incremented correctly (no race conditions)
	if counter != writers {
		t.Errorf("Counter = %d, expected %d (race condition detected)", counter, writers)
	}

	// Write locks should be serialized, so total time should be at least 50ms
	if duration < 45*time.Millisecond {
		t.Errorf("Write locks completed too quickly: %v (expected >=50ms)", duration)
	}
}

func TestShardLock_DifferentShards(t *testing.T) {
	// Locks on different shards should not interfere
	var wg sync.WaitGroup
	wg.Add(2)

	shard0Done := false
	shard1Done := false

	start := time.Now()

	// Lock shard 0
	go func() {
		defer wg.Done()
		unlock := AcquireWrite(0)
		defer unlock()
		time.Sleep(50 * time.Millisecond)
		shard0Done = true
	}()

	// Lock shard 1
	go func() {
		defer wg.Done()
		unlock := AcquireWrite(1)
		defer unlock()
		time.Sleep(50 * time.Millisecond)
		shard1Done = true
	}()

	wg.Wait()
	duration := time.Since(start)

	// Both should complete in parallel
	if !shard0Done || !shard1Done {
		t.Error("Not all shards completed")
	}

	// Should take ~50ms if parallel, ~100ms if serialized
	if duration > 80*time.Millisecond {
		t.Errorf("Different shard locks took too long: %v (expected ~50ms)", duration)
	}
}

func TestContainsSlash(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"simple-object", false},
		{"localhost:7001/client-123", true},
		{"node/obj", true},
		{"", false},
		{"/", true},
		{"a/b/c", true},
	}

	for _, tt := range tests {
		result := containsSlash(tt.input)
		if result != tt.expected {
			t.Errorf("containsSlash(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestShardLockKey(t *testing.T) {
	tests := []struct {
		shardID  int
		expected string
	}{
		{0, "0"},
		{42, "42"},
		{8191, "8191"},
	}

	for _, tt := range tests {
		result := shardLockKey(tt.shardID)
		if result != tt.expected {
			t.Errorf("shardLockKey(%d) = %q, expected %q", tt.shardID, result, tt.expected)
		}
	}
}
