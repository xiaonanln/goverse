package node

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

func TestNode_OnShardMappingChanged(t *testing.T) {
	t.Parallel()

	// Create a test node
	node := NewNode("localhost:47001")

	// Register a simple test object type
	node.RegisterObjectType((*TestObject)(nil))

	// Create some test objects
	ctx := context.Background()
	obj1ID := "TestObject-obj1"
	obj2ID := "TestObject-obj2"
	obj3ID := "TestObject-obj3"

	_, err := node.CreateObject(ctx, "TestObject", obj1ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = node.CreateObject(ctx, "TestObject", obj2ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	_, err = node.CreateObject(ctx, "TestObject", obj3ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 3: %v", err)
	}

	// Verify we have 3 objects
	if node.NumObjects() != 3 {
		t.Errorf("Expected 3 objects, got %d", node.NumObjects())
	}

	// Create a test shard mapping
	// Let's say obj1 and obj2 stay on this node, but obj3 should move to another node
	mapping := &sharding.ShardMapping{
		Shards:  make(map[int]*sharding.ShardInfo),
		Nodes:   []string{"localhost:47001", "localhost:47002"},
		Version: 1,
	}

	// Calculate shard IDs
	shard1 := sharding.GetShardID(obj1ID)
	shard2 := sharding.GetShardID(obj2ID)
	shard3 := sharding.GetShardID(obj3ID)

	// Assign shards
	mapping.Shards[shard1] = &sharding.ShardInfo{
		ShardID:     shard1,
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateAvailable,
	}
	mapping.Shards[shard2] = &sharding.ShardInfo{
		ShardID:     shard2,
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateAvailable,
	}
	mapping.Shards[shard3] = &sharding.ShardInfo{
		ShardID:     shard3,
		TargetNode:  "localhost:47002",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateMigrating,
	}

	// Call OnShardMappingChanged - this should just log, not actually migrate
	node.OnShardMappingChanged(ctx, mapping)

	// Verify all objects are still on this node (no migration happened)
	if node.NumObjects() != 3 {
		t.Errorf("Expected all 3 objects to still be on node (no migration yet), got %d", node.NumObjects())
	}
}

func TestNode_OnShardMappingChanged_AllObjectsStay(t *testing.T) {
	t.Parallel()

	// Create a test node
	node := NewNode("localhost:47001")

	// Register a simple test object type
	node.RegisterObjectType((*TestObject)(nil))

	// Create some test objects
	ctx := context.Background()
	obj1ID := "TestObject-obj1"
	obj2ID := "TestObject-obj2"

	_, err := node.CreateObject(ctx, "TestObject", obj1ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = node.CreateObject(ctx, "TestObject", obj2ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	// Create a test shard mapping where all objects stay
	mapping := &sharding.ShardMapping{
		Shards:  make(map[int]*sharding.ShardInfo),
		Nodes:   []string{"localhost:47001"},
		Version: 1,
	}

	// Assign all shards to this node
	for i := 0; i < sharding.NumShards; i++ {
		mapping.Shards[i] = &sharding.ShardInfo{
			ShardID:     i,
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47001",
			State:       sharding.ShardStateAvailable,
		}
	}

	// Call OnShardMappingChanged
	node.OnShardMappingChanged(ctx, mapping)

	// Verify all objects are still on this node
	if node.NumObjects() != 2 {
		t.Errorf("Expected all 2 objects to still be on node, got %d", node.NumObjects())
	}
}

func TestNode_OnShardMappingChanged_EmptyNode(t *testing.T) {
	t.Parallel()

	// Create a test node with no objects
	node := NewNode("localhost:47001")

	// Create a test shard mapping
	mapping := &sharding.ShardMapping{
		Shards:  make(map[int]*sharding.ShardInfo),
		Nodes:   []string{"localhost:47001", "localhost:47002"},
		Version: 1,
	}

	// Assign shards
	for i := 0; i < sharding.NumShards; i++ {
		var targetNode string
		if i%2 == 0 {
			targetNode = "localhost:47001"
		} else {
			targetNode = "localhost:47002"
		}
		mapping.Shards[i] = &sharding.ShardInfo{
			ShardID:     i,
			TargetNode:  targetNode,
			CurrentNode: targetNode,
			State:       sharding.ShardStateAvailable,
		}
	}

	ctx := context.Background()

	// Call OnShardMappingChanged on empty node - should not panic
	node.OnShardMappingChanged(ctx, mapping)

	// Verify node is still empty
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects, got %d", node.NumObjects())
	}
}
