package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
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
	cluster1 := newClusterForTesting("TestCluster1")
	cluster2 := newClusterForTesting("TestCluster2")

	// Create etcd managers for both clusters with unique test prefix
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}

	cluster1.SetEtcdManager(etcdMgr1)
	cluster2.SetEtcdManager(etcdMgr2)

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

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
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

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Wait for watches to sync
	time.Sleep(500 * time.Millisecond)

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

	// Test shard mapping initialization by leader
	t.Run("InitializeShardMapping", func(t *testing.T) {
		// Only leader should be able to initialize
		err := cluster1.InitializeShardMapping(ctx)
		if err != nil {
			t.Fatalf("Leader should be able to initialize shard mapping: %v", err)
		}

		// Non-leader should not be able to initialize
		err = cluster2.InitializeShardMapping(ctx)
		if err == nil {
			t.Error("Non-leader should not be able to initialize shard mapping")
		}

		// Wait for etcd to propagate
		time.Sleep(200 * time.Millisecond)

		// Both clusters should be able to retrieve the mapping
		mapping1, err := cluster1.GetShardMapping(ctx)
		if err != nil {
			t.Fatalf("cluster1 failed to get shard mapping: %v", err)
		}

		mapping2, err := cluster2.GetShardMapping(ctx)
		if err != nil {
			t.Fatalf("cluster2 failed to get shard mapping: %v", err)
		}

		// Verify mapping properties
		if len(mapping1.Shards) != sharding.NumShards {
			t.Errorf("mapping1 has %d shards, want %d", len(mapping1.Shards), sharding.NumShards)
		}

		if len(mapping2.Shards) != sharding.NumShards {
			t.Errorf("mapping2 has %d shards, want %d", len(mapping2.Shards), sharding.NumShards)
		}

		if mapping1.Version != 1 {
			t.Errorf("mapping1 version = %d, want 1", mapping1.Version)
		}

		if mapping2.Version != 1 {
			t.Errorf("mapping2 version = %d, want 1", mapping2.Version)
		}

		// Verify all shards are assigned to valid nodes
		validNodes := map[string]bool{
			"localhost:50001": true,
			"localhost:50002": true,
		}

		for shardID := 0; shardID < sharding.NumShards; shardID++ {
			node, ok := mapping1.Shards[shardID]
			if !ok {
				t.Errorf("Shard %d is not assigned", shardID)
			}
			if !validNodes[node] {
				t.Errorf("Shard %d assigned to invalid node %s", shardID, node)
			}
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

// TestClusterShardMappingUpdate tests updating shard mapping when nodes change
func TestClusterShardMappingUpdate(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create two clusters initially
	cluster1 := newClusterForTesting("TestCluster1")
	cluster2 := newClusterForTesting("TestCluster2")

	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}

	cluster1.SetEtcdManager(etcdMgr1)
	cluster2.SetEtcdManager(etcdMgr2)

	node1 := node.NewNode("localhost:51001")
	node2 := node.NewNode("localhost:51002")

	cluster1.SetThisNode(node1)
	cluster2.SetThisNode(node2)

	// Start and register both nodes
	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Initialize shard mapping with 2 nodes
	err = cluster1.InitializeShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize shard mapping: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Get initial mapping
	initialMapping, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get initial mapping: %v", err)
	}

	if initialMapping.Version != 1 {
		t.Errorf("Initial mapping version = %d, want 1", initialMapping.Version)
	}

	// Now add a third node
	cluster3 := newClusterForTesting("TestCluster3")
	etcdMgr3, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 3: %v", err)
	}
	cluster3.SetEtcdManager(etcdMgr3)

	node3 := node.NewNode("localhost:51003")
	cluster3.SetThisNode(node3)

	err = node3.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node3: %v", err)
	}
	defer node3.Stop(ctx)

	err = cluster3.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster3: %v", err)
	}
	defer cluster3.CloseEtcd()

	err = cluster3.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node3: %v", err)
	}
	defer cluster3.UnregisterNode(ctx)

	err = cluster3.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster3: %v", err)
	}

	// Wait for all clusters to see the new node
	time.Sleep(500 * time.Millisecond)

	// Verify all clusters can see 3 nodes
	nodes1 := cluster1.GetNodes()
	if len(nodes1) != 3 {
		t.Fatalf("cluster1 sees %d nodes, want 3", len(nodes1))
	}

	// Leader updates the shard mapping
	err = cluster1.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// With ConsensusManager, cache is automatically updated via watch
	// No need to manually invalidate

	// Get updated mapping
	updatedMapping, err := cluster1.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("Failed to get updated mapping: %v", err)
	}

	// Version should remain 1 if no shards needed reassignment (all stayed on node1/node2)
	// or increment to 2 if shards were reassigned
	if updatedMapping.Version != 1 && updatedMapping.Version != 2 {
		t.Errorf("Updated mapping version = %d, want 1 or 2", updatedMapping.Version)
	}

	t.Logf("Updated mapping version: %d", updatedMapping.Version)

	// All three clusters should see the same mapping
	mapping2, err := cluster2.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("cluster2 failed to get updated mapping: %v", err)
	}

	mapping3, err := cluster3.GetShardMapping(ctx)
	if err != nil {
		t.Fatalf("cluster3 failed to get updated mapping: %v", err)
	}

	if mapping2.Version != updatedMapping.Version {
		t.Errorf("cluster2 mapping version = %d, want %d", mapping2.Version, updatedMapping.Version)
	}

	if mapping3.Version != updatedMapping.Version {
		t.Errorf("cluster3 mapping version = %d, want %d", mapping3.Version, updatedMapping.Version)
	}

	// Verify all shards are assigned to valid nodes
	validNodes := map[string]bool{
		"localhost:51001": true,
		"localhost:51002": true,
		"localhost:51003": true,
	}

	for shardID := 0; shardID < sharding.NumShards; shardID++ {
		node, ok := updatedMapping.Shards[shardID]
		if !ok {
			t.Errorf("Shard %d is not assigned in updated mapping", shardID)
		}
		if !validNodes[node] {
			t.Errorf("Shard %d assigned to invalid node %s in updated mapping", shardID, node)
		}
	}
}
