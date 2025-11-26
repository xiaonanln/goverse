package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// waitForObjectCreated waits for an object to be created on the specified node
func waitForObjectCreated(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not created on node within %v", objID, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDistributedCreateObject tests shard mapping and routing logic
// by verifying that objects are correctly assigned to nodes based on shards
// This test requires a running etcd instance at localhost:2379
func TestDistributedCreateObject(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get free addresses for the nodes
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()

	// Create clusters using mustNewCluster
	cluster1 := mustNewCluster(ctx, t, addr1, testPrefix)
	cluster2 := mustNewCluster(ctx, t, addr2, testPrefix)

	// Register object types on the nodes (can be done after node start)
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.RegisterObjectType((*TestDistributedObject)(nil))
	node2.RegisterObjectType((*TestDistributedObject)(nil))

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC servers for both nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1) // Assign the actual node to the mock server
	testServer1 := testutil.NewTestServerHelper(addr1, mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2) // Assign the actual node to the mock server
	testServer2 := testutil.NewTestServerHelper(addr2, mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Wait for shard mapping to be initialized (cluster.Start already handles StartNodeConnections and StartShardMappingManagement)
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Verify shard mapping is ready
	_ = cluster1.GetShardMapping(ctx)

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
			targetNode, err := creatorCluster.GetCurrentNodeForObject(ctx, objID)
			if err != nil {
				t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
			}
			t.Logf("Creating object %s from %s, expect target node %s", objID, creatorCluster.GetThisNode().GetAdvertiseAddress(), targetNode)

			// Create the object - it will be routed if needed
			createdID, err := creatorCluster.CreateObject(ctx, "TestDistributedObject", objID)
			if err != nil {
				t.Fatalf("CreateObject failed for %s: %v", objID, err)
			}

			if createdID != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, createdID)
			}

			// Verify the object was created on the correct node
			var objectNode *node.Node
			switch targetNode {
			case addr1:
				objectNode = node1
			case addr2:
				objectNode = node2
			default:
				t.Fatalf("Unknown target node: %s", targetNode)
			}

			// Wait for async object creation to complete
			waitForObjectCreated(t, objectNode, objID, 5*time.Second)

			t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
		}
	})

	// Test that duplicate CreateObject with same ID and type returns success (idempotent)
	t.Run("Duplicate CreateObject with same type is idempotent", func(t *testing.T) {
		objID := "test-duplicate-obj"

		// Create the object first time
		createdID, err := cluster1.CreateObject(ctx, "TestDistributedObject", objID)
		if err != nil {
			t.Fatalf("First CreateObject failed: %v", err)
		}
		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		// Get the target node to verify object count later
		targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed: %v", err)
		}

		var objectNode *node.Node
		switch targetNode {
		case addr1:
			objectNode = node1
		case addr2:
			objectNode = node2
		default:
			t.Fatalf("Unknown target node: %s", targetNode)
		}

		// Wait for async object creation to complete before counting
		waitForObjectCreated(t, objectNode, objID, 5*time.Second)

		// Count objects on the node before duplicate attempt
		objCountBefore := objectNode.NumObjects()

		// Attempt to create the same object again with same type - should succeed (idempotent)
		createdID2, err := cluster1.CreateObject(ctx, "TestDistributedObject", objID)
		if err != nil {
			t.Fatalf("Expected success when creating duplicate object with same type, but got error: %v", err)
		}
		if createdID2 != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID2)
		}

		// Wait for async operation to complete (CreateObject is async)
		time.Sleep(1 * time.Second)

		// Verify object count hasn't increased
		objCountAfter := objectNode.NumObjects()
		if objCountAfter != objCountBefore {
			t.Fatalf("Object count changed from %d to %d after duplicate create attempt", objCountBefore, objCountAfter)
		}

		// Verify only one instance of the object exists
		objectCount := 0
		for _, obj := range objectNode.ListObjects() {
			if obj.Id == objID {
				objectCount++
			}
		}
		if objectCount != 1 {
			t.Fatalf("Expected exactly 1 instance of object %s, found %d", objID, objectCount)
		}

		t.Logf("Successfully verified duplicate CreateObject with same type is idempotent")
	})
}

// TestDistributedCreateObject_EvenDistribution tests that shard mapping
// correctly distributes shards across 3 nodes for even load distribution
// This test requires a running etcd instance at localhost:2379
// Note: This test does NOT run in parallel because it uses specific ports that may conflict with other tests
func TestDistributedCreateObject_EvenDistribution(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Get free addresses for the nodes
	addrs := make([]string, 3)
	for i := 0; i < 3; i++ {
		addrs[i] = testutil.GetFreeAddress()
	}

	// Create 3 clusters using mustNewCluster
	// Note: No object registration needed since this test only calls GetCurrentNodeForObject
	clusters := []*Cluster{
		mustNewCluster(ctx, t, addrs[0], testPrefix),
		mustNewCluster(ctx, t, addrs[1], testPrefix),
		mustNewCluster(ctx, t, addrs[2], testPrefix),
	}

	// Wait for nodes to discover each other and shard mapping to initialize
	testutil.WaitForClustersReady(t, clusters[0], clusters[1], clusters[2])

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
	shardMapping1 := clusters[0].GetShardMapping(ctx)

	// Verify all clusters have the same shard mapping
	for i := 1; i < len(clusters); i++ {
		shardMapping := clusters[i].GetShardMapping(ctx)
		// Note: Version is now tracked in ClusterState
		// Verify they have same number of shards
		if len(shardMapping.Shards) != len(shardMapping1.Shards) {
			t.Fatalf("Cluster %d has different shard count (%d) than cluster 0 (%d)",
				i, len(shardMapping.Shards), len(shardMapping1.Shards))
		}
	}

	// Verify that objects would be distributed across shards
	// (We test the routing logic without actually creating on remote nodes)
	shardCounts := make(map[string]int)
	for _, objID := range createdObjectIDs {
		targetNode, err := clusters[0].GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed: %v", err)
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
		t.Fatalf("Expected %d objects to be sharded, got %d", numObjects, totalSharded)
	}

	// Verify objects are distributed across all 3 nodes
	if len(shardCounts) != 3 {
		t.Fatalf("Expected objects to be distributed across all 3 nodes, got %d nodes", len(shardCounts))
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
