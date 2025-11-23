package cluster

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestConfigurableNumShards verifies that numShards can be configured
func TestConfigurableNumShards(t *testing.T) {
	t.Parallel()

	// Test with custom shard count
	customShards := 4096
	cfg := DefaultConfig()
	cfg.NumShards = customShards

	if cfg.NumShards != customShards {
		t.Fatalf("Expected NumShards to be %d, got %d", customShards, cfg.NumShards)
	}

	// Verify GetShardID works with custom numShards
	objectID := "test-object-123"
	shardID := sharding.GetShardID(objectID, customShards)

	if shardID < 0 || shardID >= customShards {
		t.Fatalf("GetShardID(%s, %d) = %d, want value in range [0, %d)", objectID, customShards, shardID, customShards)
	}

	// Verify consistency with custom numShards
	shardID2 := sharding.GetShardID(objectID, customShards)
	if shardID != shardID2 {
		t.Fatalf("GetShardID should be consistent: got %d and %d", shardID, shardID2)
	}

	// Verify different numShards can produce different shard IDs
	shardID3 := sharding.GetShardID(objectID, sharding.NumShards)
	// They may or may not be different, but both should be valid
	if shardID3 < 0 || shardID3 >= sharding.NumShards {
		t.Fatalf("GetShardID with default NumShards should return valid shard ID, got %d", shardID3)
	}
}
