package testutil

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

func TestTestNumShards(t *testing.T) {
	if TestNumShards != 64 {
		t.Fatalf("TestNumShards should be 64, got %d", TestNumShards)
	}
}

func TestGetObjectIDForShard(t *testing.T) {
	t.Parallel()

	// Test that GetObjectIDForShard returns IDs that hash to the correct shard
	testCases := []struct {
		shardID int
		prefix  string
	}{
		{0, "TestObject"},
		{10, "User"},
		{32, "Session"},
		{63, "MaxShard"}, // Last valid shard
	}

	for _, tc := range testCases {
		t.Run(tc.prefix, func(t *testing.T) {
			objID := GetObjectIDForShard(tc.shardID, tc.prefix)

			// Verify the object ID contains the prefix
			if len(objID) < len(tc.prefix) || objID[:len(tc.prefix)] != tc.prefix {
				t.Errorf("Expected object ID to start with %q, got %q", tc.prefix, objID)
			}

			// Verify it hashes to the correct shard
			actualShard := sharding.GetShardID(objID, TestNumShards)
			if actualShard != tc.shardID {
				t.Errorf("GetObjectIDForShard(%d, %q) = %q, which hashes to shard %d, want shard %d",
					tc.shardID, tc.prefix, objID, actualShard, tc.shardID)
			}

			t.Logf("Shard %d: %q", tc.shardID, objID)
		})
	}
}

func TestGetObjectIDForShard_AllShards(t *testing.T) {
	t.Parallel()

	// Test that we can generate IDs for all shards
	prefix := "Test"
	for shardID := 0; shardID < TestNumShards; shardID++ {
		objID := GetObjectIDForShard(shardID, prefix)
		actualShard := sharding.GetShardID(objID, TestNumShards)
		if actualShard != shardID {
			t.Errorf("Shard %d: GetObjectIDForShard returned %q which hashes to shard %d",
				shardID, objID, actualShard)
		}
	}
}

func TestGetObjectIDForShard_InvalidShard(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		shardID int
	}{
		{"negative", -1},
		{"too large", TestNumShards},
		{"way too large", 1000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("GetObjectIDForShard(%d) should panic, but didn't", tc.shardID)
				}
			}()
			GetObjectIDForShard(tc.shardID, "Test")
		})
	}
}

func TestGetObjectIDForShard_Consistency(t *testing.T) {
	t.Parallel()

	// Test that calling GetObjectIDForShard multiple times returns consistent results
	shardID := 5
	prefix := "Consistent"

	id1 := GetObjectIDForShard(shardID, prefix)
	id2 := GetObjectIDForShard(shardID, prefix)

	// Both should hash to the same shard
	shard1 := sharding.GetShardID(id1, TestNumShards)
	shard2 := sharding.GetShardID(id2, TestNumShards)

	if shard1 != shardID {
		t.Errorf("First call: expected shard %d, got %d", shardID, shard1)
	}
	if shard2 != shardID {
		t.Errorf("Second call: expected shard %d, got %d", shardID, shard2)
	}
}
