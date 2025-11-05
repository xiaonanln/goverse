package consensusmanager

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestUpdateShardMapping_NodeRemoval tests that when a node is removed from the cluster:
// - TargetNode should be set to a different node
// - CurrentNode should be set to empty if it's also the stopped node
// - CurrentNode should not be set to empty if it's not the stopped node
func TestUpdateShardMapping_NodeRemoval(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
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
	ctx := context.Background()

	// Define three nodes
	node1 := "localhost:47001"
	node2 := "localhost:47002"
	node3 := "localhost:47003"

	// Add all three nodes to initial state
	cm.mu.Lock()
	cm.state.Nodes[node1] = true
	cm.state.Nodes[node2] = true
	cm.state.Nodes[node3] = true

	// Set initial mapping with all three nodes
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Create test scenarios:
	// Shard 0: TargetNode=node1, CurrentNode=node1 (both removed)
	// Shard 1: TargetNode=node1, CurrentNode=node2 (target removed, current stays)
	// Shard 2: TargetNode=node2, CurrentNode=node1 (current removed)
	// Shard 3: TargetNode=node2, CurrentNode=node2 (neither removed)
	// Shards 4+: TargetNode round-robin among all nodes, CurrentNode empty or set

	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  node1,
		CurrentNode: node1,
		ModRevision: 0,
	}
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  node1,
		CurrentNode: node2,
		ModRevision: 0,
	}
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  node2,
		CurrentNode: node1,
		ModRevision: 0,
	}
	cm.state.ShardMapping.Shards[3] = ShardInfo{
		TargetNode:  node2,
		CurrentNode: node2,
		ModRevision: 0,
	}

	// Fill remaining shards with round-robin assignment
	nodes := []string{node1, node2, node3}
	for i := 4; i < sharding.NumShards; i++ {
		nodeIdx := i % 3
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  nodes[nodeIdx],
			CurrentNode: "",
			ModRevision: 0,
		}
	}
	cm.mu.Unlock()

	// Now remove node1 from cluster state
	cm.mu.Lock()
	delete(cm.state.Nodes, node1)
	cm.mu.Unlock()

	// Call UpdateShardMapping
	err = cm.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("UpdateShardMapping failed: %v", err)
	}

	// Wait briefly for async updates
	// Note: storeShardMapping writes to etcd but doesn't update in-memory state
	// We need to reload from etcd to verify
	client := mgr.GetClient()

	// Verify Shard 0: TargetNode changed, CurrentNode cleared (both were node1)
	shard0Key := prefix + "/shard/0"
	resp0, err := client.Get(ctx, shard0Key)
	if err != nil {
		t.Fatalf("Failed to get shard 0: %v", err)
	}
	if len(resp0.Kvs) == 0 {
		t.Fatal("Shard 0 not found in etcd")
	}
	shard0Info := parseShardInfo(resp0.Kvs[0])
	if shard0Info.TargetNode == node1 {
		t.Errorf("Shard 0 TargetNode should have changed from %s, still %s", node1, shard0Info.TargetNode)
	}
	if shard0Info.CurrentNode != "" {
		t.Errorf("Shard 0 CurrentNode should be empty (was %s which is removed), got %s", node1, shard0Info.CurrentNode)
	}

	// Verify Shard 1: TargetNode changed, CurrentNode NOT cleared (current is node2)
	shard1Key := prefix + "/shard/1"
	resp1, err := client.Get(ctx, shard1Key)
	if err != nil {
		t.Fatalf("Failed to get shard 1: %v", err)
	}
	if len(resp1.Kvs) == 0 {
		t.Fatal("Shard 1 not found in etcd")
	}
	shard1Info := parseShardInfo(resp1.Kvs[0])
	if shard1Info.TargetNode == node1 {
		t.Errorf("Shard 1 TargetNode should have changed from %s, still %s", node1, shard1Info.TargetNode)
	}
	if shard1Info.CurrentNode != node2 {
		t.Errorf("Shard 1 CurrentNode should still be %s (not removed), got %s", node2, shard1Info.CurrentNode)
	}

	// Verify Shard 2: TargetNode unchanged, CurrentNode cleared (current was node1)
	shard2Key := prefix + "/shard/2"
	resp2, err := client.Get(ctx, shard2Key)
	if err != nil {
		t.Fatalf("Failed to get shard 2: %v", err)
	}
	if len(resp2.Kvs) == 0 {
		t.Fatal("Shard 2 not found in etcd")
	}
	shard2Info := parseShardInfo(resp2.Kvs[0])
	if shard2Info.TargetNode != node2 {
		t.Errorf("Shard 2 TargetNode should still be %s, got %s", node2, shard2Info.TargetNode)
	}
	if shard2Info.CurrentNode != "" {
		t.Errorf("Shard 2 CurrentNode should be empty (was %s which is removed), got %s", node1, shard2Info.CurrentNode)
	}

	// Verify Shard 3: TargetNode unchanged, CurrentNode unchanged (neither removed)
	shard3Key := prefix + "/shard/3"
	resp3, err := client.Get(ctx, shard3Key)
	if err != nil {
		t.Fatalf("Failed to get shard 3: %v", err)
	}
	// Shard 3 should not be in updateShards map since nothing changed
	// So it might not exist in etcd if it wasn't written initially
	if len(resp3.Kvs) > 0 {
		shard3Info := parseShardInfo(resp3.Kvs[0])
		if shard3Info.TargetNode != node2 {
			t.Errorf("Shard 3 TargetNode should still be %s, got %s", node2, shard3Info.TargetNode)
		}
		if shard3Info.CurrentNode != node2 {
			t.Errorf("Shard 3 CurrentNode should still be %s, got %s", node2, shard3Info.CurrentNode)
		}
	}

	t.Logf("Test completed - verified node removal shard mapping updates")
}

