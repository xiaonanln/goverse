package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestReleaseShardOwnership_Integration tests the complete shard ownership release flow
// This test verifies that a node releases ownership of a shard when:
// - CurrentNode is this node
// - TargetNode is another node
// - No objects exist on this node for that shard
func TestReleaseShardOwnership_Integration(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1 := node.NewNode("localhost:52001")
	node2 := node.NewNode("localhost:52002")

	// Create clusters for both nodes - node1 will be leader (smaller address)
	cluster1, err := newClusterWithEtcdForTesting("Cluster1", node1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer cluster1.Stop(ctx)

	cluster2, err := newClusterWithEtcdForTesting("Cluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer cluster2.Stop(ctx)

	// Start both clusters
	err = cluster1.Start(ctx, node1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for leader election and shard mapping to stabilize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Get a shard ID that we know belongs to node1 initially
	// We'll create an object on node1, then change the target to node2, 
	// then delete the object, and verify node1 releases the shard
	testObjectID := "TestObject-ownership-release"
	targetShardID := sharding.GetShardID(testObjectID)

	t.Logf("Using object ID: %s, shard ID: %d", testObjectID, targetShardID)

	// Create the object on node1 (will be routed based on shard mapping)
	// We need to force creation on node1 by using the fixed node format
	fixedObjectID := "localhost:52001/" + testObjectID
	_, err = node1.CreateObject(ctx, "TestObject", fixedObjectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait a bit for shard claiming to happen
	time.Sleep(2 * time.Second)

	// Verify the shard mapping shows node1 as CurrentNode for this shard
	mapping1, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping: %v", err)
	}

	shardInfo := mapping1.Shards[targetShardID]
	t.Logf("Shard %d - Target: %s, Current: %s", targetShardID, shardInfo.TargetNode, shardInfo.CurrentNode)

	// Now delete the object from node1
	err = node1.DeleteObject(ctx, fixedObjectID)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Manually update the shard mapping to change TargetNode to node2
	// This simulates a cluster rebalancing scenario
	// In real scenarios, the leader would do this via ReassignShardTargetNodes
	consensusMgr := cluster1.GetConsensusManagerForTesting()
	err = consensusMgr.ReleaseShardsForNode(ctx, "localhost:52001", map[int]int{targetShardID: 0})
	if err != nil {
		t.Logf("Initial release attempt (expected to not release if target still points to node1): %v", err)
	}

	// Now let's force the target to change to node2 by manipulating the mapping
	// In a real scenario, this would happen when the leader reassigns shards
	// We'll simulate by directly updating etcd through the consensus manager
	etcdMgr := cluster1.GetEtcdManagerForTesting()
	client := etcdMgr.GetClient()
	shardKey := fmt.Sprintf("%s/shard/%d", etcdMgr.GetPrefix(), targetShardID)
	
	// Get current shard info
	resp, err := client.Get(ctx, shardKey)
	if err != nil {
		t.Fatalf("Failed to get shard from etcd: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Shard %d not found in etcd", targetShardID)
	}

	// Update TargetNode to node2 but keep CurrentNode as node1
	newValue := "localhost:52002,localhost:52001"
	_, err = client.Put(ctx, shardKey, newValue)
	if err != nil {
		t.Fatalf("Failed to update shard in etcd: %v", err)
	}

	// Wait for watch to pick up the change
	time.Sleep(1 * time.Second)

	// Now trigger the release by calling the cluster's periodic check
	// which includes releaseShardOwnership
	cluster1.releaseShardOwnership(ctx)

	// Wait a bit for the release to be processed
	time.Sleep(1 * time.Second)

	// Verify the shard mapping shows CurrentNode is now empty (released)
	resp2, err := client.Get(ctx, shardKey)
	if err != nil {
		t.Fatalf("Failed to get updated shard from etcd: %v", err)
	}
	if len(resp2.Kvs) == 0 {
		t.Fatalf("Shard %d not found in etcd after release", targetShardID)
	}

	// Parse the value to check CurrentNode
	parts := string(resp2.Kvs[0].Value)
	t.Logf("Shard %d after release: %s", targetShardID, parts)

	// The value should now be "localhost:52002," (target node, empty current node)
	expectedValue := "localhost:52002,"
	if parts != expectedValue {
		t.Errorf("Expected shard %d to have value %s after release, got %s", 
			targetShardID, expectedValue, parts)
	}
}

// TestReleaseShardOwnership_WithObjects tests that shards with objects are NOT released
func TestReleaseShardOwnership_WithObjects(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1 := node.NewNode("localhost:52101")
	node2 := node.NewNode("localhost:52102")

	// Create clusters for both nodes
	cluster1, err := newClusterWithEtcdForTesting("Cluster1", node1, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer cluster1.Stop(ctx)

	cluster2, err := newClusterWithEtcdForTesting("Cluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}
	defer cluster2.Stop(ctx)

	// Start both clusters
	err = cluster1.Start(ctx, node1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}

	// Wait for stabilization
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create an object on node1
	testObjectID := "TestObject-with-objects"
	fixedObjectID := "localhost:52101/" + testObjectID
	targetShardID := sharding.GetShardID(testObjectID)

	_, err = node1.CreateObject(ctx, "TestObject", fixedObjectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait for shard claiming
	time.Sleep(2 * time.Second)

	// Get the etcd manager and update TargetNode to node2
	etcdMgr := cluster1.GetEtcdManagerForTesting()
	client := etcdMgr.GetClient()
	shardKey := fmt.Sprintf("%s/shard/%d", etcdMgr.GetPrefix(), targetShardID)
	
	// Update TargetNode to node2 but keep CurrentNode as node1
	newValue := "localhost:52102,localhost:52101"
	_, err = client.Put(ctx, shardKey, newValue)
	if err != nil {
		t.Fatalf("Failed to update shard in etcd: %v", err)
	}

	// Wait for watch to pick up the change
	time.Sleep(1 * time.Second)

	// Now try to release - should NOT release because object still exists
	cluster1.releaseShardOwnership(ctx)

	// Wait a bit
	time.Sleep(1 * time.Second)

	// Verify CurrentNode is still node1 (not released)
	resp, err := client.Get(ctx, shardKey)
	if err != nil {
		t.Fatalf("Failed to get shard from etcd: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Shard %d not found in etcd", targetShardID)
	}

	parts := string(resp.Kvs[0].Value)
	t.Logf("Shard %d with objects present: %s", targetShardID, parts)

	// The value should still be "localhost:52102,localhost:52101" (not released)
	expectedValue := "localhost:52102,localhost:52101"
	if parts != expectedValue {
		t.Errorf("Expected shard %d to still have CurrentNode set (not released), got %s", 
			targetShardID, parts)
	}
}
