package testutil

import (
	"fmt"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestNumShards is the number of shards to use in tests.
// Using a smaller number (64) instead of production default (8192)
// makes tests faster and reduces resource usage.
const TestNumShards = 64

// GetObjectIDForShard returns an object ID that hashes to the specified shard.
// The prefix parameter allows customizing the object ID format (e.g., "TestObject", "User").
// This is useful for tests that need objects on specific shards.
//
// Example:
//
//	objID := testutil.GetObjectIDForShard(10, "TestObject")
//	// Returns something like "TestObject-10-0" that hashes to shard 10
func GetObjectIDForShard(targetShardID int, prefix string) string {
	if targetShardID < 0 || targetShardID >= TestNumShards {
		panic(fmt.Sprintf("targetShardID %d out of range [0, %d)", targetShardID, TestNumShards))
	}

	// Try different suffixes until we find one that hashes to the target shard
	for i := 0; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d-%d", prefix, targetShardID, i)
		if sharding.GetShardID(candidate, TestNumShards) == targetShardID {
			return candidate
		}
	}

	// Fallback: if we can't find one quickly, use a deterministic format
	// This should rarely happen with FNV hash distribution
	panic(fmt.Sprintf("failed to find object ID for shard %d after 10000 attempts", targetShardID))
}
