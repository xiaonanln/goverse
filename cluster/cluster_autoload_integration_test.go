package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestAutoLoadObject is a simple test object for auto-load testing
type TestAutoLoadObject struct {
	object.BaseObject
}

func (o *TestAutoLoadObject) OnCreated() {}

// Ensure TestAutoLoadObject implements Object interface
var _ object.Object = (*TestAutoLoadObject)(nil)

// TestClusterAutoLoadObjects_SingleNode verifies that auto-load objects are created
// on a single node when it owns the shard
func TestClusterAutoLoadObjects_SingleNode(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get object IDs that map to specific shards for deterministic testing
	obj1ID := testutil.GetObjectIDForShard(5, "AutoLoadObj1")
	obj2ID := testutil.GetObjectIDForShard(10, "AutoLoadObj2")

	// Create a node
	nodeAddr := "localhost:47500"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with auto-load objects
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: obj1ID},
			{Type: "TestAutoLoadObject", ID: obj2ID},
		},
	}

	// Create cluster with etcd and auto-load config
	c, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready and auto-load objects
	testutil.WaitForClusterReady(t, c)

	// Give some time for auto-load to complete
	time.Sleep(2 * time.Second)

	// Verify objects were created
	objectIDs := n.ListObjectIDs()
	t.Logf("Objects on node: %v", objectIDs)

	// Check that both auto-loaded objects exist
	foundObj1 := false
	foundObj2 := false
	for _, id := range objectIDs {
		if id == obj1ID {
			foundObj1 = true
		}
		if id == obj2ID {
			foundObj2 = true
		}
	}

	if !foundObj1 {
		t.Errorf("Auto-load object %s was not created", obj1ID)
	}
	if !foundObj2 {
		t.Errorf("Auto-load object %s was not created", obj2ID)
	}
}

// TestClusterAutoLoadObjects_MultiNode verifies that auto-load objects are only
// created on nodes that own the relevant shards
func TestClusterAutoLoadObjects_MultiNode(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get object IDs that map to specific shards
	obj1ID := testutil.GetObjectIDForShard(5, "AutoLoadObj3")
	obj2ID := testutil.GetObjectIDForShard(10, "AutoLoadObj4")

	// Create two nodes
	node1Addr := "localhost:47510"
	node2Addr := "localhost:47511"

	n1 := node.NewNode(node1Addr, testutil.TestNumShards)
	n2 := node.NewNode(node2Addr, testutil.TestNumShards)

	// Register test object type on both nodes
	n1.RegisterObjectType((*TestAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start nodes
	err := n1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer n1.Stop(ctx)

	err = n2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer n2.Stop(ctx)

	// Create cluster configs with same auto-load objects
	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: obj1ID},
			{Type: "TestAutoLoadObject", ID: obj2ID},
		},
	}

	cfg2 := cfg1 // Same config for both nodes

	// Create clusters
	c1, err := NewClusterWithNode(cfg1, n1)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer c1.Stop(ctx)

	c2, err := NewClusterWithNode(cfg2, n2)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer c2.Stop(ctx)

	// Start clusters
	err = c1.Start(ctx, n1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = c2.Start(ctx, n2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, c1, c2)

	// Give time for auto-load to complete
	time.Sleep(3 * time.Second)

	// Check which node owns which shard
	shard1 := 5
	shard2 := 10

	node1ForShard1, err := c1.GetNodeForShard(ctx, shard1)
	if err != nil {
		t.Fatalf("Failed to get node for shard %d: %v", shard1, err)
	}

	node1ForShard2, err := c1.GetNodeForShard(ctx, shard2)
	if err != nil {
		t.Fatalf("Failed to get node for shard %d: %v", shard2, err)
	}

	t.Logf("Shard %d owned by: %s", shard1, node1ForShard1)
	t.Logf("Shard %d owned by: %s", shard2, node1ForShard2)

	// Get objects on both nodes
	objects1 := n1.ListObjectIDs()
	objects2 := n2.ListObjectIDs()

	t.Logf("Node1 objects: %v", objects1)
	t.Logf("Node2 objects: %v", objects2)

	// Verify obj1 is on the node that owns shard 5
	var obj1Node *node.Node
	if node1ForShard1 == node1Addr {
		obj1Node = n1
	} else {
		obj1Node = n2
	}

	obj1Objects := obj1Node.ListObjectIDs()
	foundObj1 := false
	for _, id := range obj1Objects {
		if id == obj1ID {
			foundObj1 = true
			break
		}
	}
	if !foundObj1 {
		t.Errorf("Auto-load object %s was not created on the node that owns its shard", obj1ID)
	}

	// Verify obj2 is on the node that owns shard 10
	var obj2Node *node.Node
	if node1ForShard2 == node1Addr {
		obj2Node = n1
	} else {
		obj2Node = n2
	}

	obj2Objects := obj2Node.ListObjectIDs()
	foundObj2 := false
	for _, id := range obj2Objects {
		if id == obj2ID {
			foundObj2 = true
			break
		}
	}
	if !foundObj2 {
		t.Errorf("Auto-load object %s was not created on the node that owns its shard", obj2ID)
	}
}

