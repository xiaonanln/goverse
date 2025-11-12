package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterShardCurrentNodeClaiming tests that nodes claim ownership of shards
// by setting CurrentNode when they are the TargetNode and CurrentNode is empty
// This test requires a running etcd instance at localhost:2379
func TestClusterShardCurrentNodeClaiming(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start both clusters - node1 will be leader (smaller address)
	cluster1 := mustNewCluster(ctx, t, "localhost:51001", testPrefix)
	cluster2 := mustNewCluster(ctx, t, "localhost:51002", testPrefix)

	// Wait for leader election and shard mapping to stabilize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Test that shards have been claimed by the appropriate nodes
	t.Run("ShardOwnershipClaiming", func(t *testing.T) {
		// Get the shard mapping from both clusters
		mapping1 := cluster1.GetShardMapping(ctx)

		mapping2 := cluster2.GetShardMapping(ctx)

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
