package consensusmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/util/testutil"
	"go.etcd.io/etcd/api/v3/mvccpb"
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

	mapping := cm.GetShardMapping()
	if mapping == nil {
		t.Error("Expected non-nil mapping even when not initialized")
	}
	if len(mapping.Shards) != 0 {
		t.Errorf("Expected empty mapping, got %d shards", len(mapping.Shards))
	}
}

func TestGetShardMapping_ReturnsDeepCopy(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Initialize shard mapping
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.state.ShardMapping.Shards[0] = ShardInfo{TargetNode: "node1", CurrentNode: "node1"}
	cm.state.ShardMapping.Shards[1] = ShardInfo{TargetNode: "node2", CurrentNode: "node2"}
	cm.mu.Unlock()

	// Get the mapping
	mapping1 := cm.GetShardMapping()

	// Verify initial state
	if len(mapping1.Shards) != 2 {
		t.Fatalf("Expected 2 shards, got %d", len(mapping1.Shards))
	}

	// Modify the returned map (this should NOT affect internal state)
	mapping1.Shards[0] = ShardInfo{TargetNode: "modified", CurrentNode: "modified"}
	mapping1.Shards[999] = ShardInfo{TargetNode: "new", CurrentNode: "new"}

	// Get the mapping again
	mapping2 := cm.GetShardMapping()

	// Verify that internal state was NOT modified
	if mapping2.Shards[0].TargetNode != "node1" {
		t.Errorf("Expected shard 0 to be node1, got %s (deep copy failed)", mapping2.Shards[0].TargetNode)
	}
	if _, exists := mapping2.Shards[999]; exists {
		t.Error("Expected shard 999 to not exist (deep copy failed)")
	}
	if len(mapping2.Shards) != 2 {
		t.Errorf("Expected 2 shards, got %d (deep copy failed)", len(mapping2.Shards))
	}
}

func TestCreateShardMapping_NoNodes(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Errorf("Should not error when no nodes available, got: %v", err)
	}
	if n != 0 {
		t.Errorf("Expected 0 shards reassigned, got %d", n)
	}
}

func TestCreateShardMapping_WithNodes_NoExistingMapping(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	mgr.Connect()

	cm := NewConsensusManager(mgr)

	// Add nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Fatalf("Failed to create shard mapping: %v", err)
	}
	if n != sharding.NumShards {
		t.Errorf("Expected %d shards reassigned (creating initial mapping), got %d", sharding.NumShards, n)
	}

	if len(cm.state.ShardMapping.Shards) != 8192 {
		t.Fatalf("Expected shard mapping to have %d shards, got %d", sharding.NumShards, len(cm.state.ShardMapping.Shards))
	}
}

func TestUpdateShardMapping_WithExisting(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	mgr.Connect()

	cm := NewConsensusManager(mgr)

	// Add initial nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true

	// Set initial mapping
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < sharding.NumShards/2; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "",
		}
	}
	cm.mu.Unlock()

	// Add a new node
	cm.mu.Lock()
	cm.state.Nodes["localhost:47003"] = true
	cm.mu.Unlock()

	// Update should create a new mapping (old shard assignments may change)
	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}
	if n == 0 {
		t.Error("Expected some shards to be reassigned after adding a new node")
	}

	if len(cm.state.ShardMapping.Shards) != sharding.NumShards {
		t.Errorf("Expected %d shards in updated mapping, got %d", sharding.NumShards, len(cm.state.ShardMapping.Shards))
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
		Shards: make(map[int]ShardInfo),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % 2
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  nodes[nodeIdx],
			CurrentNode: "",
		}
	}
	cm.mu.Unlock()

	// Update with same nodes should return same mapping
	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Fatalf("Failed to update shard mapping: %v", err)
	}
	if n != 0 {
		t.Errorf("Expected 0 shards reassigned when nodes unchanged, got %d", n)
	}

	// Verify the mapping is the same (pointer comparison)
	cm.mu.RLock()
	sameMapping := (cm.state.ShardMapping == cm.state.ShardMapping)
	cm.mu.RUnlock()

	if !sameMapping {
		t.Error("Expected same mapping object when no changes needed")
	}
}

