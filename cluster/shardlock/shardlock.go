package shardlock

import (
	"slices"
	"strconv"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/keylock"
)

// ShardLock provides per-cluster shard-level locking to prevent lock collisions between clusters
type ShardLock struct {
	keyLock   *keylock.KeyLock
	numShards int
}

// NewShardLock creates a new ShardLock instance with specified number of shards
func NewShardLock(numShards int) *ShardLock {
	return &ShardLock{
		keyLock:   keylock.NewKeyLock(),
		numShards: numShards,
	}
}

// AcquireRead acquires a read lock on the shard for the given object ID.
// This prevents concurrent shard ownership transitions during object operations.
// Returns an unlock function that MUST be called to release the lock.
// For objects with fixed node addresses (containing "/"), no lock is acquired.
func (sl *ShardLock) AcquireRead(objectID string) func() {
	// Skip locking for fixed node addresses (e.g., client objects with format "node/id")
	if containsSlash(objectID) {
		return func() {} // No-op unlock
	}

	shardID := sharding.GetShardID(objectID, sl.numShards)
	return sl.keyLock.RLock(shardLockKey(shardID))
}

// AcquireWrite acquires a write lock on the given shard ID.
// This prevents concurrent object operations during shard ownership transitions.
// Returns an unlock function that MUST be called to release the lock.
func (sl *ShardLock) AcquireWrite(shardID int) func() {
	return sl.keyLock.Lock(shardLockKey(shardID))
}

// AcquireWriteMultiple acquires write locks on multiple shard IDs in sorted order.
// This prevents concurrent object operations during shard ownership transitions.
// Locks are acquired in ascending shard ID order to prevent deadlock.
// Returns a single unlock function that releases all locks when called.
// The unlock function MUST be called to release all locks.
func (sl *ShardLock) AcquireWriteMultiple(shardIDs []int) func() {
	if len(shardIDs) == 0 {
		return func() {} // No-op unlock
	}

	// Create a sorted copy to avoid modifying the caller's slice
	sortedIDs := make([]int, len(shardIDs))
	copy(sortedIDs, shardIDs)
	slices.Sort(sortedIDs)

	// Acquire all locks in sorted order
	unlockFuncs := make([]func(), 0, len(sortedIDs))
	for _, shardID := range sortedIDs {
		unlock := sl.keyLock.Lock(shardLockKey(shardID))
		unlockFuncs = append(unlockFuncs, unlock)
	}

	// Return a function that releases all locks
	return func() {
		for _, unlock := range unlockFuncs {
			unlock()
		}
	}
}

// shardLockKey converts a shard ID to a lock key for shard-level locking
func shardLockKey(shardID int) string {
	return strconv.Itoa(shardID)
}

// containsSlash checks if a string contains a "/" character
// This is used to identify fixed node addresses
func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}
