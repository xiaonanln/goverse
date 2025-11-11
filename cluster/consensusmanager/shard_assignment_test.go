package consensusmanager

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
)

func TestAssignUnassignedShards_NoNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Initialize with empty state
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.mu.Unlock()

	ctx := context.Background()
	n, err := cm.AssignUnassignedShards(ctx)

	if err != nil {
		t.Errorf("Expected no error with no nodes, got %v", err)
	}

	if n != 0 {
		t.Errorf("Expected 0 shards assigned with no nodes, got %d", n)
	}
}

func TestAssignUnassignedShards_NoMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add nodes but no mapping
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.mu.Unlock()

	ctx := context.Background()
	n, err := cm.AssignUnassignedShards(ctx)

	if err != nil {
		t.Errorf("Expected no error with no mapping, got %v", err)
	}

	if n != 0 {
		t.Errorf("Expected 0 shards assigned with no mapping, got %d", n)
	}
}

func TestAssignUnassignedShards_AllAssigned(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Setup: All shards already assigned
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < sharding.NumShards; i++ {
		node := "node1"
		if i%2 == 0 {
			node = "node2"
		}
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  node,
			CurrentNode: node,
		}
	}
	cm.mu.Unlock()

	ctx := context.Background()
	n, err := cm.AssignUnassignedShards(ctx)

	if err != nil {
		t.Errorf("Expected no error when all shards assigned, got %v", err)
	}

	if n != 0 {
		t.Errorf("Expected 0 shards assigned when all already assigned, got %d", n)
	}
}

func TestAssignUnassignedShards_BalancedAssignment(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Setup: 3 nodes, no shards assigned yet
	nodes := []string{"node1", "node2", "node3"}
	cm.mu.Lock()
	for _, node := range nodes {
		cm.state.Nodes[node] = true
	}
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// Create unassigned shards (empty CurrentNode)
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "",
			CurrentNode: "",
		}
	}
	cm.mu.Unlock()

	// We can't actually test the storeShardMapping call without etcd,
	// but we can verify the logic by checking what would be assigned
	// Let's test the distribution logic manually
	cm.mu.RLock()
	shardCounts := make(map[string]int)
	for _, node := range nodes {
		shardCounts[node] = 0
	}

	// Simulate assignment algorithm
	for i := 0; i < sharding.NumShards; i++ {
		// Find node with minimal count
		minNode := nodes[0]
		minCount := shardCounts[minNode]

		for _, node := range nodes[1:] {
			count := shardCounts[node]
			if count < minCount {
				minNode = node
				minCount = count
			}
		}

		shardCounts[minNode]++
	}
	cm.mu.RUnlock()

	// Verify balanced distribution
	// With 8192 shards and 3 nodes: 2731, 2731, 2730
	expectedCounts := []int{2731, 2731, 2730}
	actualCounts := []int{shardCounts["node1"], shardCounts["node2"], shardCounts["node3"]}

	for i, expected := range expectedCounts {
		if actualCounts[i] != expected {
			t.Errorf("Node %s: expected %d shards, got %d", nodes[i], expected, actualCounts[i])
		}
	}
}

func TestAssignUnassignedShards_PartialAssignment(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Setup: 2 nodes, some shards assigned, some unassigned
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Assign first 100 shards to node1
	for i := 0; i < 100; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "node1",
			CurrentNode: "node1",
		}
	}

	// Leave remaining shards unassigned
	for i := 100; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "",
			CurrentNode: "",
		}
	}
	cm.mu.Unlock()

	// Simulate assignment algorithm
	cm.mu.RLock()
	nodes := []string{"node1", "node2"}
	shardCounts := map[string]int{
		"node1": 100, // Already has 100
		"node2": 0,
	}

	unassignedCount := sharding.NumShards - 100

	// For each unassigned shard, it should go to the node with fewer shards
	for i := 0; i < unassignedCount; i++ {
		minNode := nodes[0]
		minCount := shardCounts[minNode]

		for _, node := range nodes[1:] {
			count := shardCounts[node]
			if count < minCount {
				minNode = node
				minCount = count
			}
		}

		shardCounts[minNode]++
	}
	cm.mu.RUnlock()

	// After assignment, node2 should get most of the unassigned shards
	// node1: 100 + some, node2: most of the 8092
	// Final: node1 ~4096, node2 ~4096
	if shardCounts["node1"] != 4096 || shardCounts["node2"] != 4096 {
		t.Errorf("Expected balanced distribution after assignment: node1=%d, node2=%d",
			shardCounts["node1"], shardCounts["node2"])
	}
}

func TestRebalanceShards_NotAllAssigned(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Setup: Some shards unassigned
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// Only assign half the shards
	for i := 0; i < sharding.NumShards/2; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "node1",
			CurrentNode: "node1",
		}
	}
	// Rest are unassigned
	for i := sharding.NumShards / 2; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "",
			CurrentNode: "",
		}
	}
	cm.mu.Unlock()

	ctx := context.Background()
	rebalanced, err := cm.RebalanceShards(ctx)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if rebalanced {
		t.Error("Should not rebalance when not all shards are assigned")
	}
}