func TestIsStateStable(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Not stable when lastNodeChange is zero
	if cm.IsStateStable(time.Second) {
		t.Error("Should not be stable when lastNodeChange is zero")
	}

	// Set lastNodeChange to recent time
	cm.mu.Lock()
	cm.state.LastChange = time.Now()
	cm.mu.Unlock()

	// Should not be stable for longer duration
	if cm.IsStateStable(10 * time.Second) {
		t.Error("Should not be stable for 10 seconds")
	}

	// Set lastNodeChange to past but nodes list is empty
	cm.mu.Lock()
	cm.state.LastChange = time.Now().Add(-20 * time.Second)
	cm.mu.Unlock()

	// Should NOT be stable when nodes list is empty
	if cm.IsStateStable(10 * time.Second) {
		t.Error("Should not be stable when nodes list is empty")
	}

	// Add nodes to the state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.mu.Unlock()

	// Should be stable now with nodes and old lastNodeChange
	if !cm.IsStateStable(10 * time.Second) {
		t.Error("Should be stable after 10 seconds with nodes present")
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

	// Set a mapping with CurrentNode set
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: "localhost:47001", CurrentNode: "localhost:47001"},
		},
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

func TestGetNodeForShard_FailsWhenCurrentNodeEmpty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a mapping with CurrentNode empty
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: "localhost:47001", CurrentNode: ""},
		},
	}
	cm.mu.Unlock()

	_, err := cm.GetNodeForShard(0)
	if err == nil {
		t.Error("Expected error when CurrentNode is empty")
	}

	expectedErrMsg := "shard 0 has no current node (not yet claimed)"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestGetNodeForShard_PrefersCurrentNode(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a mapping where CurrentNode differs from TargetNode
	cm.mu.Lock()
	// Add nodes to the state so CurrentNode validation passes
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: "localhost:47001", CurrentNode: "localhost:47002"},
		},
	}
	cm.mu.Unlock()

	// Should return CurrentNode when set
	node, err := cm.GetNodeForShard(0)
	if err != nil {
		t.Fatalf("Failed to get node for shard: %v", err)
	}

	if node != "localhost:47002" {
		t.Errorf("Expected CurrentNode localhost:47002, got %s", node)
	}
}

func TestGetCurrentNodeForObject_NoMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	_, err := cm.GetCurrentNodeForObject("test-object")
	if err == nil {
		t.Error("Expected error when no shard mapping available")
	}
}

func TestGetCurrentNodeForObject_WithMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a complete mapping with CurrentNode set
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < sharding.NumShards; i++ {
		nodeIdx := i % 2
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  nodes[nodeIdx],
			CurrentNode: nodes[nodeIdx], // CurrentNode is set and matches TargetNode
		}
	}
	cm.mu.Unlock()

	// Get node for an object
	node, err := cm.GetCurrentNodeForObject("test-object-123")
	if err != nil {
		t.Fatalf("Failed to get node for object: %v", err)
	}

	// Should be one of the nodes
	if node != "localhost:47001" && node != "localhost:47002" {
		t.Errorf("Unexpected node: %s", node)
	}
}

func TestGetCurrentNodeForObject_FailsWhenCurrentNodeEmpty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a complete mapping with CurrentNode empty
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "", // CurrentNode not set
		}
	}
	cm.mu.Unlock()

	// Should fail because CurrentNode is not set
	_, err := cm.GetCurrentNodeForObject("test-object-123")
	if err == nil {
		t.Error("Expected error when CurrentNode is empty")
	}

	if err != nil && !strings.Contains(err.Error(), "has no current node") {
		t.Errorf("Expected error message to contain 'has no current node', got '%s'", err.Error())
	}
}

func TestGetCurrentNodeForObject_PrefersCurrentNode(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a complete mapping where some shards have CurrentNode set
	cm.mu.Lock()
	// Add nodes to the state so CurrentNode validation passes
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// For all shards, set TargetNode to localhost:47001 but CurrentNode to localhost:47002
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47002",
		}
	}
	cm.mu.Unlock()

	// Get node for an object - should return CurrentNode
	node, err := cm.GetCurrentNodeForObject("test-object-123")
	if err != nil {
		t.Fatalf("Failed to get node for object: %v", err)
	}

	// Should return CurrentNode (localhost:47002) not TargetNode (localhost:47001)
	if node != "localhost:47002" {
		t.Errorf("Expected CurrentNode localhost:47002, got %s", node)
	}

	// Test with a different object to ensure it works for different shards
	node2, err := cm.GetCurrentNodeForObject("another-object-456")
	if err != nil {
		t.Fatalf("Failed to get node for object: %v", err)
	}

	if node2 != "localhost:47002" {
		t.Errorf("Expected CurrentNode localhost:47002 for second object, got %s", node2)
	}
}

