package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGetObjectsToEvict tests the GetObjectsToEvict method
func TestGetObjectsToEvict(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	addr3 := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	// Set up a test cluster state with nodes
	cm.mu.Lock()
	cm.state = &ClusterState{
		Nodes: map[string]bool{
			addr1: true,
			addr2: true,
			addr3: true,
		},
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
	}

	// Shard 0: CurrentNode is node1, TargetNode is node2 (should be evicted from node1)
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  addr2,
		CurrentNode: addr1,
	}

	// Shard 1: CurrentNode is node1, TargetNode is also node1 (should NOT be evicted)
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  addr1,
		CurrentNode: addr1,
	}

	// Shard 2: CurrentNode is node1, TargetNode is node3 (should be evicted from node1)
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  addr3,
		CurrentNode: addr1,
	}
	cm.mu.Unlock()

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
	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

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
		t.Fatalf("Expected %d objects to evict, got %d (found objects mapping to shards: %v)", expectedEvictions, len(toEvict), objToShard)
	}

	// Verify that evicted objects are from shards 0 and 2, not shard 1
	for _, objID := range toEvict {
		shardID := sharding.GetShardID(objID)
		if shardID != 0 && shardID != 2 {
			t.Fatalf("Object %s with shard %d should not be evicted", objID, shardID)
		}
	}
}

// TestGetObjectsToEvict_EmptyMapping tests with no shard mapping
func TestGetObjectsToEvict_EmptyMapping(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	objectIDs := []string{"TestObject-123"}
	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

	if len(toEvict) != 0 {
		t.Fatalf("Expected no objects to evict with empty mapping, got %d", len(toEvict))
	}
}

// TestGetObjectsToEvict_ClientObjectsSkipped tests that client objects are skipped
func TestGetObjectsToEvict_ClientObjectsSkipped(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	// Set up cluster state with nodes
	cm.mu.Lock()
	cm.state = &ClusterState{
		Nodes: map[string]bool{
			addr1: true,
			addr2: true,
		},
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
	}

	// Set up all shards to need eviction
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  addr2,
			CurrentNode: addr1,
		}
	}
	cm.mu.Unlock()

	// Only provide client objects
	objectIDs := []string{
		"localhost:50001/client-123",
		"localhost:50001/client-456",
	}

	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

	if len(toEvict) != 0 {
		t.Fatalf("Expected no client objects to be evicted, got %d", len(toEvict))
	}
}

// TestGetObjectsToEvict_ShardNotExist tests eviction when shard doesn't exist
func TestGetObjectsToEvict_ShardNotExist(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	// Set up cluster state with nodes but incomplete shard mapping
	cm.mu.Lock()
	cm.state = &ClusterState{
		Nodes: map[string]bool{
			addr1: true,
			addr2: true,
		},
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
	}

	// Only set up shard 0, leave shard 1 and 2 unmapped
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  addr1,
		CurrentNode: addr1,
	}
	cm.mu.Unlock()

	// Find object IDs that map to shards 0, 1, and 2
	objectIDs := []string{}
	objToShard := make(map[string]int)

	for i := 0; i < 100000 && len(objToShard) < 3; i++ {
		testID := "TestObject-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		shardID := sharding.GetShardID(testID)
		if shardID == 0 || shardID == 1 || shardID == 2 {
			if _, exists := objToShard[testID]; !exists {
				objectIDs = append(objectIDs, testID)
				objToShard[testID] = shardID
			}
		}
	}

	// Get objects to evict from node1
	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

	// Expected: Objects from shard 1 and 2 should be evicted (no mapping exists)
	// Object from shard 0 should NOT be evicted (both CurrentNode and TargetNode are node1)
	expectedEvictions := 0
	for _, objID := range objectIDs {
		if shardID, exists := objToShard[objID]; exists {
			if shardID == 1 || shardID == 2 {
				expectedEvictions++
			}
		}
	}

	if len(toEvict) != expectedEvictions {
		t.Fatalf("Expected %d objects to evict (shards without mapping), got %d (found objects: %v)", expectedEvictions, len(toEvict), objToShard)
	}

	// Verify that evicted objects are from shards 1 and 2, not shard 0
	for _, objID := range toEvict {
		shardID := sharding.GetShardID(objID)
		if shardID != 1 && shardID != 2 {
			t.Fatalf("Object %s with shard %d should not be evicted", objID, shardID)
		}
	}
}

