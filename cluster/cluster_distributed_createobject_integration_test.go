package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestDistributedCreateObject tests shard mapping and routing logic
// by verifying that objects are correctly assigned to nodes based on shards
// This test requires a running etcd instance at localhost:2379
func TestDistributedCreateObject(t *testing.T) {
	t.Parallel()
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster 1
	cluster1 := newClusterForTesting("TestCluster1")
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47001")
	cluster1.SetThisNode(node1)
	node1.RegisterObjectType((*TestDistributedObject)(nil))

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create cluster 2
	cluster2 := newClusterForTesting("TestCluster2")
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47002")
	cluster2.SetThisNode(node2)
	node2.RegisterObjectType((*TestDistributedObject)(nil))

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC servers for both nodes
	mockServer1 := NewMockGoverseServer()
	mockServer1.SetNode(node1) // Assign the actual node to the mock server
	testServer1 := NewTestServerHelper("localhost:47001", mockServer1)
	err = testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	defer testServer1.Stop()

	mockServer2 := NewMockGoverseServer()
	mockServer2.SetNode(node2) // Assign the actual node to the mock server
	testServer2 := NewTestServerHelper("localhost:47002", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	defer testServer2.Stop()

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Start NodeConnections for both clusters (needed for routing)
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster1: %v", err)
	}
	defer cluster1.StopNodeConnections()

	err = cluster2.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster2: %v", err)
	}
	defer cluster2.StopNodeConnections()

	// Wait for connections to be established
	time.Sleep(500 * time.Millisecond)

	// Start shard mapping management (we don't need mock servers for this test)
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	err = cluster2.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster2: %v", err)
	}
	defer cluster2.StopShardMappingManagement()

	// Wait for shard mapping to be initialized (longer wait needed)
	time.Sleep(15 * time.Second)

	// Verify shard mapping is ready
	_, err = cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Shard mapping not initialized: %v", err)
	}

	objExistsOnNode := func(objID string, n *node.Node) bool {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				return true
			}
		}
		return false
	}

	// Test CreateObject from cluster1
	t.Run("CreateObject from cluster1", func(t *testing.T) {
		// Create 10 objects and verify each is created on the correct target node
		for i := 1; i <= 10; i++ {
			objID := fmt.Sprintf("test-obj-%d", i)

			// Get the target node for this object
			var creatorCluster *Cluster
			if i%2 == 0 {
				creatorCluster = cluster2
			} else {
				creatorCluster = cluster1
			}
			targetNode, err := creatorCluster.GetNodeForObject(ctx, objID)
			if err != nil {
				t.Fatalf("GetNodeForObject failed for %s: %v", objID, err)
			}
			t.Logf("Creating object %s from %s, expect target node %s", objID, creatorCluster.thisNode.GetAdvertiseAddress(), targetNode)

			// Create the object - it will be routed if needed
			createdID, err := creatorCluster.CreateObject(ctx, "TestDistributedObject", objID, nil)
			if err != nil {
				t.Fatalf("CreateObject failed for %s: %v", objID, err)
			}

			if createdID != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, createdID)
			}

			// Verify the object was created on the correct node
			var objectNode *node.Node
			switch targetNode {
			case "localhost:47001":
				objectNode = node1
			case "localhost:47002":
				objectNode = node2
			default:
				t.Fatalf("Unknown target node: %s", targetNode)
			}

			// Check if object exists on the target node
			objExists := objExistsOnNode(objID, objectNode)
			if !objExists {
				t.Fatalf("Object %s should exist on target node %s, but doesn't", objID, targetNode)
			}

			t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
		}
	})
}