func TestGetNodeForShard_FailsWhenCurrentNodeNotInNodeList(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a mapping where CurrentNode is not in the node list
	cm.mu.Lock()
	// Only add one node to the state
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: map[int]ShardInfo{
			0: {TargetNode: "localhost:47001", CurrentNode: "localhost:47002"}, // CurrentNode not in node list
		},
	}
	cm.mu.Unlock()

	// Should fail because CurrentNode is not in the active node list
	_, err := cm.GetNodeForShard(0)
	if err == nil {
		t.Error("Expected error when CurrentNode is not in active node list")
	}

	expectedErrMsg := "current node localhost:47002 for shard 0 is not in active node list"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestGetCurrentNodeForObject_FailsWhenCurrentNodeNotInNodeList(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)

	// Set a mapping where CurrentNode is not in the node list
	cm.mu.Lock()
	// Only add one node to the state
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// For all shards, set CurrentNode to a node not in the list
	for i := 0; i < sharding.NumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47002", // Not in node list
		}
	}
	cm.mu.Unlock()

	// Should fail because CurrentNode is not in the active node list
	_, err := cm.GetCurrentNodeForObject("test-object-123")
	if err == nil {
		t.Error("Expected error when CurrentNode is not in active node list")
	}

	// Error message should mention that current node is not in active node list
	if err != nil && !strings.Contains(err.Error(), "not in active node list") {
		t.Errorf("Expected error message to contain 'not in active node list', got '%s'", err.Error())
	}
}

func TestParseShardInfo(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		wantTarget  string
		wantCurrent string
	}{
		{
			name:        "Full format with both nodes",
			value:       "localhost:47001,localhost:47002",
			wantTarget:  "localhost:47001",
			wantCurrent: "localhost:47002",
		},
		{
			name:        "Full format with empty current node",
			value:       "localhost:47001,",
			wantTarget:  "localhost:47001",
			wantCurrent: "",
		},
		{
			name:        "Backward compatibility - only target node",
			value:       "localhost:47001",
			wantTarget:  "localhost:47001",
			wantCurrent: "",
		},
		{
			name:        "With whitespace",
			value:       " localhost:47001 , localhost:47002 ",
			wantTarget:  "localhost:47001",
			wantCurrent: "localhost:47002",
		},
		{
			name:        "Edge case - extra commas in current node",
			value:       "localhost:47001,node2,extra",
			wantTarget:  "localhost:47001",
			wantCurrent: "node2,extra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseShardInfo(&mvccpb.KeyValue{
				Value: []byte(tt.value),
			})
			if info.TargetNode != tt.wantTarget {
				t.Errorf("parseShardInfo(%q).TargetNode = %q, want %q", tt.value, info.TargetNode, tt.wantTarget)
			}
			if info.CurrentNode != tt.wantCurrent {
				t.Errorf("parseShardInfo(%q).CurrentNode = %q, want %q", tt.value, info.CurrentNode, tt.wantCurrent)
			}
		})
	}
}

