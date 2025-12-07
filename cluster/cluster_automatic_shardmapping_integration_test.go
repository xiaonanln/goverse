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
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start both clusters - node1 will be leader (smaller address)
	cluster1 := mustNewCluster(ctx, t, "localhost:50011", testPrefix)
	cluster2 := mustNewCluster(ctx, t, "localhost:50012", testPrefix)

	// Wait for leadership election to complete
	testutil.WaitFor(t, 5*time.Second, "leadership election to complete", func() bool {
		return cluster1.IsLeader() || cluster2.IsLeader()
	})

	// Determine which cluster is the leader
	var leaderCluster, followerCluster *Cluster
	if cluster1.IsLeader() {
		leaderCluster = cluster1
		followerCluster = cluster2
		t.Logf("Leader is cluster1 (localhost:50011)")
	} else {
		leaderCluster = cluster2
		followerCluster = cluster1
		t.Logf("Leader is cluster2 (localhost:50012)")
	}

	// Test 1: Wait for node list to become stable and shard mapping to be created
	t.Logf("Waiting for node list to stabilize and shard mapping to be created...")
	testutil.WaitForClusterReady(t, leaderCluster)

	// After stability period, leader should have initialized shard mapping
	mappingLeader := leaderCluster.GetShardMapping(ctx)
	if mappingLeader == nil {
		t.Fatalf("Shard mapping should be initialized by leader")
	}
	// Note: Version is now tracked in ClusterState, not ShardMapping

	t.Logf("Shard mapping initialized by leader")

	// Test 2: Non-leader should also be able to get the shard mapping from etcd
	mappingFollower := followerCluster.GetShardMapping(ctx)
	if mappingFollower == nil {
		t.Fatalf("Shard mapping should be available to non-leader")
	}
	// Both should have same shard assignments
	if len(mappingFollower.Shards) != len(mappingLeader.Shards) {
		t.Fatalf("Both clusters should have same shard mapping, got %d and %d shards", len(mappingLeader.Shards), len(mappingFollower.Shards))
	}

	t.Logf("Non-leader successfully retrieved shard mapping")

	// Test 3: Verify shard assignments
	nodes := leaderCluster.GetNodes()
	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}

	// Check that all shards are assigned
	shardCount := 0
	for _, node := range nodes {
		count := 0
		for _, assignedNode := range mappingLeader.Shards {
			if assignedNode.TargetNode == node {
				count++
			}
		}
		t.Logf("Node %s has %d shards", node, count)
		shardCount += count
	}

	expectedShards := testutil.TestNumShards
	if shardCount != expectedShards {
		t.Fatalf("Expected %d total shards assigned, got %d", expectedShards, shardCount)
	}

	// Test 4: Verify automatic cache sync via watch mechanism
	// With ConsensusManager, the cache is kept in sync automatically via watch
	// We just need to verify that the non-leader maintains correct shard mapping
	t.Logf("Verifying automatic cache sync on non-leader...")

	// Non-leader should maintain correct mapping via automatic watch mechanism
	mappingFollowerNew := followerCluster.GetShardMapping(ctx)
	// Verify it still has correct number of shards
	if len(mappingFollowerNew.Shards) != len(mappingLeader.Shards) {
		t.Fatalf("Mapping should have same shards, got %d, expected %d", len(mappingFollowerNew.Shards), len(mappingLeader.Shards))
	}

	t.Logf("Non-leader maintains correct shard mapping via automatic sync")

	t.Logf("Test completed successfully - automatic shard mapping management is working")
}

// TestClusterShardMappingAutoUpdate tests that shard mapping is updated when nodes change
func TestClusterShardMappingAutoUpdate(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create first cluster and node
	node1 := node.NewNode("localhost:50021", testutil.TestNumShards)
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", node1, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}

	err = cluster1.Start(ctx, node1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}
	defer cluster1.Stop(ctx)

	// Wait for stability and initial mapping
	t.Logf("Waiting for initial shard mapping...")
	testutil.WaitForClusterReady(t, cluster1)

	_ = cluster1.GetShardMapping(ctx)
	t.Logf("Initial shard mapping created")

	// Now add a second node
	node2 := node.NewNode("localhost:50022", testutil.TestNumShards)
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}
	defer cluster2.Stop(ctx)

	t.Logf("Added second node, waiting for stability and shard mapping update...")

	// Wait for node list to stabilize and mapping to be updated
	testutil.WaitForClusterReady(t, cluster2)

	// Wait for rebalancing to complete - both nodes should appear in the shard mapping
	expectedNode1 := "localhost:50021"
	expectedNode2 := "localhost:50022"

	testutil.WaitFor(t, 15*time.Second, "both nodes to appear in shard mapping after rebalancing", func() bool {
		updatedMapping := cluster1.GetShardMapping(ctx)
		nodeSet := make(map[string]bool)
		for _, assignedNode := range updatedMapping.Shards {
			nodeSet[assignedNode.TargetNode] = true
		}
		return len(nodeSet) == 2 && nodeSet[expectedNode1] && nodeSet[expectedNode2]
	})

	t.Logf("Test completed - shard mapping successfully updated when nodes changed")
}
