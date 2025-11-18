package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestShardAssignmentAndRebalancing_Integration tests the full shard assignment and rebalancing flow with etcd
func TestShardAssignmentAndRebalancing_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
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

	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

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

	// Step 1: Create initial shard mapping with all shards assigned and balanced
	t.Run("InitialSetup", func(t *testing.T) {
		nodesMap := map[string]bool{
			"node1": true,
			"node2": true,
			"node3": true,
		}

		// Create fully assigned shard mapping with balanced distribution
		shards := make(map[int]ShardInfo)
		for i := 0; i < sharding.NumShards; i++ {
			nodeIdx := i % len(nodes)
			assignedNode := nodes[nodeIdx]
			shards[i] = ShardInfo{
				TargetNode:  assignedNode,
				CurrentNode: assignedNode, // Fully assigned
			}
		}

		setupShardMapping(t, ctx, cm, mgr, nodesMap, shards)

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)
	})

	// Step 3: Create an imbalanced scenario and test rebalancing
	t.Run("RebalanceImbalancedShards", func(t *testing.T) {
		// First, get current mapping to know which shards exist
		mapping := cm.GetShardMapping()

		// Create an imbalanced scenario: node1 has many shards, others have few
		// Move 2000 shards from node2 and node3 to node1
		// Set both TargetNode and CurrentNode to node1 to ensure they are balanced (not in migration)
		nodesMap := map[string]bool{
			"node1": true,
			"node2": true,
			"node3": true,
		}

		shards := make(map[int]ShardInfo)
		movedCount := 0
		targetMove := 2000

		// Build the full shard map with imbalance
		for i := 0; i < sharding.NumShards; i++ {
			shardInfo := mapping.Shards[i]
			if movedCount < targetMove && shardInfo.CurrentNode != "node1" {
				// Move this shard to node1
				shards[i] = ShardInfo{
					TargetNode:  "node1",
					CurrentNode: "node1",
				}
				movedCount++
			} else {
				// Keep the shard where it is
				shards[i] = ShardInfo{
					TargetNode:  shardInfo.CurrentNode,
					CurrentNode: shardInfo.CurrentNode,
				}
			}
		}

		setupShardMapping(t, ctx, cm, mgr, nodesMap, shards)
		t.Logf("Moved %d shards to create imbalance", movedCount)

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Verify imbalance exists
		mapping = cm.GetShardMapping()
		shardCounts := make(map[string]int)
		for _, node := range nodes {
			shardCounts[node] = 0
		}
		for i := 0; i < sharding.NumShards; i++ {
			// Count shards by TargetNode to match RebalanceShards logic (which uses TargetNode)
			shardCounts[mapping.Shards[i].TargetNode]++
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
			t.Fatal("Expected rebalancing to occur, but it didn't")
		}

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Verify that TargetNode was updated for multiple shards (batch migration)
		// (CurrentNode stays the same until migration completes)
		mapping = cm.GetShardMapping()
		migrationCount := 0
		for i := 0; i < sharding.NumShards; i++ {
			shardInfo := mapping.Shards[i]
			if shardInfo.TargetNode != shardInfo.CurrentNode {
				migrationCount++
			}
		}

		t.Logf("Migrated %d shards during rebalance", migrationCount)
		// With batch rebalancing, we expect multiple shards (up to 100) to be migrated
		// The exact number depends on imbalance conditions, but should be > 1
		if migrationCount < 1 {
			t.Fatalf("Expected at least 1 shard to be migrating, found %d", migrationCount)
		}
		if migrationCount > 100 {
			t.Fatalf("Expected at most 100 shards to be migrating (batch limit), found %d", migrationCount)
		}
	})

	// Step 4: Test that rebalancing doesn't happen when balanced
	t.Run("NoRebalanceWhenBalanced", func(t *testing.T) {
		// Wait a bit for previous test's changes to propagate
		time.Sleep(500 * time.Millisecond)

		// Reset to balanced state
		nodesMap := map[string]bool{
			"node1": true,
			"node2": true,
			"node3": true,
		}

		shards := make(map[int]ShardInfo)
		for i := 0; i < sharding.NumShards; i++ {
			nodeIdx := i % len(nodes)
			node := nodes[nodeIdx]
			shards[i] = ShardInfo{
				TargetNode:  node,
				CurrentNode: node,
			}
		}

		setupShardMapping(t, ctx, cm, mgr, nodesMap, shards)
		t.Logf("Reset to balanced state with %d shards", len(shards))

		// Wait for watch to propagate
		time.Sleep(200 * time.Millisecond)

		// Call rebalance
		rebalanced, err := cm.RebalanceShards(ctx)
		if err != nil {
			t.Fatalf("RebalanceShards failed: %v", err)
		}

		if rebalanced {
			t.Fatal("Expected no rebalancing to occur when already balanced")
		}
	})
}
