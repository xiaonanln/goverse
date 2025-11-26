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

// TestReleaseObject is a simple test object for shard release ownership tests
type TestReleaseObject struct {
	object.BaseObject
}

func (o *TestReleaseObject) OnCreated() {}

// TestReleaseShardOwnership_Integration tests the complete shard ownership release flow
// This test verifies that a node releases ownership of a shard when:
// - CurrentNode is this node
// - TargetNode is another node
// - No objects exist on this node for that shard
func TestReleaseShardOwnership_Integration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes
	node1 := node.NewNode("localhost:52001", testutil.TestNumShards)
	node2 := node.NewNode("localhost:52002", testutil.TestNumShards)

	// Register test object type
	node1.RegisterObjectType((*TestReleaseObject)(nil))
	node2.RegisterObjectType((*TestReleaseObject)(nil))

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
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Pick a shard that belongs to node1
	// We'll use a shard ID directly without creating objects
	targetShardID := 10 // Arbitrary shard ID for testing (must be < 64 since we use TestNumShards)

	t.Logf("Using shard ID: %d", targetShardID)

	// Manually update the shard mapping to change TargetNode to node2
	// This simulates a cluster rebalancing scenario where the target changes
	// In real scenarios, the leader would do this via ReassignShardTargetNodes
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
	t.Logf("Waiting for shard ownership release to be processed...")
	time.Sleep(3 * time.Second)

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
	expectedValues := map[string]bool{
		"localhost:52002,":                true,
		"localhost:52002,localhost:52002": true,
	}
	// Accept either the empty-current-node form or duplicated-target form because the target node might have claimed it
	if !expectedValues[parts] {
		t.Fatalf("Expected shard %d to have value %v after release, got %s",
			targetShardID, expectedValues, parts)
	}
}
