package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterShardMappingIntegration tests shard mapping with actual etcd integration
// This test requires a running etcd instance at localhost:2379
func TestClusterShardMappingIntegration(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start both clusters - node1 will be leader (smaller address)
	cluster1 := mustNewCluster(ctx, t, "localhost:50001", testPrefix)
	cluster2 := mustNewCluster(ctx, t, "localhost:50002", testPrefix)

	// Wait for leader election and shard mapping to stabilize
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Test leader detection
	t.Run("LeaderDetection", func(t *testing.T) {
		// node1 should be the leader (smallest address)
		if !cluster1.IsLeader() {
			t.Errorf("cluster1 (localhost:50001) should be the leader")
		}

		if cluster2.IsLeader() {
			t.Errorf("cluster2 (localhost:50002) should not be the leader")
		}

		leader1 := cluster1.GetLeaderNode()
		leader2 := cluster2.GetLeaderNode()

		if leader1 != "localhost:50001" {
			t.Errorf("cluster1 sees leader as %s, want localhost:50001", leader1)
		}

		if leader2 != "localhost:50001" {
			t.Errorf("cluster2 sees leader as %s, want localhost:50001", leader2)
		}
	})

	// Test GetCurrentNodeForObject
	t.Run("GetCurrentNodeForObject", func(t *testing.T) {
		testCases := []string{
			"object1",
			"object2",
			"user-12345",
			"session-abc",
		}

		for _, objectID := range testCases {
			// Both clusters should agree on which node owns the object
			node1, err := cluster1.GetCurrentNodeForObject(ctx, objectID)
			if err != nil {
				t.Errorf("cluster1.GetCurrentNodeForObject(%s) error: %v", objectID, err)
				continue
			}

			node2, err := cluster2.GetCurrentNodeForObject(ctx, objectID)
			if err != nil {
				t.Errorf("cluster2.GetCurrentNodeForObject(%s) error: %v", objectID, err)
				continue
			}

			if node1 != node2 {
				t.Errorf("Clusters disagree on node for object %s: %s vs %s", objectID, node1, node2)
			}

			// Verify consistency
			node1b, _ := cluster1.GetCurrentNodeForObject(ctx, objectID)
			if node1 != node1b {
				t.Errorf("GetCurrentNodeForObject(%s) not consistent: %s vs %s", objectID, node1, node1b)
			}
		}
	})

	// Test GetNodeForShard
	t.Run("GetNodeForShard", func(t *testing.T) {
		testCases := []int{0, 1, 100, 1000, sharding.NumShards - 1}

		for _, shardID := range testCases {
			// Both clusters should agree on which node owns the shard
			node1, err := cluster1.GetNodeForShard(ctx, shardID)
			if err != nil {
				t.Errorf("cluster1.GetNodeForShard(%d) error: %v", shardID, err)
				continue
			}

			node2, err := cluster2.GetNodeForShard(ctx, shardID)
			if err != nil {
				t.Errorf("cluster2.GetNodeForShard(%d) error: %v", shardID, err)
				continue
			}

			if node1 != node2 {
				t.Errorf("Clusters disagree on node for shard %d: %s vs %s", shardID, node1, node2)
			}
		}

		// Test invalid shard IDs
		invalidCases := []int{-1, sharding.NumShards, sharding.NumShards + 1}
		for _, shardID := range invalidCases {
			_, err := cluster1.GetNodeForShard(ctx, shardID)
			if err == nil {
				t.Errorf("GetNodeForShard(%d) should return error for invalid shard ID", shardID)
			}
		}
	})
}
