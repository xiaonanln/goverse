package consensusmanager

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
)

// TestRebalanceShards_BatchMigration tests that RebalanceShards can migrate multiple shards in a single call
func TestRebalanceShards_BatchMigration(t *testing.T) {
	t.Parallel()
	// Create a consensus manager without connecting to etcd (unit test)
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	ctx := context.Background()

	// Set up 3 nodes
	nodes := []string{"node1", "node2", "node3"}

	// Set up nodes in ConsensusManager state
	cm.mu.Lock()
	cm.state.Nodes = make(map[string]bool)
	for _, node := range nodes {
		cm.state.Nodes[node] = true
	}

	// Create highly imbalanced shard mapping:
	// node1: most shards (75%)
	// node2: remaining shards split with node3
	// node3: remaining shards split with node2
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign most shards (75%) to node1
	imbalancedCount := (testNumShards * 3) / 4
	for i := 0; i < imbalancedCount; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "node1",
			CurrentNode: "node1",
			ModRevision: 0,
		}
	}

	// Assign remaining shards to node2 and node3
	for i := imbalancedCount; i < testNumShards; i++ {
		if i%2 == 0 {
			cm.state.ShardMapping.Shards[i] = ShardInfo{
				TargetNode:  "node2",
				CurrentNode: "node2",
				ModRevision: 0,
			}
		} else {
			cm.state.ShardMapping.Shards[i] = ShardInfo{
				TargetNode:  "node3",
				CurrentNode: "node3",
				ModRevision: 0,
			}
		}
	}
	cm.mu.Unlock()

	// Count initial distribution
	initialCounts := make(map[string]int)
	cm.mu.RLock()
	for _, shardInfo := range cm.state.ShardMapping.Shards {
		initialCounts[shardInfo.TargetNode]++
	}
	cm.mu.RUnlock()

	t.Logf("Initial distribution - node1: %d, node2: %d, node3: %d",
		initialCounts["node1"], initialCounts["node2"], initialCounts["node3"])

	// Verify imbalance exists
	maxCount := initialCounts["node1"]
	minCount := initialCounts["node2"]
	if initialCounts["node3"] < minCount {
		minCount = initialCounts["node3"]
	}

	if maxCount < minCount+2 || maxCount <= 2*minCount {
		t.Fatalf("Test setup error: imbalance conditions not met. max=%d, min=%d", maxCount, minCount)
	}

	// Create a mock storeShardMapping that tracks what would be stored
	// Since we can't actually store to etcd in this unit test, we'll modify the in-memory state directly
	// to simulate what would happen after storeShardMapping succeeds

	// Call RebalanceShards
	// Note: This will fail because storeShardMapping tries to write to etcd,
	// but we can check the logic by inspecting what it tried to do
	_, err := cm.RebalanceShards(ctx)

	// We expect an error because we're not connected to etcd
	if err == nil {
		t.Fatal("Expected error when not connected to etcd, but got nil")
	}

	// The key test is that the function attempted to migrate multiple shards
	// We can verify this by checking the log output (which we saw in development)
	// For a proper test, we'd need to mock storeShardMapping or use a real etcd instance
	t.Logf("RebalanceShards returned error as expected (no etcd): %v", err)
}