// TestGetObjectsToEvict_CurrentNodeMismatch tests eviction when CurrentNode != localAddr
func TestGetObjectsToEvict_CurrentNodeMismatch(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	// Set up cluster state with nodes
	cm.mu.Lock()
	cm.state = &ClusterState{
		Nodes: map[string]bool{
			addr1: true,
			addr2: true,
		},
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
	}

	// Shard 0: Both TargetNode and CurrentNode are node2, but we're checking on node1
	// This object should be evicted from node1
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  addr2,
		CurrentNode: addr2,
	}

	// Shard 1: Both TargetNode and CurrentNode are node1 (should NOT be evicted)
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  addr1,
		CurrentNode: addr1,
	}

	// Shard 2: TargetNode is node1 but CurrentNode is node2 (should be evicted)
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  addr1,
		CurrentNode: addr2,
	}
	cm.mu.Unlock()

	// Find object IDs that map to our test shards
	objectIDs := []string{}
	objToShard := make(map[string]int)

	for i := 0; i < 100000 && len(objToShard) < 3; i++ {
		testID := "TestObject-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		shardID := sharding.GetShardID(testID)
		if shardID == 0 || shardID == 1 || shardID == 2 {
			if _, exists := objToShard[testID]; !exists {
				objectIDs = append(objectIDs, testID)
				objToShard[testID] = shardID
			}
		}
	}

	// Get objects to evict from node1
	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

	// Expected: Objects from shard 0 and 2 should be evicted (CurrentNode != node1)
	// Object from shard 1 should NOT be evicted (both CurrentNode and TargetNode are node1)
	expectedEvictions := 0
	for _, objID := range objectIDs {
		if shardID, exists := objToShard[objID]; exists {
			if shardID == 0 || shardID == 2 {
				expectedEvictions++
			}
		}
	}

	if len(toEvict) != expectedEvictions {
		t.Fatalf("Expected %d objects to evict (CurrentNode mismatch), got %d (found objects: %v)", expectedEvictions, len(toEvict), objToShard)
	}

	// Verify that evicted objects are from shards 0 and 2, not shard 1
	for _, objID := range toEvict {
		shardID := sharding.GetShardID(objID)
		if shardID != 0 && shardID != 2 {
			t.Fatalf("Object %s with shard %d should not be evicted", objID, shardID)
		}
	}
}

// TestGetObjectsToEvict_TargetNodeMismatch tests eviction when TargetNode != localAddr
func TestGetObjectsToEvict_TargetNodeMismatch(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	t.Parallel()

	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "")

	// Set up cluster state with nodes
	cm.mu.Lock()
	cm.state = &ClusterState{
		Nodes: map[string]bool{
			addr1: true,
			addr2: true,
		},
		ShardMapping: &ShardMapping{
			Shards: make(map[int]ShardInfo),
		},
	}

	// Shard 0: CurrentNode is node1, TargetNode is node2 (should be evicted - migration target is different)
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  addr2,
		CurrentNode: addr1,
	}

	// Shard 1: Both TargetNode and CurrentNode are node1 (should NOT be evicted)
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  addr1,
		CurrentNode: addr1,
	}

	// Shard 2: CurrentNode is empty but TargetNode is node2 (should be evicted - target is different)
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  addr2,
		CurrentNode: "",
	}
	cm.mu.Unlock()

	// Find object IDs that map to our test shards
	objectIDs := []string{}
	objToShard := make(map[string]int)

	for i := 0; i < 100000 && len(objToShard) < 3; i++ {
		testID := "TestObject-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
		shardID := sharding.GetShardID(testID)
		if shardID == 0 || shardID == 1 || shardID == 2 {
			if _, exists := objToShard[testID]; !exists {
				objectIDs = append(objectIDs, testID)
				objToShard[testID] = shardID
			}
		}
	}

	// Get objects to evict from node1
	toEvict := cm.GetObjectsToEvict(addr1, objectIDs)

	// Expected: Objects from shard 0 and 2 should be evicted (TargetNode != node1)
	// Object from shard 1 should NOT be evicted (both CurrentNode and TargetNode are node1)
	expectedEvictions := 0
	for _, objID := range objectIDs {
		if shardID, exists := objToShard[objID]; exists {
			if shardID == 0 || shardID == 2 {
				expectedEvictions++
			}
		}
	}

	if len(toEvict) != expectedEvictions {
		t.Fatalf("Expected %d objects to evict (TargetNode mismatch), got %d (found objects: %v)", expectedEvictions, len(toEvict), objToShard)
	}

	// Verify that evicted objects are from shards 0 and 2, not shard 1
	for _, objID := range toEvict {
		shardID := sharding.GetShardID(objID)
		if shardID != 0 && shardID != 2 {
			t.Fatalf("Object %s with shard %d should not be evicted", objID, shardID)
		}
	}
}
