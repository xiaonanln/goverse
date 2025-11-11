package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestGetObjectsToEvict tests the GetObjectsToEvict method
func TestGetObjectsToEvict(t *testing.T) {
	t.Parallel()

	cm := NewConsensusManager(nil)

	// Set up a test shard mapping
	testMapping := &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shard 0: CurrentNode is node1, TargetNode is node2 (should be evicted from node1)
	testMapping.Shards[0] = ShardInfo{
		TargetNode:  "localhost:50002",
		CurrentNode: "localhost:50001",
	}

	// Shard 1: CurrentNode is node1, TargetNode is also node1 (should NOT be evicted)
	testMapping.Shards[1] = ShardInfo{
		TargetNode:  "localhost:50001",
		CurrentNode: "localhost:50001",
	}

	// Shard 2: CurrentNode is node1, TargetNode is node3 (should be evicted from node1)
	testMapping.Shards[2] = ShardInfo{
		TargetNode:  "localhost:50003",
		CurrentNode: "localhost:50001",
	}

	cm.SetMappingForTesting(testMapping)

	// Find object IDs that map to our test shards
	objectIDs := []string{}
	objToShard := make(map[string]int)
	
	for i := 0; i < 100000 && len(objectIDs) < 4; i++ {
		testID := "TestObject-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		shardID := sharding.GetShardID(testID)
		if shardID == 0 || shardID == 1 || shardID == 2 {
			if _, exists := objToShard[testID]; !exists {
				objectIDs = append(objectIDs, testID)
				objToShard[testID] = shardID
			}
		}
	}
	
	// Add client object
	objectIDs = append(objectIDs, "localhost:50001/client-123")

	// Get objects to evict from node1
	toEvict := cm.GetObjectsToEvict("localhost:50001", objectIDs)

	// Count how many objects should be evicted based on their shards
	expectedEvictions := 0
	for _, objID := range objectIDs {
		if shardID, exists := objToShard[objID]; exists {
			if shardID == 0 || shardID == 2 {
				expectedEvictions++
			}
		}
	}

	if len(toEvict) != expectedEvictions {
		t.Errorf("Expected %d objects to evict, got %d (found objects mapping to shards: %v)", expectedEvictions, len(toEvict), objToShard)
	}

	// Verify that evicted objects are from shards 0 and 2, not shard 1
	for _, objID := range toEvict {
		shardID := sharding.GetShardID(objID)
		if shardID != 0 && shardID != 2 {
			t.Errorf("Object %s with shard %d should not be evicted", objID, shardID)
		}
	}
}

// TestGetObjectsToEvict_EmptyMapping tests with no shard mapping
func TestGetObjectsToEvict_EmptyMapping(t *testing.T) {
	t.Parallel()

	cm := NewConsensusManager(nil)

	objectIDs := []string{"TestObject-123"}
	toEvict := cm.GetObjectsToEvict("localhost:50001", objectIDs)

	if len(toEvict) != 0 {
		t.Errorf("Expected no objects to evict with empty mapping, got %d", len(toEvict))
	}
}

// TestGetObjectsToEvict_ClientObjectsSkipped tests that client objects are skipped
func TestGetObjectsToEvict_ClientObjectsSkipped(t *testing.T) {
	t.Parallel()

	cm := NewConsensusManager(nil)

	testMapping := &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Set up all shards to need eviction
	for i := 0; i < sharding.NumShards; i++ {
		testMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:50002",
			CurrentNode: "localhost:50001",
		}
	}

	cm.SetMappingForTesting(testMapping)

	// Only provide client objects
	objectIDs := []string{
		"localhost:50001/client-123",
		"localhost:50001/client-456",
	}

	toEvict := cm.GetObjectsToEvict("localhost:50001", objectIDs)

	if len(toEvict) != 0 {
		t.Errorf("Expected no client objects to be evicted, got %d", len(toEvict))
	}
}
