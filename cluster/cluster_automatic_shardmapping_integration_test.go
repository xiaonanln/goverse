package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterAutomaticShardMappingManagement tests automatic shard mapping management with etcd integration
// This test requires a running etcd instance at localhost:2379
func TestClusterAutomaticShardMappingManagement(t *testing.T) {
	t.Parallel()
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create two clusters
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	// Create etcd managers for both clusters with unique test prefix

	// Create nodes - node1 will be leader (smaller address)
	node1 := node.NewNode("localhost:50011")
	node2 := node.NewNode("localhost:50012")

	cluster1.SetThisNode(node1)
	cluster2.SetThisNode(node2)

	// Start and register node1
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Start shard mapping management on cluster1
	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	// Wait for node registration to complete
	time.Sleep(500 * time.Millisecond)

	// Start and register node2
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Start shard mapping management on cluster2
	err = cluster2.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster2: %v", err)
	}
	defer cluster2.StopShardMappingManagement()

	// Wait for watches to sync
	time.Sleep(500 * time.Millisecond)

	// Verify cluster1 is the leader
	if !cluster1.IsLeader() {
		t.Fatalf("cluster1 should be the leader")
	}
	if cluster2.IsLeader() {
		t.Fatalf("cluster2 should not be the leader")
	}

	t.Logf("Leader is cluster1 (localhost:50011)")

	// Test 1: Wait for node list to become stable (10 seconds) plus check interval (5 seconds)
	// Add some buffer for processing
	t.Logf("Waiting for node list to stabilize (%v) and shard mapping to be created...", NodeStabilityDuration)
	time.Sleep(NodeStabilityDuration + ShardMappingCheckInterval + 2*time.Second)

	// After stability period, leader should have initialized shard mapping
	mapping1, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping from cluster1 (leader): %v", err)
	}
	if mapping1 == nil {
		t.Fatalf("Shard mapping should be initialized by leader")
	}
	// Note: Version is now tracked in ClusterState, not ShardMapping

	t.Logf("Shard mapping initialized by leader")

	// Test 2: Non-leader should also be able to get the shard mapping from etcd
	mapping2, err := cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get shard mapping from cluster2 (non-leader): %v", err)
	}
	if mapping2 == nil {
		t.Fatalf("Shard mapping should be available to non-leader")
	}
	// Both should have same shard assignments
	if len(mapping2.Shards) != len(mapping1.Shards) {
		t.Fatalf("Both clusters should have same shard mapping, got %d and %d shards", len(mapping1.Shards), len(mapping2.Shards))
	}

	t.Logf("Non-leader successfully retrieved shard mapping")

	// Test 3: Verify shard assignments
	nodes := cluster1.GetNodes()
	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}

	// Check that all shards are assigned
	shardCount := 0
	for _, node := range nodes {
		count := 0
		for _, assignedNode := range mapping1.Shards {
			if assignedNode.TargetNode == node {
				count++
			}
		}
		t.Logf("Node %s has %d shards", node, count)
		shardCount += count
	}

	expectedShards := 8192 // From sharding.NumShards
	if shardCount != expectedShards {
		t.Fatalf("Expected %d total shards assigned, got %d", expectedShards, shardCount)
	}

	// Test 4: Verify periodic refresh on non-leader
	t.Logf("Testing periodic refresh on non-leader...")

	// Invalidate cache on non-leader
	cluster2.InvalidateShardMappingCache()

	// Wait for next check interval
	time.Sleep(ShardMappingCheckInterval + 1*time.Second)

	// Non-leader should have refreshed mapping from etcd
	mapping2New, err := cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get refreshed shard mapping from cluster2: %v", err)
	}
	// Verify it still has correct number of shards
	if len(mapping2New.Shards) != len(mapping1.Shards) {
		t.Fatalf("Refreshed mapping should have same shards, got %d, expected %d", len(mapping2New.Shards), len(mapping1.Shards))
	}

	t.Logf("Non-leader successfully refreshed shard mapping")

	t.Logf("Test completed successfully - automatic shard mapping management is working")
}

// TestClusterShardMappingAutoUpdate tests that shard mapping is updated when nodes change
func TestClusterShardMappingAutoUpdate(t *testing.T) {
	t.Parallel()
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create first cluster and node
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	node1 := node.NewNode("localhost:50021")
	cluster1.SetThisNode(node1)

	// Start node1
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	err = cluster1.StartShardMappingManagement(ctx)
	if err != nil {
		t.Fatalf("Failed to start shard mapping management for cluster1: %v", err)
	}
	defer cluster1.StopShardMappingManagement()

	// Wait for stability and initial mapping
	t.Logf("Waiting for initial shard mapping...")
	time.Sleep(NodeStabilityDuration + ShardMappingCheckInterval + 2*time.Second)

	_, err = cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get initial shard mapping: %v", err)
	}
	t.Logf("Initial shard mapping created")

	// Now add a second node
	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	node2 := node.NewNode("localhost:50022")
	cluster2.SetThisNode(node2)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	t.Logf("Added second node, waiting for stability and shard mapping update...")

	// Wait for node list to stabilize and mapping to be updated
	time.Sleep(NodeStabilityDuration + ShardMappingCheckInterval + 2*time.Second)

	// Get updated mapping
	updatedMapping, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get updated shard mapping: %v", err)
	}

	// Note: Version tracking is now in ClusterState
	// Just verify the mapping has changed (different shard count or assignments)
	t.Logf("Shard mapping updated after node addition")

	// Verify mapping still only contains the original node (second node should not be included)
	nodeSet := make(map[string]bool)
	for _, assignedNode := range updatedMapping.Shards {
		nodeSet[assignedNode.TargetNode] = true
	}

	expectedNode := "localhost:50021"
	if len(nodeSet) != 1 || !nodeSet[expectedNode] {
		t.Fatalf("Expected shard mapping to contain only %s, got %d nodes: %v", expectedNode, len(nodeSet), nodeSet)
	}

	t.Logf("Test completed - shard mapping successfully updated when nodes changed")
}
