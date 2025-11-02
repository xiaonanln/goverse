package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
)

// mockListener implements StateChangeListener for testing
type mockListener struct {
	stateChangedCount int
}

func (m *mockListener) OnClusterStateChanged() {
	m.stateChangedCount++
}

func TestNewConsensusManager(t *testing.T) {
	// Error is intentionally ignored as we're only testing ConsensusManager creation,
	// not etcd manager functionality
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	if cm == nil {
		t.Fatal("NewConsensusManager returned nil")
	}

	if cm.etcdManager != mgr {
		t.Error("ConsensusManager should have the correct etcd manager")
	}

	if cm.logger == nil {
		t.Error("ConsensusManager should have a logger")
	}

	if cm.state.Nodes == nil {
		t.Error("ConsensusManager should have initialized nodes map")
	}
}

func TestAddRemoveListener(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	listener1 := &mockListener{}
	listener2 := &mockListener{}

	// Add listeners
	cm.AddListener(listener1)
	cm.AddListener(listener2)

	if len(cm.listeners) != 2 {
		t.Errorf("Expected 2 listeners, got %d", len(cm.listeners))
	}

	// Remove listener
	cm.RemoveListener(listener1)

	if len(cm.listeners) != 1 {
		t.Errorf("Expected 1 listener after removal, got %d", len(cm.listeners))
	}

	if cm.listeners[0] != listener2 {
		t.Error("Wrong listener was removed")
	}
}

func TestGetNodes_Empty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	nodes := cm.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected empty node list, got %d nodes", len(nodes))
	}
}

func TestGetLeaderNode_Empty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	leader := cm.GetLeaderNode()
	if leader != "" {
		t.Errorf("Expected empty leader, got %s", leader)
	}
}

func TestGetLeaderNode_WithNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47003"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	leader := cm.GetLeaderNode()
	if leader != "localhost:47001" {
		t.Errorf("Expected leader localhost:47001, got %s", leader)
	}
}

func TestGetShardMapping_NotAvailable(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.GetShardMapping()
	if err == nil {
		t.Error("Expected error when shard mapping not available")
	}
}

func TestCreateShardMapping_NoNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.CreateShardMapping()
	if err == nil {
		t.Error("Expected error when creating shard mapping with no nodes")
	}
}

func TestCreateShardMapping_WithNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	mapping, err := cm.CreateShardMapping()
	if err != nil {
		t.Fatalf("Failed to create shard mapping: %v", err)
	}

	if mapping == nil {
		t.Fatal("Mapping should not be nil")
	}

	// Note: Version is now tracked in ClusterState, not ShardMapping

	if len(mapping.Shards) != sharding.NumShards {
		t.Errorf("Expected %d shards, got %d", sharding.NumShards, len(mapping.Shards))
	}
}

func TestUpdateShardMapping_NoExisting(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	// Update should create new mapping
	_, err := cm.UpdateShardMapping()
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}

	// Note: Version is now tracked in ClusterState, not ShardMapping
}

func TestUpdateShardMapping_WithExisting(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add initial nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true

	// Set initial mapping
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]string),
	}
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = "localhost:47001"
	}
	cm.mu.Unlock()

	// Add a new node
	cm.mu.Lock()
	cm.state.Nodes["localhost:47003"] = true
	cm.mu.Unlock()

	// Update should create a new mapping (old shard assignments may change)
	mapping, err := cm.UpdateShardMapping()
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}

	// Note: Version tracking happens in ClusterState when StoreShardMapping is called
	// Just verify the mapping is valid
	if len(mapping.Shards) != sharding.NumShards {
		t.Errorf("Expected %d shards in updated mapping, got %d", sharding.NumShards, len(mapping.Shards))
	}
}

func TestUpdateShardMapping_NoChanges(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Add nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true

	// Set mapping with same nodes
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]string),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % 2
		cm.state.ShardMapping.Shards[i] = nodes[nodeIdx]
	}
	cm.mu.Unlock()

	// Update with same nodes should return same mapping
	mapping, err := cm.UpdateShardMapping()
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}

	// Verify the mapping is the same (pointer comparison)
	cm.mu.RLock()
	sameMapping := (mapping == cm.state.ShardMapping)
	cm.mu.RUnlock()

	if !sameMapping {
		t.Error("Expected same mapping object when no changes needed")
	}
}

func TestIsNodeListStable(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Not stable when lastNodeChange is zero
	if cm.IsNodeListStable(time.Second) {
		t.Error("Should not be stable when lastNodeChange is zero")
	}

	// Set lastNodeChange to recent time
	cm.mu.Lock()
	cm.state.LastChange = time.Now()
	cm.mu.Unlock()

	// Should not be stable for longer duration
	if cm.IsNodeListStable(10 * time.Second) {
		t.Error("Should not be stable for 10 seconds")
	}

	// Set lastNodeChange to past
	cm.mu.Lock()
	cm.state.LastChange = time.Now().Add(-20 * time.Second)
	cm.mu.Unlock()

	// Should be stable now
	if !cm.IsNodeListStable(10 * time.Second) {
		t.Error("Should be stable after 10 seconds")
	}
}

func TestGetLastNodeChangeTime(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Initial state
	changeTime := cm.GetLastNodeChangeTime()
	if !changeTime.IsZero() {
		t.Error("Initial change time should be zero")
	}

	// Set a change time
	testTime := time.Now()
	cm.mu.Lock()
	cm.state.LastChange = testTime
	cm.mu.Unlock()

	changeTime = cm.GetLastNodeChangeTime()
	if !changeTime.Equal(testTime) {
		t.Error("Should return the set change time")
	}
}

func TestGetNodeForShard_InvalidShard(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.GetNodeForShard(-1)
	if err == nil {
		t.Error("Expected error for negative shard ID")
	}

	_, err = cm.GetNodeForShard(sharding.NumShards)
	if err == nil {
		t.Error("Expected error for shard ID >= NumShards")
	}
}

func TestGetNodeForShard_NoMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.GetNodeForShard(0)
	if err == nil {
		t.Error("Expected error when no shard mapping available")
	}
}

func TestGetNodeForShard_WithMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a mapping
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: map[int]string{0: "localhost:47001", 1: "localhost:47002"},
	}
	cm.mu.Unlock()

	node, err := cm.GetNodeForShard(0)
	if err != nil {
		t.Fatalf("Failed to get node for shard: %v", err)
	}

	if node != "localhost:47001" {
		t.Errorf("Expected localhost:47001, got %s", node)
	}
}

func TestGetNodeForObject_NoMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.GetNodeForObject("test-object")
	if err == nil {
		t.Error("Expected error when no shard mapping available")
	}
}

func TestGetNodeForObject_WithMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a complete mapping
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]string),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % 2
		cm.state.ShardMapping.Shards[i] = nodes[nodeIdx]
	}
	cm.mu.Unlock()

	// Get node for an object
	node, err := cm.GetNodeForObject("test-object-123")
	if err != nil {
		t.Fatalf("Failed to get node for object: %v", err)
	}

	// Should be one of the nodes
	if node != "localhost:47001" && node != "localhost:47002" {
		t.Errorf("Unexpected node: %s", node)
	}
}

func TestStopWatch_NotStarted(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Should not panic
	cm.StopWatch()
}

func TestStartWatch_NoEtcdManager(t *testing.T) {
	cm := NewConsensusManager(nil)

	ctx := context.Background()
	err := cm.StartWatch(ctx)
	if err == nil {
		t.Error("Expected error when etcd manager not set")
	}
}
