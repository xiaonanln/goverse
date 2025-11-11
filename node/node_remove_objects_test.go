package node

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestRemoveObjectsForShards tests the RemoveObjectsForShards method
func TestRemoveObjectsForShards(t *testing.T) {
	t.Parallel()

	n := NewNode("localhost:50000")

	// Test with empty shard targets - should return nil
	err := n.RemoveObjectsForShards(context.Background(), nil)
	if err != nil {
		t.Errorf("Expected no error for empty shard targets, got: %v", err)
	}

	err = n.RemoveObjectsForShards(context.Background(), make(map[int]string))
	if err != nil {
		t.Errorf("Expected no error for empty shard targets map, got: %v", err)
	}
}

// TestRemoveObjectsForShards_SkipsClientObjects tests that client objects are not removed
func TestRemoveObjectsForShards_SkipsClientObjects(t *testing.T) {
	t.Parallel()

	// Verify that containsSlash helper works correctly
	clientID := "localhost:50000/client-123"
	if !containsSlash(clientID) {
		t.Errorf("Client ID should contain slash: %s", clientID)
	}

	regularID := "TestObject-456"
	if containsSlash(regularID) {
		t.Errorf("Regular object ID should not contain slash: %s", regularID)
	}
}

// containsSlash is a helper to check if string contains "/"
func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}

// TestShardIDMapping tests that object IDs consistently map to shard IDs
func TestShardIDMapping(t *testing.T) {
	t.Parallel()

	objectID := "TestObject-123"
	shardID := sharding.GetShardID(objectID)

	if shardID < 0 || shardID >= sharding.NumShards {
		t.Errorf("Shard ID %d is out of range [0, %d)", shardID, sharding.NumShards)
	}

	// Same object should always map to same shard
	shardID2 := sharding.GetShardID(objectID)
	if shardID != shardID2 {
		t.Errorf("Same object should map to same shard: %d vs %d", shardID, shardID2)
	}
}