// TestDistributedCreateObject_EvenDistribution tests that shard mapping
// correctly distributes shards across 3 nodes for even load distribution
// This test requires a running etcd instance at localhost:2379
func TestDistributedCreateObject_EvenDistribution(t *testing.T) {
	t.Parallel()

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create 3 clusters
	clusters := make([]*Cluster, 3)
	nodes := make([]*node.Node, 3)
	nodeAddrs := []string{"localhost:47011", "localhost:47012", "localhost:47013"}

	// Set up all 3 nodes
	for i := 0; i < 3; i++ {
		clusters[i] = newClusterForTesting("TestCluster" + fmt.Sprintf("%d", i+1))
		etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
		if err != nil {
			t.Fatalf("Failed to create etcd manager %d: %v", i+1, err)
		}
		clusters[i].SetEtcdManager(etcdMgr)

		nodes[i] = node.NewNode(nodeAddrs[i])
		clusters[i].SetThisNode(nodes[i])
		nodes[i].RegisterObjectType((*TestDistributedObject)(nil))

		err = nodes[i].Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node%d: %v", i+1, err)
		}
		defer nodes[i].Stop(ctx)

		err = clusters[i].ConnectEtcd()
		if err != nil {
			t.Fatalf("Failed to connect etcd for cluster%d: %v", i+1, err)
		}
		defer clusters[i].CloseEtcd()

		err = clusters[i].RegisterNode(ctx)
		if err != nil {
			t.Fatalf("Failed to register node%d: %v", i+1, err)
		}
		defer clusters[i].UnregisterNode(ctx)

		err = clusters[i].WatchNodes(ctx)
		if err != nil {
			t.Fatalf("Failed to start watching nodes for cluster%d: %v", i+1, err)
		}
	}

	// Wait for nodes to discover each other
	time.Sleep(2 * time.Second)

	// Start shard mapping management for all clusters
	for i := 0; i < 3; i++ {
		err := clusters[i].StartShardMappingManagement(ctx)
		if err != nil {
			t.Fatalf("Failed to start shard mapping management for cluster%d: %v", i+1, err)
		}
		defer clusters[i].StopShardMappingManagement()
	}

	// Wait for shard mapping to be initialized
	time.Sleep(12 * time.Second)

	// Test shard distribution by getting target nodes for 100 object IDs
	numObjects := 100
	createdObjectIDs := make([]string, numObjects)
	for i := 0; i < numObjects; i++ {
		objID := "test-obj-" + fmt.Sprintf("%02d", i)
		createdObjectIDs[i] = objID
	}

	// Wait for all objects to be analyzed
	time.Sleep(1 * time.Second)

	// Verify shard mapping is consistent across all clusters
	t.Logf("Verifying shard mapping consistency across %d clusters", len(clusters))

	// Get shard mapping from first cluster
	shardMapping1, err := clusters[0].GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping from cluster 0: %v", err)
	}

	// Verify all clusters have the same shard mapping
	for i := 1; i < len(clusters); i++ {
		shardMapping, err := clusters[i].GetShardMapping(ctx)
		if err != nil {
			t.Fatalf("Failed to get shard mapping from cluster %d: %v", i, err)
		}
		if shardMapping.Version != shardMapping1.Version {
			t.Errorf("Cluster %d has different shard mapping version (%d) than cluster 0 (%d)",
				i, shardMapping.Version, shardMapping1.Version)
		}
	}

	// Verify that objects would be distributed across shards
	// (We test the routing logic without actually creating on remote nodes)
	shardCounts := make(map[string]int)
	for _, objID := range createdObjectIDs {
		targetNode, err := clusters[0].GetNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetNodeForObject failed: %v", err)
		}
		shardCounts[targetNode]++
	}

	t.Logf("Object shard distribution: %v", shardCounts)

	// Verify total objects accounted for
	totalSharded := 0
	for _, count := range shardCounts {
		totalSharded += count
	}
	if totalSharded != numObjects {
		t.Errorf("Expected %d objects to be sharded, got %d", numObjects, totalSharded)
	}

	// Verify objects are distributed across all 3 nodes
	if len(shardCounts) != 3 {
		t.Errorf("Expected objects to be distributed across all 3 nodes, got %d nodes", len(shardCounts))
	}

	// Verify each node has roughly equal distribution
	// With 100 objects and 3 nodes, expect roughly 33 per node, allow 15-50 per node
	for nodeAddr, count := range shardCounts {
		if count < 15 || count > 50 {
			t.Logf("Warning: Node %s has %d objects (expected ~33 for even distribution)", nodeAddr, count)
		} else {
			t.Logf("Node %s has %d objects (good distribution)", nodeAddr, count)
		}
	}
}

// TestDistributedObject is a simple object for testing distributed creation
type TestDistributedObject struct {
	object.BaseObject
}

func (o *TestDistributedObject) OnCreated() {}
