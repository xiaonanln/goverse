package consensusmanager

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
)

// shardLockKey converts a shard ID to a lock key for testing
func shardLockKey(shardID int) string {
	return strconv.Itoa(shardID)
}

// TestShardLocking_ReadWriteExclusion tests that write locks block read locks
func TestShardLocking_ReadWriteExclusion(t *testing.T) {
	sl := shardlock.NewShardLock()

	// Test object ID that maps to a specific shard
	objectID := "test-object-123"
	shardID := sharding.GetShardID(objectID)

	// Track operation order
	var operationOrder []string
	var mu sync.Mutex

	recordOp := func(op string) {
		mu.Lock()
		operationOrder = append(operationOrder, op)
		mu.Unlock()
	}

	// Acquire write lock (simulating ReleaseShardsForNode)
	writeLockAcquired := make(chan bool)
	writeLockReleased := make(chan bool)
	go func() {
		unlock := sl.AcquireWrite(shardID)
		recordOp("write-acquired")
		close(writeLockAcquired)
		time.Sleep(100 * time.Millisecond) // Hold lock briefly
		recordOp("write-released")
		unlock()
		close(writeLockReleased)
	}()

	// Wait for write lock to be acquired
	<-writeLockAcquired

	// Try to acquire read lock (simulating CreateObject/CallObject)
	readLockAcquired := make(chan bool)
	go func() {
		time.Sleep(10 * time.Millisecond) // Ensure we try after write lock is held
		unlock := sl.AcquireRead(objectID)
		recordOp("read-acquired")
		close(readLockAcquired)
		unlock()
		recordOp("read-released")
	}()

	// Wait for operations to complete
	<-writeLockReleased
	<-readLockAcquired

	// Give a moment for all operations to be recorded
	time.Sleep(10 * time.Millisecond)

	// Verify order: write lock must be released before read lock is acquired
	mu.Lock()
	defer mu.Unlock()

	if len(operationOrder) != 4 {
		t.Fatalf("Expected 4 operations, got %d: %v", len(operationOrder), operationOrder)
	}

	// Verify proper ordering
	expectedOrder := []string{"write-acquired", "write-released", "read-acquired", "read-released"}
	for i, expected := range expectedOrder {
		if operationOrder[i] != expected {
			t.Errorf("Operation %d: expected %s, got %s. Full order: %v",
				i, expected, operationOrder[i], operationOrder)
		}
	}
}

// TestShardLocking_MultipleReadLocks tests that multiple read locks can be held concurrently
func TestShardLocking_MultipleReadLocks(t *testing.T) {
	sl := shardlock.NewShardLock()
	objectID := "test-object-456"

	// Counter for concurrent readers
	var concurrentReaders atomic.Int32
	var maxConcurrentReaders atomic.Int32

	// Start multiple readers
	numReaders := 5
	var wg sync.WaitGroup
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			unlock := sl.AcquireRead(objectID)
			defer unlock()

			// Track concurrent readers
			current := concurrentReaders.Add(1)
			time.Sleep(50 * time.Millisecond)

			// Update max if we have more concurrent readers
			for {
				max := maxConcurrentReaders.Load()
				if current <= max || maxConcurrentReaders.CompareAndSwap(max, current) {
					break
				}
			}

			concurrentReaders.Add(-1)
		}()
	}

	wg.Wait()

	// Verify that we had multiple concurrent readers
	max := maxConcurrentReaders.Load()
	if max < 2 {
		t.Errorf("Expected at least 2 concurrent readers, got %d", max)
	}
}

