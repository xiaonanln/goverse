package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterShardCurrentNodeClaiming tests that nodes claim ownership of shards
// by setting CurrentNode when they are the TargetNode and CurrentNode is empty
// This test requires a running etcd instance at localhost:2379
func TestClusterShardCurrentNodeClaiming(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two nodes - node1 will be leader (smaller address)
	node1 := node.NewNode("localhost:51001")
	node2 := node.NewNode("localhost:51002")

	// Start and register node1
	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	// Start and register node2
	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	// Create two clusters to test shard claiming
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", node1, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}

	err = cluster1.Start(ctx, node1)
	if err != nil {
		t.Fatalf("Failed to start cluster1: %v", err)
	}
	defer cluster1.Stop(ctx)

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", node2, "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}

	err = cluster2.Start(ctx, node2)
	if err != nil {
		t.Fatalf("Failed to start cluster2: %v", err)
	}
	defer cluster2.Stop(ctx)

	// Wait for leader election and shard mapping to stabilize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Test that shards have been claimed by the appropriate nodes
	t.Run("ShardOwnershipClaiming", func(t *testing.T) {
		// Get the shard mapping from both clusters
		mapping1, err := cluster1.GetShardMapping(ctx)
		if err != nil {
			t.Fatalf("Failed to get shard mapping from cluster1: %v", err)
		}

		mapping2, err := cluster2.GetShardMapping(ctx)
		if err != nil {
			t.Fatalf("Failed to get shard mapping from cluster2: %v", err)
		}

		// Verify that both clusters see the same shard mapping
		if len(mapping1.Shards) != len(mapping2.Shards) {
			t.Errorf("Clusters have different number of shards: %d vs %d",
				len(mapping1.Shards), len(mapping2.Shards))
		}

		// Count shards claimed by each node
		node1ClaimedCount := 0
		node2ClaimedCount := 0
		unclaimedCount := 0

		for shardID, shardInfo := range mapping1.Shards {
			// Verify that CurrentNode matches TargetNode (ownership has been claimed)
			if shardInfo.CurrentNode == "" {
				unclaimedCount++
				t.Logf("Shard %d is unclaimed (target: %s, current: %s)",
					shardID, shardInfo.TargetNode, shardInfo.CurrentNode)
			} else if shardInfo.CurrentNode == shardInfo.TargetNode {
				// Good - node claimed ownership
				if shardInfo.CurrentNode == "localhost:51001" {
					node1ClaimedCount++
				} else if shardInfo.CurrentNode == "localhost:51002" {
					node2ClaimedCount++
				}
			} else {
				// CurrentNode doesn't match TargetNode - this shouldn't happen
				t.Errorf("Shard %d: CurrentNode (%s) doesn't match TargetNode (%s)",
					shardID, shardInfo.CurrentNode, shardInfo.TargetNode)
			}
		}

		t.Logf("Node1 claimed: %d shards", node1ClaimedCount)
		t.Logf("Node2 claimed: %d shards", node2ClaimedCount)
		t.Logf("Unclaimed: %d shards", unclaimedCount)

		// We expect most or all shards to be claimed
		totalClaimed := node1ClaimedCount + node2ClaimedCount
		if totalClaimed == 0 {
			t.Errorf("No shards were claimed by any node")
		}

		// Both nodes should have claimed some shards (rough distribution check)
		if node1ClaimedCount == 0 {
			t.Errorf("Node1 didn't claim any shards")
		}
		if node2ClaimedCount == 0 {
			t.Errorf("Node2 didn't claim any shards")
		}
	})
}