// TestClusterAutoLoadObjects_EmptyConfig verifies that empty auto-load config works
func TestClusterAutoLoadObjects_EmptyConfig(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47520"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with empty auto-load objects
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects:               []config.AutoLoadObjectConfig{}, // Empty
	}

	// Create cluster with etcd
	c, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster - should not fail with empty auto-load config
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Verify cluster started successfully
	if !c.IsReady() {
		t.Error("Cluster should be ready")
	}
}

// TestClusterAutoLoadObjects_InvalidType verifies graceful handling of invalid object types
func TestClusterAutoLoadObjects_InvalidType(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get an object ID for a shard this node will own
	objID := testutil.GetObjectIDForShard(5, "InvalidTypeObj")

	// Create a node
	nodeAddr := "localhost:47530"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with invalid object type
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "NonExistentObjectType", ID: objID}, // Invalid type
		},
	}

	// Create cluster with etcd
	c, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster - should not crash even with invalid type
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, c)

	// Give time for auto-load attempt
	time.Sleep(2 * time.Second)

	// Verify cluster is still operational despite invalid object type
	if !c.IsReady() {
		t.Error("Cluster should still be ready despite invalid auto-load object type")
	}

	// Verify the invalid object was not created
	objectIDs := n.ListObjectIDs()
	for _, id := range objectIDs {
		if id == objID {
			t.Errorf("Invalid object type should not have been created: %s", objID)
		}
	}
}

// TestClusterAutoLoadObjects_PerShard verifies that per-shard auto-load creates
// one object per shard owned by the node
func TestClusterAutoLoadObjects_PerShard(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47540"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with per-shard auto-load object
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "PerShardTest", PerShard: true},
		},
	}

	// Create cluster with etcd and auto-load config
	c, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready and auto-load objects
	testutil.WaitForClusterReady(t, c)

	// Give some time for auto-load to complete
	time.Sleep(2 * time.Second)

	// Verify objects were created - should be one per shard (64 in tests)
	objectIDs := n.ListObjectIDs()
	t.Logf("Objects on node: %d objects", len(objectIDs))

	// Count objects matching the per-shard pattern
	perShardCount := 0
	for _, id := range objectIDs {
		// Check if ID matches the pattern shard#<N>/PerShardTest
		if len(id) > 6 && id[:6] == "shard#" {
			// Find the slash
			slashIdx := -1
			for i := 6; i < len(id); i++ {
				if id[i] == '/' {
					slashIdx = i
					break
				}
			}
			if slashIdx > 6 && id[slashIdx+1:] == "PerShardTest" {
				perShardCount++
			}
		}
	}

	// All shards should have objects since this is the only node
	expectedCount := testutil.TestNumShards
	if perShardCount != expectedCount {
		t.Errorf("Expected %d per-shard objects, got %d", expectedCount, perShardCount)
	}

	// Verify a few specific shard IDs
	expectedShardIDs := []int{0, 5, 10, testutil.TestNumShards - 1}
	for _, shardID := range expectedShardIDs {
		expectedID := fmt.Sprintf("shard#%d/PerShardTest", shardID)
		found := false
		for _, id := range objectIDs {
			if id == expectedID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find object with ID %s, but it was not created", expectedID)
		}
	}
}