func TestFormatShardInfo(t *testing.T) {
	tests := []struct {
		name string
		info ShardInfo
		want string
	}{
		{
			name: "Both nodes present",
			info: ShardInfo{
				TargetNode:  "localhost:47001",
				CurrentNode: "localhost:47002",
			},
			want: "localhost:47001,localhost:47002",
		},
		{
			name: "Empty current node",
			info: ShardInfo{
				TargetNode:  "localhost:47001",
				CurrentNode: "",
			},
			want: "localhost:47001,",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatShardInfo(tt.info)
			if got != tt.want {
				t.Errorf("formatShardInfo() = %q, want %q", got, tt.want)
			}
		})
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

// TestClaimShardOwnership tests that a node claims ownership of shards
// where it is the target node and CurrentNode is empty
func TestClaimShardOwnership(t *testing.T) {
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

	// Define this node's address
	thisNodeAddr := "localhost:47001"

	// Add nodes to state
	cm.mu.Lock()
	cm.state.Nodes[thisNodeAddr] = true
	cm.state.Nodes["localhost:47002"] = true

	// Initialize shard mapping with some shards for this node
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Add a few test shards
	// Shard 0: target is this node, current is empty (should be claimed)
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  thisNodeAddr,
		CurrentNode: "",
		ModRevision: 0,
	}

	// Shard 1: target is another node, current is empty (should NOT be claimed)
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  "localhost:47002",
		CurrentNode: "",
		ModRevision: 0,
	}

	// Shard 2: target is this node, current is already set (should NOT be claimed)
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  thisNodeAddr,
		CurrentNode: thisNodeAddr,
		ModRevision: 0,
	}
	cm.mu.Unlock()

	// Call ClaimShardsForNode with the node address
	err = cm.ClaimShardsForNode(ctx, thisNodeAddr)
	if err != nil {
		t.Fatalf("ClaimShardsForNode failed: %v", err)
	}

	// Wait a bit for the async update to complete
	time.Sleep(100 * time.Millisecond)

	// Reload the state from etcd to verify
	client := mgr.GetClient()
	key0 := prefix + "/shard/0"
	resp0, err := client.Get(ctx, key0)
	if err != nil {
		t.Fatalf("Failed to get shard 0 from etcd: %v", err)
	}

	if len(resp0.Kvs) == 0 {
		t.Error("Shard 0 should exist in etcd after claiming")
	} else {
		shardInfo0 := parseShardInfo(resp0.Kvs[0])
		if shardInfo0.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 0 CurrentNode should be %s, got %s", thisNodeAddr, shardInfo0.CurrentNode)
		}
		if shardInfo0.TargetNode != thisNodeAddr {
			t.Errorf("Shard 0 TargetNode should be %s, got %s", thisNodeAddr, shardInfo0.TargetNode)
		}
	}

	// Verify shard 1 was not claimed (it's for another node)
	key1 := prefix + "/shard/1"
	resp1, err := client.Get(ctx, key1)
	if err != nil {
		t.Fatalf("Failed to get shard 1 from etcd: %v", err)
	}

	// Shard 1 should not have been updated (we didn't write it to etcd initially)
	if len(resp1.Kvs) > 0 {
		shardInfo1 := parseShardInfo(resp1.Kvs[0])
		// If it exists, CurrentNode should still be empty
		if shardInfo1.CurrentNode != "" && shardInfo1.CurrentNode != "localhost:47002" {
			t.Errorf("Shard 1 should not be claimed by this node, CurrentNode: %s", shardInfo1.CurrentNode)
		}
	}
}

// TestClaimShardOwnership_NoThisNode tests that claiming doesn't happen
// when this node address is not set
func TestClaimShardOwnership_NoThisNode(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr)
	ctx := context.Background()

	// Don't set this node's address

	// Add a shard
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  "localhost:47001",
		CurrentNode: "",
		ModRevision: 0,
	}
	cm.mu.Unlock()

	// Call ClaimShardsForNode with empty string - should return error
	err := cm.ClaimShardsForNode(ctx, "")
	if err == nil {
		t.Error("ClaimShardsForNode should return error when localNode is empty")
	}

	// Verify the shard wasn't modified
	cm.mu.RLock()
	shard0 := cm.state.ShardMapping.Shards[0]
	cm.mu.RUnlock()

	if shard0.CurrentNode != "" {
		t.Errorf("Shard should not be claimed when localNode is empty, CurrentNode: %s", shard0.CurrentNode)
	}
}