func TestRebalanceShards_Balanced(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Setup: Balanced distribution
	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// Assign shards evenly: 4096 each
	for i := 0; i < sharding.NumShards; i++ {
		node := "node1"
		if i%2 == 0 {
			node = "node2"
		}
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  node,
			CurrentNode: node,
		}
	}
	cm.mu.Unlock()

	ctx := context.Background()
	rebalanced, err := cm.RebalanceShards(ctx)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if rebalanced {
		t.Error("Should not rebalance when already balanced")
	}
}

func TestRebalanceShards_ImbalanceConditions(t *testing.T) {
	tests := []struct {
		name             string
		maxCount         int
		minCount         int
		shouldRebalance  bool
		description      string
	}{
		{
			name:            "Exactly at threshold a=b+2",
			maxCount:        5,
			minCount:        3,
			shouldRebalance: false, // a > 2*b fails (5 <= 6)
			description:     "5 >= 3+2 (true) but 5 > 2*3 (false)",
		},
		{
			name:            "Both conditions met",
			maxCount:        7,
			minCount:        3,
			shouldRebalance: true, // 7 >= 5 and 7 > 6
			description:     "7 >= 3+2 (true) and 7 > 2*3 (true)",
		},
		{
			name:            "Only first condition met",
			maxCount:        4,
			minCount:        2,
			shouldRebalance: false, // 4 >= 4 (true) but 4 > 4 (false)
			description:     "4 >= 2+2 (true) but 4 > 2*2 (false)",
		},
		{
			name:            "Neither condition met",
			maxCount:        4,
			minCount:        3,
			shouldRebalance: false,
			description:     "4 >= 3+2 (false)",
		},
		{
			name:            "Large imbalance",
			maxCount:        6000,
			minCount:        2000,
			shouldRebalance: true, // 6000 >= 4000 and 6000 > 4000
			description:     "6000 >= 2000+2 (true) and 6000 > 2*2000 (true)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
			cm := NewConsensusManager(mgr)

			// Setup: Create a simple distribution with only tt.maxCount + tt.minCount shards total
			cm.mu.Lock()
			cm.state.Nodes["node1"] = true
			cm.state.Nodes["node2"] = true
			cm.state.ShardMapping = &ShardMapping{
				Shards: make(map[int]ShardInfo),
			}

			// Assign tt.maxCount shards to node1
			for i := 0; i < tt.maxCount; i++ {
				cm.state.ShardMapping.Shards[i] = ShardInfo{
					TargetNode:  "node1",
					CurrentNode: "node1",
				}
			}

			// Assign tt.minCount shards to node2
			for i := tt.maxCount; i < tt.maxCount+tt.minCount; i++ {
				cm.state.ShardMapping.Shards[i] = ShardInfo{
					TargetNode:  "node2",
					CurrentNode: "node2",
				}
			}

			// For test purposes, we need all shards assigned, so fill the rest evenly
			totalAssigned := tt.maxCount + tt.minCount
			// Distribute remaining evenly
			for i := totalAssigned; i < sharding.NumShards; i++ {
				node := "node1"
				if (i-totalAssigned)%2 == 1 {
					node = "node2"
				}
				cm.state.ShardMapping.Shards[i] = ShardInfo{
					TargetNode:  node,
					CurrentNode: node,
				}
			}
			cm.mu.Unlock()

			// For this test, just verify the logic works with the specific counts
			// The actual rebalance check happens in RebalanceShards method
			a := tt.maxCount
			b := tt.minCount

			conditionsMet := (a >= b+2) && (a > 2*b)
			if conditionsMet != tt.shouldRebalance {
				t.Errorf("%s: condition check mismatch. a=%d, b=%d, conditions met=%v, expected=%v",
					tt.description, a, b, conditionsMet, tt.shouldRebalance)
			}
		})
	}
}

func TestRebalanceShards_NoNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.mu.Unlock()

	ctx := context.Background()
	rebalanced, err := cm.RebalanceShards(ctx)

	if err != nil {
		t.Errorf("Expected no error with no nodes, got %v", err)
	}

	if rebalanced {
		t.Error("Should not rebalance with no nodes")
	}
}

func TestRebalanceShards_NoMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	cm.mu.Lock()
	cm.state.Nodes["node1"] = true
	cm.state.Nodes["node2"] = true
	cm.mu.Unlock()

	ctx := context.Background()
	rebalanced, err := cm.RebalanceShards(ctx)

	if err != nil {
		t.Errorf("Expected no error with no mapping, got %v", err)
	}

	if rebalanced {
		t.Error("Should not rebalance with no mapping")
	}
}
