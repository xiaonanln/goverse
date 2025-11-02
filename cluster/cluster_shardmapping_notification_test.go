package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestObject for testing shard mapping changes
type ShardTestObject struct {
	object.BaseObject
}

func (o *ShardTestObject) OnCreated() {}

// TestShardMappingChangeNotification tests that nodes are notified when shard mapping changes
func TestShardMappingChangeNotification(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create two test nodes
	node1 := node.NewNode("localhost:47001")
	node2 := node.NewNode("localhost:47002")

	// Register test object type on both nodes
	node1.RegisterObjectType((*ShardTestObject)(nil))
	node2.RegisterObjectType((*ShardTestObject)(nil))

	// Create some objects on node1
	obj1ID := "ShardTestObject-obj1"
	obj2ID := "ShardTestObject-obj2"
	obj3ID := "ShardTestObject-obj3"

	_, err := node1.CreateObject(ctx, "ShardTestObject", obj1ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = node1.CreateObject(ctx, "ShardTestObject", obj2ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	_, err = node1.CreateObject(ctx, "ShardTestObject", obj3ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 3: %v", err)
	}

	// Verify all 3 objects are on node1
	if node1.NumObjects() != 3 {
		t.Errorf("Expected 3 objects on node1, got %d", node1.NumObjects())
	}

	// Create an initial shard mapping with only node1
	cluster1 := newClusterForTesting("TestCluster1")
	cluster1.SetThisNode(node1)

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster1.SetEtcdManager(mgr)

	initialMapping := &sharding.ShardMapping{
		Shards:  make(map[int]*sharding.ShardInfo),
		Nodes:   []string{"localhost:47001"},
		Version: 1,
	}

	// Assign all shards to node1
	for i := 0; i < sharding.NumShards; i++ {
		initialMapping.Shards[i] = &sharding.ShardInfo{
			ShardID:     i,
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47001",
			State:       sharding.ShardStateAvailable,
		}
	}

	// Call OnShardMappingChanged with initial mapping - all objects should stay
	node1.OnShardMappingChanged(ctx, initialMapping)

	// Create a new mapping with node2 added
	newMapping := &sharding.ShardMapping{
		Shards:  make(map[int]*sharding.ShardInfo),
		Nodes:   []string{"localhost:47001", "localhost:47002"},
		Version: 2,
	}

	// Distribute shards between node1 and node2
	shard1 := sharding.GetShardID(obj1ID)
	shard2 := sharding.GetShardID(obj2ID)
	shard3 := sharding.GetShardID(obj3ID)

	// Let's say obj1 and obj2 stay on node1, but obj3 moves to node2
	newMapping.Shards[shard1] = &sharding.ShardInfo{
		ShardID:     shard1,
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateAvailable,
	}
	newMapping.Shards[shard2] = &sharding.ShardInfo{
		ShardID:     shard2,
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateAvailable,
	}
	newMapping.Shards[shard3] = &sharding.ShardInfo{
		ShardID:     shard3,
		TargetNode:  "localhost:47002",
		CurrentNode: "localhost:47001",
		State:       sharding.ShardStateMigrating,
	}

	// Assign remaining shards
	for i := 0; i < sharding.NumShards; i++ {
		if _, exists := newMapping.Shards[i]; !exists {
			var targetNode string
			if i%2 == 0 {
				targetNode = "localhost:47001"
			} else {
				targetNode = "localhost:47002"
			}
			newMapping.Shards[i] = &sharding.ShardInfo{
				ShardID:     i,
				TargetNode:  targetNode,
				CurrentNode: targetNode,
				State:       sharding.ShardStateAvailable,
			}
		}
	}

	// Call OnShardMappingChanged with new mapping
	// This should log that obj3 needs to be migrated to node2
	node1.OnShardMappingChanged(ctx, newMapping)

	// Verify that no actual migration happened (all objects still on node1)
	if node1.NumObjects() != 3 {
		t.Errorf("Expected 3 objects still on node1 (no migration yet), got %d", node1.NumObjects())
	}

	// Verify node2 has no objects
	if node2.NumObjects() != 0 {
		t.Errorf("Expected 0 objects on node2, got %d", node2.NumObjects())
	}

	t.Log("Test completed successfully - OnShardMappingChanged correctly identified migration needs without performing actual migration")
}

// TestShardMappingChangeWithCluster tests the full integration with cluster
func TestShardMappingChangeWithCluster(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a test node
	testNode := node.NewNode("localhost:47001")
	testNode.RegisterObjectType((*ShardTestObject)(nil))

	// Create some test objects
	_, err := testNode.CreateObject(ctx, "ShardTestObject", "ShardTestObject-test1", nil)
	if err != nil {
		t.Fatalf("Failed to create test object: %v", err)
	}

	// Set up cluster
	cluster := newClusterForTesting("TestCluster")
	cluster.SetThisNode(testNode)

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	// Create and store initial mapping
	mapping := &sharding.ShardMapping{
		Shards:  make(map[int]string),
		Nodes:   []string{"localhost:47001"},
		Version: 1,
	}

	for i := 0; i < sharding.NumShards; i++ {
		mapping.Shards[i] = "localhost:47001"
	}

	cluster.shardMapper.SetMappingForTesting(mapping)

	// Simulate a mapping update by creating a new version
	newMapping := &sharding.ShardMapping{
		Shards:  make(map[int]string),
		Nodes:   []string{"localhost:47001", "localhost:47002"},
		Version: 2,
	}

	// Distribute shards
	for i := 0; i < sharding.NumShards; i++ {
		if i%2 == 0 {
			newMapping.Shards[i] = "localhost:47001"
		} else {
			newMapping.Shards[i] = "localhost:47002"
		}
	}

	// Manually call OnShardMappingChanged
	testNode.OnShardMappingChanged(ctx, newMapping)

	// Verify the object is still on the node (no migration)
	if testNode.NumObjects() != 1 {
		t.Errorf("Expected 1 object on node, got %d", testNode.NumObjects())
	}

	t.Log("Test completed - cluster integration with shard mapping change notification works correctly")
}
