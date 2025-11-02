package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestShardTestObject for testing automatic shard mapping change notifications
type TestShardTestObject struct {
	object.BaseObject
}

func (o *TestShardTestObject) OnCreated() {}

// TestAutomaticShardMappingNotification tests that nodes are automatically notified
// when shard mapping changes through the cluster management loop
func TestAutomaticShardMappingNotification(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a test node
	testNode := node.NewNode("localhost:47001")
	testNode.RegisterObjectType((*TestShardTestObject)(nil))

	// Create some test objects
	obj1ID := "TestShardTestObject-obj1"
	obj2ID := "TestShardTestObject-obj2"

	_, err := testNode.CreateObject(ctx, "TestShardTestObject", obj1ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = testNode.CreateObject(ctx, "TestShardTestObject", obj2ID, nil)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	// Set up cluster
	cluster := newClusterForTesting("TestCluster")
	cluster.SetThisNode(testNode)

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	cluster.SetEtcdManager(mgr)

	// Connect to etcd
	err = cluster.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	// Register node
	err = cluster.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}
	defer cluster.UnregisterNode(ctx)

	// Start watching nodes
	err = cluster.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes: %v", err)
	}

	// Wait for node to be registered
	time.Sleep(500 * time.Millisecond)

	// Track if OnShardMappingChanged was called by checking log output
	// We'll use a channel to signal when cluster becomes ready
	readyChan := cluster.ClusterReady()

	// Start shard mapping management - this should trigger automatic notification
	err = cluster.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management: %v", err)
	}
	defer cluster.StopShardMappingManagement()

	// Wait for cluster to become ready (with timeout)
	select {
	case <-readyChan:
		t.Log("Cluster became ready - OnShardMappingChanged should have been called automatically")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for cluster to become ready")
	}

	// Verify cluster is ready
	if !cluster.IsReady() {
		t.Error("Cluster should be ready")
	}

	// Verify shard mapping was created
	mapping, err := cluster.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping: %v", err)
	}

	// Note: Version is now tracked in ClusterState
	if mapping == nil {
		t.Error("Expected shard mapping to exist")
	}

	// Verify objects are still on the node
	if testNode.NumObjects() != 2 {
		t.Errorf("Expected 2 objects on node, got %d", testNode.NumObjects())
	}

	t.Log("Test completed - automatic shard mapping notification works correctly")
}

// TestShardMappingChangeAfterClusterReady tests that shard mapping changes
// after cluster is ready also trigger automatic notifications
func TestShardMappingChangeAfterClusterReady(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create two nodes
	node1 := node.NewNode("localhost:47101")
	node2 := node.NewNode("localhost:47102")

	node1.RegisterObjectType((*TestShardTestObject)(nil))
	node2.RegisterObjectType((*TestShardTestObject)(nil))

	// Create objects on node1
	_, err := node1.CreateObject(ctx, "TestShardTestObject", "TestShardTestObject-test1", nil)
	if err != nil {
		t.Fatalf("Failed to create object on node1: %v", err)
	}

	// Set up cluster for node1 (will be leader)
	cluster1 := newClusterForTesting("TestCluster1")
	cluster1.SetThisNode(node1)

	mgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager for node1: %v", err)
	}
	cluster1.SetEtcdManager(mgr1)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes: %v", err)
	}

	// Wait for node to be registered
	time.Sleep(500 * time.Millisecond)

	// Start shard mapping management on node1
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management on node1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	// Wait for cluster1 to become ready
	select {
	case <-cluster1.ClusterReady():
		t.Log("Cluster1 became ready")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for cluster1 to become ready")
	}

	// Now add node2 to trigger a shard mapping change
	cluster2 := newClusterForTesting("TestCluster2")
	cluster2.SetThisNode(node2)

	mgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager for node2: %v", err)
	}
	cluster2.SetEtcdManager(mgr2)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes on node2: %v", err)
	}

	err = cluster2.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management on node2: %v", err)
	}
	defer cluster2.StopShardMappingManagement()

	// Wait for cluster2 to become ready
	select {
	case <-cluster2.ClusterReady():
		t.Log("Cluster2 became ready")
	case <-time.After(15 * time.Second):
		t.Fatal("Timeout waiting for cluster2 to become ready")
	}

	// Wait for shard mapping to stabilize and update
	// The leader should detect the new node and update the mapping
	time.Sleep(NodeStabilityDuration + ShardMappingCheckInterval + 2*time.Second)

	// Verify both nodes have the updated mapping
	mapping1, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping from cluster1: %v", err)
	}

	mapping2, err := cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping from cluster2: %v", err)
	}

	// Note: Version is now tracked in ClusterState, not ShardMapping
	// Both should have the same mapping (pointer comparison not valid across clusters)
	if len(mapping1.Shards) != len(mapping2.Shards) {
		t.Errorf("Shard mapping sizes differ: cluster1=%d, cluster2=%d", len(mapping1.Shards), len(mapping2.Shards))
	}

	// The mapping should include both nodes (verify by checking shard assignments)
	nodeSet := make(map[string]bool)
	for _, node := range mapping1.Shards {
		nodeSet[node] = true
	}
	if len(nodeSet) != 2 {
		t.Errorf("Expected 2 nodes in mapping, got %d unique nodes", len(nodeSet))
	}

	t.Log("Test completed - shard mapping change after cluster ready triggers automatic notifications")
}