// TestClusterAutoLoadObjects_PerShardMultiNode verifies that per-shard auto-load
// creates objects only on nodes that own each shard
func TestClusterAutoLoadObjects_PerShardMultiNode(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1Addr := "localhost:47550"
	node2Addr := "localhost:47551"

	n1 := node.NewNode(node1Addr, testutil.TestNumShards)
	n2 := node.NewNode(node2Addr, testutil.TestNumShards)

	// Register test object type on both nodes
	n1.RegisterObjectType((*TestAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start nodes
	err := n1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer n1.Stop(ctx)

	err = n2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer n2.Stop(ctx)

	// Create cluster configs with per-shard auto-load object
	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "MultiNodePerShard", PerShard: true},
		},
	}

	cfg2 := cfg1 // Same config for both nodes

	// Create clusters
	c1, err := NewClusterWithNode(cfg1, n1)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer c1.Stop(ctx)

	c2, err := NewClusterWithNode(cfg2, n2)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer c2.Stop(ctx)

	// Start clusters
	err = c1.Start(ctx, n1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = c2.Start(ctx, n2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, c1, c2)

	// Give time for auto-load to complete
	time.Sleep(3 * time.Second)

	// Get objects on both nodes
	objects1 := n1.ListObjectIDs()
	objects2 := n2.ListObjectIDs()

	t.Logf("Node1 objects: %d", len(objects1))
	t.Logf("Node2 objects: %d", len(objects2))

	// Count per-shard objects on each node
	perShardCount1 := 0
	perShardCount2 := 0

	for _, id := range objects1 {
		if len(id) > 6 && id[:6] == "shard#" {
			slashIdx := -1
			for i := 6; i < len(id); i++ {
				if id[i] == '/' {
					slashIdx = i
					break
				}
			}
			if slashIdx > 6 && id[slashIdx+1:] == "MultiNodePerShard" {
				perShardCount1++
			}
		}
	}

	for _, id := range objects2 {
		if len(id) > 6 && id[:6] == "shard#" {
			slashIdx := -1
			for i := 6; i < len(id); i++ {
				if id[i] == '/' {
					slashIdx = i
					break
				}
			}
			if slashIdx > 6 && id[slashIdx+1:] == "MultiNodePerShard" {
				perShardCount2++
			}
		}
	}

	// Total objects should equal total shards
	totalPerShardObjects := perShardCount1 + perShardCount2
	if totalPerShardObjects != testutil.TestNumShards {
		t.Errorf("Expected total of %d per-shard objects across both nodes, got %d (node1: %d, node2: %d)",
			testutil.TestNumShards, totalPerShardObjects, perShardCount1, perShardCount2)
	}

	// Each node should have some objects (roughly half)
	if perShardCount1 == 0 {
		t.Error("Node1 should have some per-shard objects")
	}
	if perShardCount2 == 0 {
		t.Error("Node2 should have some per-shard objects")
	}

	t.Logf("Node1 has %d per-shard objects, Node2 has %d per-shard objects", perShardCount1, perShardCount2)
}

