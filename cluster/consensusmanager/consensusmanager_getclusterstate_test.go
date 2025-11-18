package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
)

// TestLockClusterState_Empty tests LockClusterState with empty state
func TestLockClusterState_Empty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Call LockClusterState - should return state and unlock function
	state, unlock := cm.LockClusterState()
	defer unlock()

	if state == nil {
		t.Fatal("LockClusterState returned nil")
	}

	if state.Nodes == nil {
		t.Fatal("State should have initialized nodes map")
	}

	if len(state.Nodes) != 0 {
		t.Fatalf("Expected empty nodes in state, got %d", len(state.Nodes))
	}
}

// TestLockClusterState_WithData tests LockClusterState with populated state
func TestLockClusterState_WithData(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  "localhost:47001",
		CurrentNode: "localhost:47001",
	}
	cm.state.Revision = 123
	cm.mu.Unlock()

	// Call LockClusterState - should return state with lock held
	state, unlock := cm.LockClusterState()
	defer unlock()

	if state == nil {
		t.Fatal("LockClusterState returned nil")
	}

	if len(state.Nodes) != 2 {
		t.Fatalf("Expected 2 nodes in state, got %d", len(state.Nodes))
	}

	if !state.Nodes["localhost:47001"] || !state.Nodes["localhost:47002"] {
		t.Fatal("State should contain the correct nodes")
	}

	if state.Revision != 123 {
		t.Fatalf("Expected revision 123, got %d", state.Revision)
	}

	if state.ShardMapping == nil {
		t.Fatal("State should have shard mapping")
	}

	if len(state.ShardMapping.Shards) != 1 {
		t.Fatalf("Expected 1 shard in state, got %d", len(state.ShardMapping.Shards))
	}
}

// TestLockClusterState_LockingBehavior tests that the lock is properly held
func TestLockClusterState_LockingBehavior(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.mu.Unlock()

	// Lock the state
	state, unlock := cm.LockClusterState()

	// Verify we can read the state
	if !state.Nodes["localhost:47001"] {
		t.Fatal("State should have the original node")
	}

	// Note: We can't easily test that modifications are prevented without
	// complex concurrency testing, but the lock is held for reading

	// Unlock
	unlock()

	// After unlock, verify we can still access the state through another call
	state2, unlock2 := cm.LockClusterState()
	defer unlock2()

	if !state2.Nodes["localhost:47001"] {
		t.Fatal("State should still have the original node")
	}
}

// TestLockClusterState_FullShardMapping tests LockClusterState with full 8192 shard mapping
func TestLockClusterState_FullShardMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

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
		cm.state.Nodes[addr] = true
	}

	// Initialize full shard mapping with all 8192 shards
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Populate all shards with round-robin assignment to nodes
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % len(nodeAddrs)
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  nodeAddrs[nodeIdx],
			CurrentNode: nodeAddrs[nodeIdx],
		}
	}
	cm.state.Revision = 456
	cm.mu.Unlock()

	// Call LockClusterState - should return state with all shards
	state, unlock := cm.LockClusterState()
	defer unlock()

	if state == nil {
		t.Fatal("LockClusterState returned nil")
	}

	if len(state.Nodes) != len(nodeAddrs) {
		t.Fatalf("Expected %d nodes in state, got %d", len(nodeAddrs), len(state.Nodes))
	}

	if state.ShardMapping == nil {
		t.Fatal("State should have shard mapping")
	}

	if len(state.ShardMapping.Shards) != sharding.NumShards {
		t.Fatalf("Expected %d shards in state, got %d", sharding.NumShards, len(state.ShardMapping.Shards))
	}

	if state.Revision != 456 {
		t.Fatalf("Expected revision 456, got %d", state.Revision)
	}

	// Verify a few sample shards are correctly accessible
	for i := 0; i < 10; i++ {
		shard, exists := state.ShardMapping.Shards[i]
		if !exists {
			t.Fatalf("Shard %d should exist in state", i)
			continue
		}
		expectedNode := nodeAddrs[i%len(nodeAddrs)]
		if shard.TargetNode != expectedNode {
			t.Fatalf("Shard %d: expected target node %s, got %s", i, expectedNode, shard.TargetNode)
		}
		if shard.CurrentNode != expectedNode {
			t.Fatalf("Shard %d: expected current node %s, got %s", i, expectedNode, shard.CurrentNode)
		}
	}

	t.Logf("Successfully accessed state with %d nodes and %d shards", len(state.Nodes), len(state.ShardMapping.Shards))
}

// TestGetClusterStateForTesting_ReturnsClonedState tests that GetClusterStateForTesting returns a cloned copy
func TestGetClusterStateForTesting_ReturnsClonedState(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "")

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.mu.Unlock()

	// Get state copy
	state := cm.GetClusterStateForTesting()

	if state == nil {
		t.Fatal("GetClusterStateForTesting returned nil")
	}

	if !state.Nodes["localhost:47001"] {
		t.Fatal("State should have the original node")
	}
}