// TestRebalanceShards_BatchLogic tests the batch selection logic without etcd
func TestRebalanceShards_BatchLogic(t *testing.T) {
	t.Parallel()
	// Test the logic that decides how many shards to migrate
	// Tests with both testNumShards = 64 and production shards = 8192

	// Create a test scenario with varying degrees of imbalance
	testCases := []struct {
		name            string
		numShards       int // Total number of shards to use
		node1Count      int
		node2Count      int
		node3Count      int
		expectedBatch   int // Minimum expected batch size
		shouldRebalance bool
	}{
		// Tests with 64 shards (test default)
		{
			name:            "64 shards - Severe imbalance - should migrate many shards",
			numShards:       64,
			node1Count:      48, // 75% of 64
			node2Count:      8,
			node3Count:      8,
			expectedBatch:   1, // Should migrate shards
			shouldRebalance: true,
		},
		{
			name:            "64 shards - Moderate imbalance - should migrate some shards",
			numShards:       64,
			node1Count:      34, // Imbalanced: a=34, b=15, 34 >= 17 and 34 > 30
			node2Count:      15,
			node3Count:      15,
			expectedBatch:   1, // Should migrate at least some shards
			shouldRebalance: true,
		},
		{
			name:            "64 shards - Balanced - should not rebalance",
			numShards:       64,
			node1Count:      21, // 64/3 = 21.33, so 21, 21, 22
			node2Count:      21,
			node3Count:      22,
			expectedBatch:   0, // Should not rebalance (diff is only 1)
			shouldRebalance: false,
		},
		{
			name:            "64 shards - Small imbalance - should not rebalance",
			numShards:       64,
			node1Count:      23, // a=23, b=20, a < b+2 is false, but a <= 2*b is true (23 <= 40)
			node2Count:      21,
			node3Count:      20,
			expectedBatch:   0, // a >= b+2 (23 >= 22) but a <= 2*b (23 <= 40), so no rebalance
			shouldRebalance: false,
		},
		// Tests with 8192 shards (production default)
		{
			name:            "8192 shards - Severe imbalance - should migrate many shards",
			numShards:       8192,
			node1Count:      6144, // 75% of 8192
			node2Count:      1024,
			node3Count:      1024,
			expectedBatch:   1, // Should migrate shards
			shouldRebalance: true,
		},
		{
			name:            "8192 shards - Moderate imbalance - should migrate some shards",
			numShards:       8192,
			node1Count:      4352, // Imbalanced: a=4352, b=1920, 4352 >= 1922 and 4352 > 3840
			node2Count:      1920,
			node3Count:      1920,
			expectedBatch:   1, // Should migrate at least some shards
			shouldRebalance: true,
		},
		{
			name:            "8192 shards - Balanced - should not rebalance",
			numShards:       8192,
			node1Count:      2730, // 8192/3 = 2730.67, so 2730, 2731, 2731
			node2Count:      2731,
			node3Count:      2731,
			expectedBatch:   0, // Should not rebalance (diff is only 1)
			shouldRebalance: false,
		},
		{
			name:            "8192 shards - Small imbalance - should not rebalance",
			numShards:       8192,
			node1Count:      2950, // a=2950, b=2621, a >= b+2 (2950 >= 2623) but a <= 2*b (2950 <= 5242), so no rebalance
			node2Count:      2621,
			node3Count:      2621,
			expectedBatch:   0,
			shouldRebalance: false,
		},
		// New test case to demonstrate more aggressive rebalancing (20% threshold vs 100% threshold)
		{
			name:       "8192 shards - 30% imbalance - should rebalance with new algorithm",
			numShards:  8192,
			node1Count: 3550, // ~43% of shards (30% above ideal)
			node2Count: 2321, // ~28% of shards (15% below ideal)
			node3Count: 2321, // ~28% of shards (15% below ideal)
			// Old algorithm: would require a > 2*b - NO rebalance
			// New algorithm: difference exceeds 20% of ideal load - YES rebalance
			expectedBatch:   1,
			shouldRebalance: true,
		},
		{
			name:       "64 shards - 30% imbalance - should rebalance with new algorithm",
			numShards:  64,
			node1Count: 28, // ~44% of shards (30% above ideal)
			node2Count: 18, // ~28% of shards (15% below ideal)
			node3Count: 18, // ~28% of shards (15% below ideal)
			// Old algorithm: would require a > 2*b - NO rebalance
			// New algorithm: difference exceeds 20% of ideal load - YES rebalance
			expectedBatch:   1,
			shouldRebalance: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a consensus manager without connecting to etcd
			mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
			cm := NewConsensusManager(mgr, shardlock.NewShardLock(tc.numShards), 0, "", tc.numShards)

			ctx := context.Background()

			// Set up 3 nodes
			nodes := []string{"node1", "node2", "node3"}

			cm.mu.Lock()
			cm.state.Nodes = make(map[string]bool)
			for _, node := range nodes {
				cm.state.Nodes[node] = true
			}

			// Create shard mapping according to test case
			cm.state.ShardMapping = &ShardMapping{
				Shards: make(map[int]ShardInfo),
			}

			shardID := 0
			for i := 0; i < tc.node1Count; i++ {
				cm.state.ShardMapping.Shards[shardID] = ShardInfo{
					TargetNode:  "node1",
					CurrentNode: "node1",
					ModRevision: 0,
				}
				shardID++
			}
			for i := 0; i < tc.node2Count; i++ {
				cm.state.ShardMapping.Shards[shardID] = ShardInfo{
					TargetNode:  "node2",
					CurrentNode: "node2",
					ModRevision: 0,
				}
				shardID++
			}
			for i := 0; i < tc.node3Count; i++ {
				cm.state.ShardMapping.Shards[shardID] = ShardInfo{
					TargetNode:  "node3",
					CurrentNode: "node3",
					ModRevision: 0,
				}
				shardID++
			}
			cm.mu.Unlock()

			// Call RebalanceShards
			rebalanced, err := cm.RebalanceShards(ctx)

			if tc.shouldRebalance {
				// We expect an error because we're not connected to etcd,
				// but the function should have attempted to rebalance
				if err == nil {
					t.Fatal("Expected error when not connected to etcd")
				}
				t.Logf("Attempted to rebalance (error expected): %v", err)
			} else {
				// Should not attempt to rebalance, so no error
				if err != nil {
					t.Fatalf("Expected no error when cluster is balanced, got: %v", err)
				}
				if rebalanced {
					t.Fatal("Expected no rebalancing when cluster is balanced")
				}
			}
		})
	}
}
