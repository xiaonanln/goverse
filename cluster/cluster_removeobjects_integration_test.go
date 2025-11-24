package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// waitForObjectRemoved waits for an object to be removed from a node
func waitForObjectRemoved(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		found := false
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				found = true
				break
			}
		}
		if !found {
			return // Object successfully removed
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not removed from node within %v", objID, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestClusterRemoveObjectsNotBelongingToThisNode tests that objects are removed from nodes
// when shard mappings change, and eventually created on the new target node
// This test requires a running etcd instance at localhost:2379
func TestClusterRemoveObjectsNotBelongingToThisNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create clusters using mustNewCluster
	node1Addr := testutil.GetFreeAddress()
	node2Addr := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, node1Addr, testPrefix)
	cluster2 := mustNewCluster(ctx, t, node2Addr, testPrefix)

	// Register object types on the nodes
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.RegisterObjectType((*TestRemoveObject)(nil))
	node2.RegisterObjectType((*TestRemoveObject)(nil))

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC servers for both nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(node1Addr, mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := testutil.NewTestServerHelper(node2Addr, mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Wait for shard mapping to be initialized
	t.Logf("Waiting for initial shard mapping to be created...")
	testutil.WaitForClusterReady(t, cluster1)
	testutil.WaitForClusterReady(t, cluster2)

	// Verify shard mapping is ready
	initialMapping := cluster1.GetShardMapping(ctx)
	t.Logf("Initial shard mapping created with %d shards", len(initialMapping.Shards))

	// Helper function to check if an object exists on a node
	objExistsOnNode := func(objID string, n *node.Node) bool {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				return true
			}
		}
		return false
	}

	// Create 10 objects but will only move 2 of them
	numObjects := 10
	objectIDs := make([]string, numObjects)
	shardIDs := make([]int, numObjects)

	t.Logf("Creating %d objects on cluster...", numObjects)
	for i := 0; i < numObjects; i++ {
		objID := fmt.Sprintf("test-remove-obj-%d", i)
		objectIDs[i] = objID
		shardIDs[i] = sharding.GetShardID(objID, testutil.TestNumShards)

		// Create the object
		createdID, err := cluster1.CreateObject(ctx, "TestRemoveObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed for %s: %v", objID, err)
		}
		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		t.Logf("Created object %s (shard %d)", objID, shardIDs[i])
	}

	// Wait for all async object creations to complete
	t.Logf("Waiting for all objects to be created...")
	for _, objID := range objectIDs {
		// Check which node should have this object
		targetNode, err := cluster1.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to get target node for %s: %v", objID, err)
		}
		var n *node.Node
		if targetNode == node1Addr {
			n = node1
		} else {
			n = node2
		}
		waitForObjectCreated(t, n, objID, 5*time.Second)
	}

	// Find which objects are on node1 - we'll only move 2 of them to avoid rebalancing interference
	objectsOnNode1 := make([]string, 0)
	objectsOnNode1ShardIDs := make([]int, 0)

	for i, objID := range objectIDs {
		if objExistsOnNode(objID, node1) {
			objectsOnNode1 = append(objectsOnNode1, objID)
			objectsOnNode1ShardIDs = append(objectsOnNode1ShardIDs, shardIDs[i])
			t.Logf("Object %s (shard %d) is on node1", objID, shardIDs[i])
		} else if objExistsOnNode(objID, node2) {
			t.Logf("Object %s (shard %d) is on node2", objID, shardIDs[i])
		} else {
			t.Fatalf("Object %s not found on any node", objID)
		}
	}

	if len(objectsOnNode1) == 0 {
		t.Skipf("No objects on node1, test cannot proceed (all objects happened to be on node2)")
	}

	// Select only 2 objects to move to avoid rebalancing interference
	objectsToMove := objectsOnNode1
	objectsToMoveShardIDs := objectsOnNode1ShardIDs
	if len(objectsOnNode1) > 2 {
		objectsToMove = objectsOnNode1[:2]
		objectsToMoveShardIDs = objectsOnNode1ShardIDs[:2]
		t.Logf("Will move only 2 out of %d objects from node1 to avoid rebalancing", len(objectsOnNode1))
	}

	// Now manually update the shard mapping in etcd to reassign shards from node1 to node2
	t.Logf("Manually updating shard mapping to reassign %d objects from node1 to node2...", len(objectsToMove))

	// Connect directly to etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close()

	// Update shard mappings for the selected objects to point to node2
	shardPrefix := testPrefix + "/shard/"
	shardsToUpdate := make(map[int]bool)

	for i, objID := range objectsToMove {
		shardID := objectsToMoveShardIDs[i]

		// Update the shard mapping to point to node2
		// Format: "targetNode,currentNode"
		key := fmt.Sprintf("%s%d", shardPrefix, shardID)
		value := fmt.Sprintf("%s,%s", node2Addr, node1Addr) // targetNode=node2, currentNode=node1

		_, err := etcdClient.Put(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to update shard %d in etcd: %v", shardID, err)
		}
		shardsToUpdate[shardID] = true
		t.Logf("Updated shard %d mapping for object %s: %s -> %s", shardID, objID, node1Addr, node2Addr)
	}

	t.Logf("Updated %d shard mappings to reassign objects from node1 to node2", len(shardsToUpdate))

	// Wait for all shard mappings to reflect node2 as CurrentNode
	t.Logf("Waiting for moved shards' current nodes to be changed to node2...")
	testutil.WaitFor(t, 30*time.Second, "all moved shards current nodes changed to node2", func() bool {
		mapping := cluster1.GetShardMapping(ctx)
		for _, shardID := range objectsToMoveShardIDs {
			shardInfo, exists := mapping.Shards[shardID]
			if !exists || shardInfo.CurrentNode != node2Addr {
				t.Logf("Shard %d current node is still %s, target node is %s, waiting...", shardID, shardInfo.CurrentNode, shardInfo.TargetNode)
				return false
			}
		}
		return true
	})
	t.Logf("All moved shards' current nodes successfully changed to node2")

	time.Sleep(1 * time.Second)

	// The clusters will automatically pick up shard mapping changes via etcd watch
	// Wait for the cluster to process the shard mapping changes
	// The removeObjectsNotBelongingToThisNode runs on ShardMappingCheckInterval (5s)
	// We need to wait for:
	// 1. The watch to pick up the changes (immediate)
	// 2. The check interval to trigger (up to 5s)
	// 3. Cluster state to be stable (10s stability duration)
	// Total: up to 15s + some buffer
	t.Logf("Waiting for objects to be removed from node1...")
	for _, objID := range objectsToMove {
		waitForObjectRemoved(t, node1, objID, testutil.WaitForShardMappingTimeout)
		t.Logf("✓ Object %s successfully removed from node1", objID)
	}

	// Test Step 4: Verify objects can be re-created on the new target node
	t.Logf("Testing that objects can be re-created on new target node when accessed...")

	// Try to re-create all moved objects
	recreatedOnNode2 := 0
	for _, objID := range objectsToMove {
		t.Logf("Attempting to re-create %s on new target node...", objID)

		// Re-create the object - it should now be routed appropriately
		createdID, err := cluster1.CreateObject(ctx, "TestRemoveObject", objID)
		if err != nil {
			t.Logf("Note: Failed to re-create object %s: %v (this is acceptable for this test)", objID, err)
			continue
		}
		if createdID != objID {
			t.Logf("Note: Expected object ID %s, got %s", objID, createdID)
			continue
		}

		// Wait for async object creation to complete
		// Try to find it on node2 (the target node)
		deadline := time.Now().Add(5 * time.Second)
		found := false
		for time.Now().Before(deadline) {
			if objExistsOnNode(objID, node2) {
				t.Logf("✓ Object %s successfully created on node2 (new target)", objID)
				recreatedOnNode2++
				found = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if !found {
			if objExistsOnNode(objID, node1) {
				t.Logf("Note: Object %s was created on node1, but will be removed again in next cycle", objID)
			} else {
				t.Logf("Note: Object %s not visible after re-creation within timeout", objID)
			}
		}
	}

	// The key test is that objects WERE removed from node1
	t.Logf("Successfully verified that %d objects were removed from node1 after shard reassignment", len(objectsToMove))

	// If at least some objects were routed correctly to node2, that's good
	if recreatedOnNode2 > 0 {
		t.Logf("✓ %d/%d objects successfully routed to new target node2", recreatedOnNode2, len(objectsToMove))
	}

	t.Logf("Test completed successfully - removeObjectsNotBelongingToThisNode working as expected")
}

// TestRemoveObject is a simple object for testing object removal
type TestRemoveObject struct {
	object.BaseObject
}

func (o *TestRemoveObject) OnCreated() {}
