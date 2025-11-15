package shardlock

import (
	"strconv"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/keylock"
)

// Global KeyLock instance for shard-level locking
var globalKeyLock = keylock.NewKeyLock()

// AcquireRead acquires a read lock on the shard for the given object ID.
// This prevents concurrent shard ownership transitions during object operations.
// Returns an unlock function that MUST be called to release the lock.
// For objects with fixed node addresses (containing "/"), no lock is acquired.
func AcquireRead(objectID string) func() {
	// Skip locking for fixed node addresses (e.g., client objects with format "node/id")
	if containsSlash(objectID) {
		return func() {} // No-op unlock
	}

	shardID := sharding.GetShardID(objectID)
	return globalKeyLock.RLock(shardLockKey(shardID))
}

// AcquireWrite acquires a write lock on the given shard ID.
// This prevents concurrent object operations during shard ownership transitions.
// Returns an unlock function that MUST be called to release the lock.
func AcquireWrite(shardID int) func() {
	return globalKeyLock.Lock(shardLockKey(shardID))
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