// TestShardLocking_ReleaseBlocksCreate simulates the race condition scenario
func TestShardLocking_ReleaseBlocksCreate(t *testing.T) {
	sl := shardlock.NewShardLock()
	objectID := "test-object-789"
	shardID := sharding.GetShardID(objectID)

	// Simulate ReleaseShardsForNode acquiring write lock
	releaseStarted := make(chan bool)
	releaseCompleted := make(chan bool)
	var releaseTime time.Time

	go func() {
		unlock := sl.AcquireWrite(shardID)
		close(releaseStarted)
		time.Sleep(100 * time.Millisecond) // Simulate etcd operations
		releaseTime = time.Now()
		unlock()
		close(releaseCompleted)
	}()

	// Wait for release to start
	<-releaseStarted

	// Simulate CreateObject trying to acquire read lock
	createStarted := make(chan bool)
	createAcquiredLock := make(chan bool)
	var createTime time.Time

	go func() {
		close(createStarted)
		unlock := sl.AcquireRead(objectID)
		createTime = time.Now()
		close(createAcquiredLock)
		unlock()
	}()

	// Wait for create to start
	<-createStarted

	// Wait for both to complete
	<-releaseCompleted
	<-createAcquiredLock

	// Verify that create acquired lock AFTER release released it
	if createTime.Before(releaseTime) {
		t.Errorf("CreateObject acquired lock before ReleaseShardsForNode released it. "+
			"Release: %v, Create: %v", releaseTime, createTime)
	}
}

// TestShardLocking_DifferentShardsNoBlocking tests that operations on different shards don't block
func TestShardLocking_DifferentShardsNoBlocking(t *testing.T) {
	sl := shardlock.NewShardLock()

	// Use object IDs that map to different shards
	objectID1 := "test-object-1"
	objectID2 := "test-object-2"
	shardID1 := sharding.GetShardID(objectID1)
	shardID2 := sharding.GetShardID(objectID2)

	// Make sure they're different shards
	if shardID1 == shardID2 {
		// Try different IDs
		objectID2 = "test-object-9999"
		shardID2 = sharding.GetShardID(objectID2)
		if shardID1 == shardID2 {
			t.Skip("Cannot find two different shard IDs for testing")
		}
	}

	// Track concurrent operations
	var concurrentOps atomic.Int32
	var maxConcurrentOps atomic.Int32

	var wg sync.WaitGroup
	wg.Add(2)

	// Operation on shard 1 (write lock)
	go func() {
		defer wg.Done()
		unlock := sl.AcquireWrite(shardID1)
		defer unlock()

		current := concurrentOps.Add(1)
		time.Sleep(50 * time.Millisecond)

		for {
			max := maxConcurrentOps.Load()
			if current <= max || maxConcurrentOps.CompareAndSwap(max, current) {
				break
			}
		}
		concurrentOps.Add(-1)
	}()

	// Operation on shard 2 (write lock)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // Start slightly after first one
		unlock := sl.AcquireWrite(shardID2)
		defer unlock()

		current := concurrentOps.Add(1)
		time.Sleep(50 * time.Millisecond)

		for {
			max := maxConcurrentOps.Load()
			if current <= max || maxConcurrentOps.CompareAndSwap(max, current) {
				break
			}
		}
		concurrentOps.Add(-1)
	}()

	wg.Wait()

	// Verify that operations on different shards ran concurrently
	max := maxConcurrentOps.Load()
	if max < 2 {
		t.Errorf("Expected operations on different shards to run concurrently, but max concurrent ops was %d", max)
	}
}

// TestAcquireShardReadLock_FixedNodeAddress tests that fixed node addresses don't acquire locks
func TestAcquireShardReadLock_FixedNodeAddress(t *testing.T) {
	sl := shardlock.NewShardLock()

	// Object with fixed node address (contains "/")
	objectID := "localhost:7000/test-object"

	// Acquire read lock - should be a no-op (returns immediately)
	unlock := sl.AcquireRead(objectID)
	unlock()

	// For fixed node addresses, the lock acquisition is a no-op
	// We can't verify internal state, but the test passes if it doesn't hang
}

// TestAcquireShardReadLock_NormalObject tests that normal objects do acquire locks
func TestAcquireShardReadLock_NormalObject(t *testing.T) {
	sl := shardlock.NewShardLock()
	objectID := "test-object-normal"

	// Acquire read lock
	unlock := sl.AcquireRead(objectID)

	// Verify we can acquire and release the lock without errors
	// (if there's a problem, the test will hang or panic)
	unlock()

	// Lock acquisition and release works correctly
}