// TestUpdateShardMapping_NodeRemoval_Integration is a comprehensive integration test
// that simulates a real scenario where a node is removed from the cluster
func TestUpdateShardMapping_NodeRemoval_Integration(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
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
	ctx := context.Background()

	// Start with 3 nodes
	node1 := "localhost:47001"
	node2 := "localhost:47002"
	node3 := "localhost:47003"

	cm.mu.Lock()
	cm.state.Nodes[node1] = true
	cm.state.Nodes[node2] = true
	cm.state.Nodes[node3] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.mu.Unlock()

	// Create initial shard mapping
	err = cm.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("Initial UpdateShardMapping failed: %v", err)
	}

	// Reload shard mapping from etcd to get the initial mapping
	state, err := cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		t.Fatalf("Failed to load initial state: %v", err)
	}

	// Count shards assigned to each node
	shardCountsBefore := make(map[string]int)
	shardsWithNode1AsTarget := 0
	for _, info := range state.ShardMapping.Shards {
		shardCountsBefore[info.TargetNode]++
		if info.TargetNode == node1 {
			shardsWithNode1AsTarget++
		}
	}

	t.Logf("Initial shard distribution: node1=%d, node2=%d, node3=%d",
		shardCountsBefore[node1], shardCountsBefore[node2], shardCountsBefore[node3])

	if shardsWithNode1AsTarget == 0 {
		t.Fatal("Expected some shards to be assigned to node1 initially")
	}

	// Update in-memory state with loaded shard mapping, but keep the nodes
	cm.mu.Lock()
	cm.state.ShardMapping = state.ShardMapping
	cm.mu.Unlock()

	// Now simulate node1 claiming some shards (set CurrentNode)
	err = cm.ClaimShardsForNode(ctx, node1)
	if err != nil {
		t.Fatalf("ClaimShardsForNode failed: %v", err)
	}

	// Reload shard mapping to see claimed shards
	state, err = cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		t.Fatalf("Failed to load state after claiming: %v", err)
	}

	shardsClaimedByNode1 := 0
	for _, info := range state.ShardMapping.Shards {
		if info.CurrentNode == node1 {
			shardsClaimedByNode1++
		}
	}

	t.Logf("Shards claimed by node1: %d", shardsClaimedByNode1)

	if shardsClaimedByNode1 == 0 {
		t.Fatal("Expected node1 to claim some shards")
	}

	// Update in-memory state with claimed shards, but keep the nodes
	cm.mu.Lock()
	cm.state.ShardMapping = state.ShardMapping
	cm.mu.Unlock()

	// Now remove node1 from cluster
	cm.mu.Lock()
	delete(cm.state.Nodes, node1)
	cm.mu.Unlock()

	t.Logf("Removing node1 from cluster")

	// Update shard mapping
	err = cm.UpdateShardMapping(ctx)
	if err != nil {
		t.Fatalf("UpdateShardMapping after node removal failed: %v", err)
	}

	// Reload state to verify changes
	state, err = cm.loadClusterStateFromEtcd(ctx)
	if err != nil {
		t.Fatalf("Failed to load state after node removal: %v", err)
	}

	// Verify all shards:
	// 1. No shard should have node1 as TargetNode
	// 2. No shard should have node1 as CurrentNode
	// 3. All shards should be distributed between node2 and node3
	shardCountsAfter := make(map[string]int)
	shardsWithNode1AsTargetAfter := 0
	shardsWithNode1AsCurrentAfter := 0

	for shardID, info := range state.ShardMapping.Shards {
		if info.TargetNode == node1 {
			shardsWithNode1AsTargetAfter++
			t.Errorf("Shard %d still has removed node1 as TargetNode", shardID)
		}
		if info.CurrentNode == node1 {
			shardsWithNode1AsCurrentAfter++
			t.Errorf("Shard %d still has removed node1 as CurrentNode", shardID)
		}
		shardCountsAfter[info.TargetNode]++
	}

	t.Logf("Shard distribution after removal: node2=%d, node3=%d",
		shardCountsAfter[node2], shardCountsAfter[node3])

	if shardsWithNode1AsTargetAfter > 0 {
		t.Errorf("Found %d shards still assigned to removed node1 as TargetNode", shardsWithNode1AsTargetAfter)
	}

	if shardsWithNode1AsCurrentAfter > 0 {
		t.Errorf("Found %d shards still assigned to removed node1 as CurrentNode", shardsWithNode1AsCurrentAfter)
	}

	// Verify shards are now distributed only between node2 and node3
	totalShardsAfter := shardCountsAfter[node2] + shardCountsAfter[node3]
	if totalShardsAfter != sharding.NumShards {
		t.Errorf("Expected %d total shards, got %d", sharding.NumShards, totalShardsAfter)
	}

	t.Logf("Test completed - verified comprehensive node removal behavior")
}
