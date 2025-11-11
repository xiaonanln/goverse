package consensusmanager

import (
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
)

// TestGetClusterState_Empty tests GetClusterState with empty state
func TestGetClusterState_Empty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Call GetClusterState - should return a cloned state
	state := cm.GetClusterState()
	
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}
	
	if state.Nodes == nil {
		t.Error("Cloned state should have initialized nodes map")
	}
	
	if len(state.Nodes) != 0 {
		t.Errorf("Expected empty nodes in cloned state, got %d", len(state.Nodes))
	}
}

// TestGetClusterState_WithData tests GetClusterState with populated state
func TestGetClusterState_WithData(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

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

	// Call GetClusterState - should return a cloned state
	state := cm.GetClusterState()
	
	if state == nil {
		t.Fatal("GetClusterState returned nil")
	}
	
	if len(state.Nodes) != 2 {
		t.Errorf("Expected 2 nodes in cloned state, got %d", len(state.Nodes))
	}
	
	if !state.Nodes["localhost:47001"] || !state.Nodes["localhost:47002"] {
		t.Error("Cloned state should contain the correct nodes")
	}
	
	if state.Revision != 123 {
		t.Errorf("Expected revision 123, got %d", state.Revision)
	}
	
	if state.ShardMapping == nil {
		t.Fatal("Cloned state should have shard mapping")
	}
	
	if len(state.ShardMapping.Shards) != 1 {
		t.Errorf("Expected 1 shard in cloned state, got %d", len(state.ShardMapping.Shards))
	}
}

// TestGetClusterState_IsIndependent tests that the cloned state is independent
func TestGetClusterState_IsIndependent(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.mu.Unlock()

	// Get cloned state
	state1 := cm.GetClusterState()
	
	// Modify the cloned state
	state1.Nodes["localhost:47002"] = true
	
	// Get another cloned state
	state2 := cm.GetClusterState()
	
	// Verify state2 doesn't have the modification from state1
	if _, exists := state2.Nodes["localhost:47002"]; exists {
		t.Error("Cloned states should be independent - modification in one shouldn't affect others")
	}
	
	// Verify state2 has the original node
	if !state2.Nodes["localhost:47001"] {
		t.Error("Cloned state should have the original node")
	}
}
