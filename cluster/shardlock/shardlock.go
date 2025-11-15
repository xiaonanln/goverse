package shardlock

import (
	"strconv"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/keylock"
)

// ShardLock provides per-cluster shard-level locking to prevent lock collisions between clusters
type ShardLock struct {
	keyLock *keylock.KeyLock
}

// NewShardLock creates a new ShardLock instance for a cluster
func NewShardLock() *ShardLock {
	return &ShardLock{
		keyLock: keylock.NewKeyLock(),
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

	shardID := sharding.GetShardID(objectID)
	return sl.keyLock.RLock(shardLockKey(shardID))
}

// AcquireWrite acquires a write lock on the given shard ID.
// This prevents concurrent object operations during shard ownership transitions.
// Returns an unlock function that MUST be called to release the lock.
func (sl *ShardLock) AcquireWrite(shardID int) func() {
	return sl.keyLock.Lock(shardLockKey(shardID))
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