// TestClusterAutoLoadObjects_PerNode verifies that per-node auto-load creates
// one object per node using the fixed-node ID format
func TestClusterAutoLoadObjects_PerNode(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:47600"
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register test object type
	n.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create cluster config with per-node auto-load object
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "PerNodeTest", PerNode: true},
		},
	}

	// Create cluster with etcd and auto-load config
	c, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Fatalf("Failed to create cluster: %v", err)
	}
	defer c.Stop(ctx)

	// Start the cluster
	err = c.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Wait for cluster to be ready and auto-load objects
	testutil.WaitForClusterReady(t, c)

	// Give some time for auto-load to complete
	time.Sleep(2 * time.Second)

	// Verify object was created with correct ID format: <nodeAddr>/<baseName>
	expectedObjectID := fmt.Sprintf("%s/PerNodeTest", nodeAddr)
	objectIDs := n.ListObjectIDs()
	t.Logf("Objects on node: %v", objectIDs)

	found := false
	for _, id := range objectIDs {
		if id == expectedObjectID {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected per-node object %s was not created. Found objects: %v", expectedObjectID, objectIDs)
	}
}

// TestClusterAutoLoadObjects_PerNodeMultiNode verifies that per-node auto-load
// creates one object per node when multiple nodes are present
func TestClusterAutoLoadObjects_PerNodeMultiNode(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1Addr := "localhost:47610"
	node2Addr := "localhost:47611"

	n1 := node.NewNode(node1Addr, testutil.TestNumShards)
	n2 := node.NewNode(node2Addr, testutil.TestNumShards)

	// Register test object type on both nodes
	n1.RegisterObjectType((*TestAutoLoadObject)(nil))
	n2.RegisterObjectType((*TestAutoLoadObject)(nil))

	// Start nodes
	err := n1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer n1.Stop(ctx)

	err = n2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer n2.Stop(ctx)

	// Create cluster configs with per-node auto-load object
	cfg1 := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 2 * time.Second,
		ShardMappingCheckInterval:     500 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
		AutoLoadObjects: []config.AutoLoadObjectConfig{
			{Type: "TestAutoLoadObject", ID: "MultiNodePerNode", PerNode: true},
		},
	}

	cfg2 := cfg1 // Same config for both nodes

	// Create clusters
	c1, err := NewClusterWithNode(cfg1, n1)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer c1.Stop(ctx)

	c2, err := NewClusterWithNode(cfg2, n2)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer c2.Stop(ctx)

	// Start clusters
	err = c1.Start(ctx, n1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = c2.Start(ctx, n2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, c1, c2)

	// Give time for auto-load to complete
	time.Sleep(3 * time.Second)

	// Verify each node created its own per-node object
	expectedObjectID1 := fmt.Sprintf("%s/MultiNodePerNode", node1Addr)
	expectedObjectID2 := fmt.Sprintf("%s/MultiNodePerNode", node2Addr)

	objects1 := n1.ListObjectIDs()
	objects2 := n2.ListObjectIDs()

	t.Logf("Node1 objects: %v", objects1)
	t.Logf("Node2 objects: %v", objects2)

	found1 := false
	for _, id := range objects1 {
		if id == expectedObjectID1 {
			found1 = true
			break
		}
	}

	found2 := false
	for _, id := range objects2 {
		if id == expectedObjectID2 {
			found2 = true
			break
		}
	}

	if !found1 {
		t.Errorf("Expected per-node object %s was not created on node1. Found objects: %v", expectedObjectID1, objects1)
	}
	if !found2 {
		t.Errorf("Expected per-node object %s was not created on node2. Found objects: %v", expectedObjectID2, objects2)
	}

	// Verify objects are NOT created on the wrong nodes
	for _, id := range objects1 {
		if id == expectedObjectID2 {
			t.Errorf("Node1 should not have node2's per-node object %s", expectedObjectID2)
		}
	}
	for _, id := range objects2 {
		if id == expectedObjectID1 {
			t.Errorf("Node2 should not have node1's per-node object %s", expectedObjectID1)
		}
	}
}
