package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
"github.com/xiaonanln/goverse/cluster/shardlock"
)

// TestGetClusterState_Empty tests GetClusterState with empty state
func TestGetClusterState_Empty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Call GetClusterState - should return a cloned state
	state := cm.GetClusterState()
	
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}
	
	if state.Nodes == nil {
		t.Error("Cloned state should have initialized nodes map")
	}
	
	if state.Nodes.len() != 0 {
		t.Errorf("Expected empty nodes in cloned state, got %d", state.Nodes.len())
	}
}

// TestGetClusterState_WithData tests GetClusterState with populated state
func TestGetClusterState_WithData(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes.set("localhost:47001", true)
	cm.state.Nodes.set("localhost:47002", true)
	cm.state.ShardMapping.Shards.set(0, ShardInfo{
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
	})
	cm.state.Revision = 123
	cm.mu.Unlock()

	// Call GetClusterState - should return a cloned state
	state := cm.GetClusterState()
	
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}
	
	if state.Nodes.len() != 2 {
		t.Errorf("Expected 2 nodes in cloned state, got %d", state.Nodes.len())
	}
	
	if !state.HasNode("localhost:47001") || !state.HasNode("localhost:47002") {
		t.Error("Cloned state should contain the correct nodes")
	}
	
	if state.Revision != 123 {
		t.Errorf("Expected revision 123, got %d", state.Revision)
	}
	
	if state.ShardMapping == nil {
		t.Fatal("Cloned state should have shard mapping")
	}
	
	if state.ShardMapping.Shards.len() != 1 {
		t.Errorf("Expected 1 shard in cloned state, got %d", state.ShardMapping.Shards.len())
	}
}

// TestGetClusterState_IsIndependent tests that the cloned state is independent
func TestGetClusterState_IsIndependent(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes.set("localhost:47001", true)
	cm.mu.Unlock()

	// Get cloned state
	state1 := cm.GetClusterState()
	
	// Modify the cloned state
	state1.AddNode("localhost:47002")
	
	// Get another cloned state
	state2 := cm.GetClusterState()
	
	// Verify state2 doesn't have the modification from state1
	if state2.HasNode("localhost:47002") {
		t.Error("Cloned states should be independent - modification in one shouldn't affect others")
	}
	
	// Verify state2 has the original node
	if !state2.HasNode("localhost:47001") {
		t.Error("Cloned state should have the original node")
	}
}

// TestGetClusterState_FullShardMapping tests GetClusterState with full 8192 shard mapping
func TestGetClusterState_FullShardMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock())

	// Add nodes to internal state
	cm.mu.Lock()
	nodeAddrs := []string{
		"localhost:47001",
		"localhost:47002",
		"localhost:47003",
		"localhost:47004",
		"localhost:47005",
	}
	for _, addr := range nodeAddrs {
		cm.state.Nodes.set(addr, true)
	}

	// Populate all shards with round-robin assignment to nodes
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % len(nodeAddrs)
		cm.state.ShardMapping.Shards.set(i, ShardInfo{
			TargetNode:  nodeAddrs[nodeIdx],
			CurrentNode: nodeAddrs[nodeIdx],
		})
	}
	cm.state.Revision = 456
	cm.mu.Unlock()

	// Call GetClusterState - should return a cloned state with all shards
	state := cm.GetClusterState()

	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}

	if state.Nodes.len() != len(nodeAddrs) {
		t.Errorf("Expected %d nodes in cloned state, got %d", len(nodeAddrs), state.Nodes.len())
	}

	if state.ShardMapping == nil {
		t.Fatal("Cloned state should have shard mapping")
	}

	if state.ShardMapping.Shards.len() != sharding.NumShards {
		t.Errorf("Expected %d shards in cloned state, got %d", sharding.NumShards, state.ShardMapping.Shards.len())
	}

	if state.Revision != 456 {
		t.Errorf("Expected revision 456, got %d", state.Revision)
	}

	// Verify a few sample shards are correctly cloned
	for i := 0; i < 10; i++ {
		shard, exists := state.ShardMapping.Shards.get(i)
		if !exists {
			t.Errorf("Shard %d should exist in cloned state", i)
			continue
		}
		expectedNode := nodeAddrs[i%len(nodeAddrs)]
		if shard.TargetNode != expectedNode {
			t.Errorf("Shard %d: expected target node %s, got %s", i, expectedNode, shard.TargetNode)
		}
		if shard.CurrentNode != expectedNode {
			t.Errorf("Shard %d: expected current node %s, got %s", i, expectedNode, shard.CurrentNode)
		}
	}

	// Verify the clone is independent by modifying it
	state.SetShard(0, ShardInfo{
		TargetNode:  "localhost:47099",
		CurrentNode: "localhost:47099",
	})

	// Get another clone and verify it wasn't affected
	state2 := cm.GetClusterState()
	shard0, _ := state2.ShardMapping.Shards.get(0)
	if shard0.TargetNode == "localhost:47099" {
		t.Error("Cloned states should be independent - modification in one shouldn't affect others")
	}

	t.Logf("Successfully cloned state with %d nodes and %d shards", state.Nodes.len(), state.ShardMapping.Shards.len())
}
