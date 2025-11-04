package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestClusterShardMappingIntegration tests shard mapping with actual etcd integration
// This test requires a running etcd instance at localhost:2379
func TestClusterShardMappingIntegration(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two clusters to test leader election and shard mapping
	cluster1, err := newClusterWithEtcdForTesting("TestCluster1", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	cluster1.StartShardMappingManagement(ctx)

	cluster2, err := newClusterWithEtcdForTesting("TestCluster2", "localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	cluster2.StartShardMappingManagement(ctx)
	// Create etcd managers for both clusters with unique test prefix

	// Create nodes - node1 will be leader (smaller address)
	node1 := node.NewNode("localhost:50001")
	node2 := node.NewNode("localhost:50002")

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

	// Wait for registration to complete
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

	// Wait for leader election and shard mapping to stabilize
	time.Sleep(20 * time.Second)

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

	// Test GetNodeForObject
	t.Run("GetNodeForObject", func(t *testing.T) {
		testCases := []string{
			"object1",
			"object2",
			"user-12345",
			"session-abc",
		}

		for _, objectID := range testCases {
			// Both clusters should agree on which node owns the object
			node1, err := cluster1.GetNodeForObject(ctx, objectID)
			if err != nil {
				t.Errorf("cluster1.GetNodeForObject(%s) error: %v", objectID, err)
				continue
			}

			node2, err := cluster2.GetNodeForObject(ctx, objectID)
			if err != nil {
				t.Errorf("cluster2.GetNodeForObject(%s) error: %v", objectID, err)
				continue
			}

			if node1 != node2 {
				t.Errorf("Clusters disagree on node for object %s: %s vs %s", objectID, node1, node2)
			}

			// Verify consistency
			node1b, _ := cluster1.GetNodeForObject(ctx, objectID)
			if node1 != node1b {
				t.Errorf("GetNodeForObject(%s) not consistent: %s vs %s", objectID, node1, node1b)
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
