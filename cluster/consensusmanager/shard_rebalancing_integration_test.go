package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestShardAssignmentAndRebalancing_Integration tests the full shard assignment and rebalancing flow with etcd
func TestShardAssignmentAndRebalancing_Integration(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create etcd manager and consensus manager
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test - etcd not available: %v", err)
		return
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	cm := NewConsensusManager(mgr)

	// Initialize and start watching
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	// Register 3 nodes
	nodes := []string{"node1", "node2", "node3"}
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	for _, node := range nodes {
		key := nodesPrefix + node
		_, err := mgr.RegisterKeyLease(ctx, key, node, 10)
		if err != nil {
			t.Fatalf("Failed to register node %s: %v", node, err)
		}
	}

	// Wait for nodes to be discovered
	time.Sleep(200 * time.Millisecond)

	// Step 1: Create initial shard mapping with all shards unassigned (CurrentNode empty)
	t.Run("InitialSetup", func(t *testing.T) {
		updateShards := make(map[int]ShardInfo)
		for i := 0; i < sharding.NumShards; i++ {
			nodeIdx := i % len(nodes)
			updateShards[i] = ShardInfo{
				TargetNode:  nodes[nodeIdx],
				CurrentNode: "", // Initially unassigned
				ModRevision: 0,
			}
		}

		n, err := cm.storeShardMapping(ctx, updateShards)
		if err != nil {
			t.Fatalf("Failed to store initial shard mapping: %v", err)
		}
		if n != sharding.NumShards {
			t.Errorf("Expected to store %d shards, stored %d", sharding.NumShards, n)
		}

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)
	})

	// Step 2: Assign unassigned shards
	t.Run("AssignUnassignedShards", func(t *testing.T) {
		assignedCount, err := cm.AssignUnassignedShards(ctx)
		if err != nil {
			t.Fatalf("AssignUnassignedShards failed: %v", err)
		}

		if assignedCount != sharding.NumShards {
			t.Errorf("Expected to assign %d shards, assigned %d", sharding.NumShards, assignedCount)
		}

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Verify all shards are now assigned
		mapping, err := cm.GetShardMapping()
		if err != nil {
			t.Fatalf("Failed to get shard mapping: %v", err)
		}

		unassignedCount := 0
		shardCounts := make(map[string]int)
		for _, node := range nodes {
			shardCounts[node] = 0
		}

		for i := 0; i < sharding.NumShards; i++ {
			shardInfo, exists := mapping.Shards[i]
			if !exists || shardInfo.CurrentNode == "" {
				unassignedCount++
			} else {
				shardCounts[shardInfo.CurrentNode]++
			}
		}

		if unassignedCount > 0 {
			t.Errorf("Expected all shards to be assigned, found %d unassigned", unassignedCount)
		}

		// Verify balanced distribution (should be roughly equal)
		// With 8192 shards and 3 nodes: should be 2731, 2731, 2730
		expectedPerNode := sharding.NumShards / len(nodes)
		for node, count := range shardCounts {
			if count < expectedPerNode-1 || count > expectedPerNode+1 {
				t.Errorf("Node %s has %d shards, expected around %d", node, count, expectedPerNode)
			}
		}
	})

	// Step 3: Create an imbalanced scenario and test rebalancing
	t.Run("RebalanceImbalancedShards", func(t *testing.T) {
		// First, get current mapping to preserve ModRevisions
		mapping, err := cm.GetShardMapping()
		if err != nil {
			t.Fatalf("Failed to get shard mapping: %v", err)
		}

		// Create an imbalanced scenario: node1 has many, node3 has few
		// Move 2000 shards from node2 and node3 to node1
		updateShards := make(map[int]ShardInfo)
		movedCount := 0
		targetMove := 2000

		for i := 0; i < sharding.NumShards && movedCount < targetMove; i++ {
			shardInfo := mapping.Shards[i]
			if shardInfo.CurrentNode != "node1" {
				updateShards[i] = ShardInfo{
					TargetNode:  shardInfo.TargetNode,
					CurrentNode: "node1",
					ModRevision: shardInfo.ModRevision,
				}
				movedCount++
			}
		}

		n, err := cm.storeShardMapping(ctx, updateShards)
		if err != nil {
			t.Fatalf("Failed to create imbalanced mapping: %v", err)
		}
		t.Logf("Moved %d shards to create imbalance", n)

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Verify imbalance exists
		mapping, _ = cm.GetShardMapping()
		shardCounts := make(map[string]int)
		for _, node := range nodes {
			shardCounts[node] = 0
		}
		for i := 0; i < sharding.NumShards; i++ {
			shardCounts[mapping.Shards[i].CurrentNode]++
		}

		t.Logf("Before rebalance - node1: %d, node2: %d, node3: %d",
			shardCounts["node1"], shardCounts["node2"], shardCounts["node3"])

		// Find max and min
		maxCount := 0
		minCount := sharding.NumShards
		for _, count := range shardCounts {
			if count > maxCount {
				maxCount = count
			}
			if count < minCount {
				minCount = count
			}
		}

		// Verify conditions for rebalancing are met
		if maxCount < minCount+2 || maxCount <= 2*minCount {
			t.Fatalf("Imbalance conditions not met: max=%d, min=%d", maxCount, minCount)
		}

		// Call rebalance
		rebalanced, err := cm.RebalanceShards(ctx)
		if err != nil {
			t.Fatalf("RebalanceShards failed: %v", err)
		}

		if !rebalanced {
			t.Error("Expected rebalancing to occur, but it didn't")
		}

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Verify that TargetNode was updated for one shard
		// (CurrentNode stays the same until migration completes)
		mapping, _ = cm.GetShardMapping()
		migrationCount := 0
		for i := 0; i < sharding.NumShards; i++ {
			shardInfo := mapping.Shards[i]
			if shardInfo.TargetNode != shardInfo.CurrentNode {
				migrationCount++
			}
		}

		if migrationCount != 1 {
			t.Errorf("Expected exactly 1 shard to be migrating, found %d", migrationCount)
		}
	})

	// Step 4: Test that rebalancing doesn't happen when balanced
	t.Run("NoRebalanceWhenBalanced", func(t *testing.T) {
		// Reset to balanced state
		updateShards := make(map[int]ShardInfo)
		for i := 0; i < sharding.NumShards; i++ {
			nodeIdx := i % len(nodes)
			node := nodes[nodeIdx]
			updateShards[i] = ShardInfo{
				TargetNode:  node,
				CurrentNode: node,
				ModRevision: 0,
			}
		}

		n, err := cm.storeShardMapping(ctx, updateShards)
		if err != nil {
			t.Fatalf("Failed to create balanced mapping: %v", err)
		}
		t.Logf("Reset to balanced state with %d shards", n)

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Call rebalance
		rebalanced, err := cm.RebalanceShards(ctx)
		if err != nil {
			t.Fatalf("RebalanceShards failed: %v", err)
		}

		if rebalanced {
			t.Error("Expected no rebalancing to occur when already balanced")
		}
	})
}
