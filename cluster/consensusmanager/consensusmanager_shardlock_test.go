package consensusmanager

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestShardLocking_ReadWriteExclusion tests that write locks block read locks
func TestShardLocking_ReadWriteExclusion(t *testing.T) {
	cm := NewConsensusManager(nil)

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
		unlock := cm.shardLock.Lock(shardLockKey(shardID))
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
		unlock := cm.shardLock.RLock(shardLockKey(shardID))
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
	cm := NewConsensusManager(nil)

	objectID := "test-object-456"
	shardID := sharding.GetShardID(objectID)

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
			unlock := cm.shardLock.RLock(shardLockKey(shardID))
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
	cm := NewConsensusManager(nil)

	objectID := "test-object-789"
	shardID := sharding.GetShardID(objectID)

	// Simulate ReleaseShardsForNode acquiring write lock
	releaseStarted := make(chan bool)
	releaseCompleted := make(chan bool)
	var releaseTime time.Time

	go func() {
		unlock := cm.shardLock.Lock(shardLockKey(shardID))
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
		unlock := cm.shardLock.RLock(shardLockKey(shardID))
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
	cm := NewConsensusManager(nil)

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
		unlock := cm.shardLock.Lock(shardLockKey(shardID1))
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
		unlock := cm.shardLock.Lock(shardLockKey(shardID2))
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
	cm := NewConsensusManager(nil)

	// Object with fixed node address (contains "/")
	objectID := "localhost:7000/test-object"

	// Acquire read lock - should be a no-op
	unlock := cm.AcquireShardReadLock(objectID)
	unlock()

	// Verify that no lock entry was created
	if cm.shardLock.Len() != 0 {
		t.Errorf("Expected no lock entries for fixed node address, got %d", cm.shardLock.Len())
	}
}

// TestAcquireShardReadLock_NormalObject tests that normal objects do acquire locks
func TestAcquireShardReadLock_NormalObject(t *testing.T) {
	cm := NewConsensusManager(nil)

	objectID := "test-object-normal"

	// Acquire read lock
	unlock := cm.AcquireShardReadLock(objectID)

	// Verify that a lock entry exists
	if cm.shardLock.Len() != 1 {
		t.Errorf("Expected 1 lock entry, got %d", cm.shardLock.Len())
	}

	unlock()

	// After releasing, the lock should be cleaned up
	if cm.shardLock.Len() != 0 {
		t.Errorf("Expected lock to be cleaned up after release, but got %d entries", cm.shardLock.Len())
	}
}