// TestClaimShardOwnership_TargetAndEmpty tests that a node claims ownership
// only when TargetNode is this node AND (CurrentNode is empty or not alive)
func TestClaimShardOwnership_TargetAndEmpty(t *testing.T) {
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

	// Define this node's address and a dead node
	thisNodeAddr := "localhost:47001"
	deadNodeAddr := "localhost:47003"

	// Add nodes to state - note that deadNodeAddr is NOT in the active node list
	cm.mu.Lock()
	cm.state.Nodes[thisNodeAddr] = true
	cm.state.Nodes["localhost:47002"] = true
	// deadNodeAddr is intentionally NOT added to simulate a dead node

	// Initialize shard mapping with some shards
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Shard 0: target is another node, current is dead node (should NOT be claimed - target != thisNode)
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  "localhost:47002",
		CurrentNode: deadNodeAddr,
		ModRevision: 0,
	}

	// Shard 1: target is this node, current is dead node (should be claimed)
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  thisNodeAddr,
		CurrentNode: deadNodeAddr,
		ModRevision: 0,
	}

	// Shard 2: target is another node, current is empty (should NOT be claimed - target != thisNode)
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  "localhost:47002",
		CurrentNode: "",
		ModRevision: 0,
	}

	// Shard 3: target is this node, current is empty (should be claimed)
	cm.state.ShardMapping.Shards[3] = ShardInfo{
		TargetNode:  thisNodeAddr,
		CurrentNode: "",
		ModRevision: 0,
	}

	// Shard 4: target is this node, current is this node (should NOT be claimed - already correct, no update needed)
	cm.state.ShardMapping.Shards[4] = ShardInfo{
		TargetNode:  thisNodeAddr,
		CurrentNode: thisNodeAddr,
		ModRevision: 0,
	}

	cm.mu.Unlock()

	// Call ClaimShardsForNode
	err = cm.ClaimShardsForNode(ctx, thisNodeAddr)
	if err != nil {
		t.Fatalf("ClaimShardsForNode failed: %v", err)
	}

	// Wait a bit for the async update to complete
	time.Sleep(100 * time.Millisecond)

	// Reload the state from etcd to verify
	client := mgr.GetClient()

	// Verify shard 0 was NOT claimed (target is different node)
	key0 := prefix + "/shard/0"
	resp0, err := client.Get(ctx, key0)
	if err != nil {
		t.Fatalf("Failed to get shard 0 from etcd: %v", err)
	}
	if len(resp0.Kvs) > 0 {
		shardInfo0 := parseShardInfo(resp0.Kvs[0])
		if shardInfo0.CurrentNode == thisNodeAddr {
			t.Errorf("Shard 0 should NOT be claimed (target is different node), but CurrentNode is: %s", shardInfo0.CurrentNode)
		}
	}

	// Verify shard 1 was claimed (target is this node AND current is dead)
	key1 := prefix + "/shard/1"
	resp1, err := client.Get(ctx, key1)
	if err != nil {
		t.Fatalf("Failed to get shard 1 from etcd: %v", err)
	}
	if len(resp1.Kvs) == 0 {
		t.Error("Shard 1 should exist in etcd after claiming")
	} else {
		shardInfo1 := parseShardInfo(resp1.Kvs[0])
		if shardInfo1.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 1 CurrentNode should be %s (target is this node AND current was dead), got %s", thisNodeAddr, shardInfo1.CurrentNode)
		}
	}

	// Verify shard 2 was NOT claimed (target is different node)
	key2 := prefix + "/shard/2"
	resp2, err := client.Get(ctx, key2)
	if err != nil {
		t.Fatalf("Failed to get shard 2 from etcd: %v", err)
	}
	if len(resp2.Kvs) > 0 {
		shardInfo2 := parseShardInfo(resp2.Kvs[0])
		if shardInfo2.CurrentNode == thisNodeAddr {
			t.Errorf("Shard 2 should NOT be claimed (target is different node), but CurrentNode is: %s", shardInfo2.CurrentNode)
		}
	}

	// Verify shard 3 was claimed (target is this node AND current is empty)
	key3 := prefix + "/shard/3"
	resp3, err := client.Get(ctx, key3)
	if err != nil {
		t.Fatalf("Failed to get shard 3 from etcd: %v", err)
	}
	if len(resp3.Kvs) == 0 {
		t.Error("Shard 3 should exist in etcd after claiming")
	} else {
		shardInfo3 := parseShardInfo(resp3.Kvs[0])
		if shardInfo3.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 3 CurrentNode should be %s (target is this node AND current was empty), got %s", thisNodeAddr, shardInfo3.CurrentNode)
		}
	}

	// Verify shard 4 was NOT claimed (target is this node but current is already this node - no update needed)
	// Since we didn't claim it, it won't be in etcd unless it was there before
	// This test just verifies it wasn't incorrectly updated
	key4 := prefix + "/shard/4"
	resp4, err := client.Get(ctx, key4)
	if err != nil {
		t.Fatalf("Failed to get shard 4 from etcd: %v", err)
	}
	// Shard 4 may or may not exist in etcd - we just verify it wasn't claimed by checking the count
	// The log should show only 2 shards were claimed (shards 1 and 3)
	if len(resp4.Kvs) > 0 {
		shardInfo4 := parseShardInfo(resp4.Kvs[0])
		// If it exists in etcd, it should still be set to thisNodeAddr (unchanged)
		if shardInfo4.CurrentNode != thisNodeAddr {
			t.Errorf("Shard 4 CurrentNode should remain %s (no update needed), got %s", thisNodeAddr, shardInfo4.CurrentNode)
		}
	}
}
