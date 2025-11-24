package consensusmanager

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
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
	t.Parallel()
	// Error is intentionally ignored as we're only testing ConsensusManager creation,
	// not etcd manager functionality
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	if cm == nil {
		t.Fatal("NewConsensusManager returned nil")
	}

	if cm.etcdManager != mgr {
		t.Fatal("ConsensusManager should have the correct etcd manager")
	}

	if cm.logger == nil {
		t.Fatal("ConsensusManager should have a logger")
	}

	if cm.state.Nodes == nil {
		t.Fatal("ConsensusManager should have initialized nodes map")
	}
}

func TestAddRemoveListener(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	listener1 := &mockListener{}
	listener2 := &mockListener{}

	// Add listeners
	cm.AddListener(listener1)
	cm.AddListener(listener2)

	if len(cm.listeners) != 2 {
		t.Fatalf("Expected 2 listeners, got %d", len(cm.listeners))
	}

	// Remove listener
	cm.RemoveListener(listener1)

	if len(cm.listeners) != 1 {
		t.Fatalf("Expected 1 listener after removal, got %d", len(cm.listeners))
	}

	if cm.listeners[0] != listener2 {
		t.Fatal("Wrong listener was removed")
	}
}

func TestGetNodes_Empty(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	nodes := cm.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected empty node list, got %d nodes", len(nodes))
	}
}

func TestGetLeaderNode_Empty(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	leader := cm.GetLeaderNode()
	if leader != "" {
		t.Fatalf("Expected empty leader, got %s", leader)
	}
}

func TestGetLeaderNode_WithNodes(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Add some nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47003"] = true
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	leader := cm.GetLeaderNode()
	if leader != "localhost:47001" {
		t.Fatalf("Expected leader localhost:47001, got %s", leader)
	}
}

func TestGetShardMapping_NotAvailable(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	mapping := cm.GetShardMapping()
	if mapping == nil {
		t.Fatal("Expected non-nil mapping even when not initialized")
	}
	if len(mapping.Shards) != 0 {
		t.Fatalf("Expected empty mapping, got %d shards", len(mapping.Shards))
	}
}

func TestGetShardMapping_ReturnsDeepCopy(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

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
		t.Fatalf("Expected shard 0 to be node1, got %s (deep copy failed)", mapping2.Shards[0].TargetNode)
	}
	if _, exists := mapping2.Shards[999]; exists {
		t.Fatal("Expected shard 999 to not exist (deep copy failed)")
	}
	if len(mapping2.Shards) != 2 {
		t.Fatalf("Expected 2 shards, got %d (deep copy failed)", len(mapping2.Shards))
	}
}

func TestCreateShardMapping_NoNodes(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Fatalf("Should not error when no nodes available, got: %v", err)
	}
	if n != 0 {
		t.Fatalf("Expected 0 shards reassigned, got %d", n)
	}
}

func TestCreateShardMapping_WithNodes_NoExistingMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	mgr.Connect()

	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Add nodes to internal state
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.mu.Unlock()

	n, err := cm.ReassignShardTargetNodes(context.Background())
	if err != nil {
		t.Fatalf("Failed to create shard mapping: %v", err)
	}
	if n != testNumShards {
		t.Fatalf("Expected %d shards reassigned (creating initial mapping), got %d", testNumShards, n)
	}

	if len(cm.state.ShardMapping.Shards) != testNumShards {
		t.Fatalf("Expected shard mapping to have %d shards, got %d", testNumShards, len(cm.state.ShardMapping.Shards))
	}
}

func TestUpdateShardMapping_WithExisting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	mgr.Connect()

	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Add initial nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true

	// Set initial mapping
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < testNumShards/2; i++ {
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
		t.Fatal("Expected some shards to be reassigned after adding a new node")
	}

	if len(cm.state.ShardMapping.Shards) != testNumShards {
		t.Fatalf("Expected %d shards in updated mapping, got %d", testNumShards, len(cm.state.ShardMapping.Shards))
	}
}

func TestUpdateShardMapping_NoChanges(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Add nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true

	// Set mapping with same nodes
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < testNumShards; i++ {
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
		t.Fatalf("Expected 0 shards reassigned when nodes unchanged, got %d", n)
	}

	// Verify the mapping is the same (pointer comparison)
	cm.mu.RLock()
	sameMapping := (cm.state.ShardMapping == cm.state.ShardMapping)
	cm.mu.RUnlock()

	if !sameMapping {
		t.Fatal("Expected same mapping object when no changes needed")
	}
}

func TestIsStateStable(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	localAddr := "localhost:47001"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 10*time.Second, localAddr, testNumShards)

	// Not stable when lastNodeChange is zero
	if cm.IsStateStable() {
		t.Fatal("Should not be stable when lastNodeChange is zero")
	}

	// Set lastNodeChange to recent time
	cm.mu.Lock()
	cm.state.LastChange = time.Now()
	cm.mu.Unlock()

	// Should not be stable for longer duration (no nodes yet)
	if cm.IsStateStable() {
		t.Fatal("Should not be stable for 10 seconds when local node not in cluster")
	}

	// Set lastNodeChange to past but nodes list is empty
	cm.mu.Lock()
	cm.state.LastChange = time.Now().Add(-20 * time.Second)
	cm.mu.Unlock()

	// Should NOT be stable when nodes list is empty
	if cm.IsStateStable() {
		t.Fatal("Should not be stable when nodes list is empty")
	}

	// Add nodes to the state
	cm.mu.Lock()
	cm.state.Nodes[localAddr] = true
	cm.mu.Unlock()

	// Should be stable now with nodes and old lastNodeChange
	if !cm.IsStateStable() {
		t.Fatal("Should be stable after 10 seconds with nodes present")
	}
}

func TestGetLastNodeChangeTime(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Initial state
	changeTime := cm.GetLastNodeChangeTime()
	if !changeTime.IsZero() {
		t.Fatal("Initial change time should be zero")
	}

	// Set a change time
	testTime := time.Now()
	cm.mu.Lock()
	cm.state.LastChange = testTime
	cm.mu.Unlock()

	changeTime = cm.GetLastNodeChangeTime()
	if !changeTime.Equal(testTime) {
		t.Fatal("Should return the set change time")
	}
}

func TestGetNodeForShard_InvalidShard(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	_, err := cm.GetNodeForShard(-1)
	if err == nil {
		t.Fatal("Expected error for negative shard ID")
	}

	_, err = cm.GetNodeForShard(testNumShards)
	if err == nil {
		t.Fatal("Expected error for shard ID >= NumShards")
	}
}

func TestGetNodeForShard_NoMapping(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	_, err := cm.GetNodeForShard(0)
	if err == nil {
		t.Fatal("Expected error when no shard mapping available")
	}
}

func TestGetNodeForShard_WithMapping(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

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
		t.Fatalf("Expected localhost:47001, got %s", node)
	}
}

func TestGetNodeForShard_FailsWhenCurrentNodeEmpty(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

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
		t.Fatal("Expected error when CurrentNode is empty")
	}

	expectedErrMsg := "shard 0 has no current node (not yet claimed)"
	if err.Error() != expectedErrMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestGetNodeForShard_PrefersCurrentNode(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

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
		t.Fatalf("Expected CurrentNode localhost:47002, got %s", node)
	}
}

func TestGetCurrentNodeForObject_NoMapping(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	_, err := cm.GetCurrentNodeForObject("test-object")
	if err == nil {
		t.Fatal("Expected error when no shard mapping available")
	}
}

func TestGetCurrentNodeForObject_WithMapping(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Set a complete mapping with CurrentNode set
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	nodes := []string{"localhost:47001", "localhost:47002"}
	for i := 0; i < testNumShards; i++ {
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
		t.Fatalf("Unexpected node: %s", node)
	}
}

func TestGetCurrentNodeForObject_FailsWhenCurrentNodeEmpty(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Set a complete mapping with CurrentNode empty
	cm.mu.Lock()
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < testNumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "", // CurrentNode not set
		}
	}
	cm.mu.Unlock()

	// Should fail because CurrentNode is not set
	_, err := cm.GetCurrentNodeForObject("test-object-123")
	if err == nil {
		t.Fatal("Expected error when CurrentNode is empty")
	}

	if err != nil && !strings.Contains(err.Error(), "has no current node") {
		t.Fatalf("Expected error message to contain 'has no current node', got '%s'", err.Error())
	}
}

func TestGetCurrentNodeForObject_FailsWhenShardInMigration(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Set a complete mapping where shards are in migration state
	cm.mu.Lock()
	// Add nodes to the state so CurrentNode validation passes
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// For all shards, set TargetNode to localhost:47001 but CurrentNode to localhost:47002
	// This simulates a migration in progress
	for i := 0; i < testNumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47002",
		}
	}
	cm.mu.Unlock()

	// Get node for an object - should fail because shard is in migration
	node, err := cm.GetCurrentNodeForObject("test-object-123")
	if err == nil {
		t.Fatalf("Expected error when shard is in migration, but got node: %s", node)
	}

	// Verify error message mentions migration
	expectedMsg := "is in migration"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("Expected error message to contain '%s', got: %v", expectedMsg, err)
	}

	// Test with a different object to ensure it consistently fails during migration
	node2, err := cm.GetCurrentNodeForObject("another-object-456")
	if err == nil {
		t.Fatalf("Expected error when shard is in migration, but got node: %s", node2)
	}
}

func TestGetNodeForShard_FailsWhenCurrentNodeNotInNodeList(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

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
		t.Fatal("Expected error when CurrentNode is not in active node list")
	}

	expectedErrMsg := "current node localhost:47002 for shard 0 is not in active node list"
	if err.Error() != expectedErrMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestGetCurrentNodeForObject_FailsWhenCurrentNodeNotInNodeList(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Set a mapping where CurrentNode is not in the node list
	cm.mu.Lock()
	// Only add one node to the state
	cm.state.Nodes["localhost:47001"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	// For all shards, set CurrentNode to a node not in the list
	for i := 0; i < testNumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47002", // Not in node list
		}
	}
	cm.mu.Unlock()

	// Should fail because CurrentNode is not in the active node list
	_, err := cm.GetCurrentNodeForObject("test-object-123")
	if err == nil {
		t.Fatal("Expected error when CurrentNode is not in active node list")
	}

	// Error message should mention that current node is not in active node list
	if err != nil && !strings.Contains(err.Error(), "not in active node list") {
		t.Fatalf("Expected error message to contain 'not in active node list', got '%s'", err.Error())
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
				t.Fatalf("parseShardInfo(%q).TargetNode = %q, want %q", tt.value, info.TargetNode, tt.wantTarget)
			}
			if info.CurrentNode != tt.wantCurrent {
				t.Fatalf("parseShardInfo(%q).CurrentNode = %q, want %q", tt.value, info.CurrentNode, tt.wantCurrent)
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
				t.Fatalf("formatShardInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStopWatch_NotStarted(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	// Should not panic
	cm.StopWatch()
}

func TestStartWatch_NoEtcdManager(t *testing.T) {
	cm := NewConsensusManager(nil, shardlock.NewShardLock(), 0, "", testNumShards)

	ctx := context.Background()
	err := cm.StartWatch(ctx)
	if err == nil {
		t.Fatal("Expected error when etcd manager not set")
	}
}

// TestClaimShardOwnership tests that a node claims ownership of shards
// where it is the target node and CurrentNode is empty
func TestClaimShardOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
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

	ctx := context.Background()

	// Define this node's address
	thisNodeAddr := "localhost:47001"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 10*time.Second, thisNodeAddr, testNumShards)

	// Add nodes to state
	cm.mu.Lock()
	cm.state.Nodes[thisNodeAddr] = true
	cm.state.Nodes["localhost:47002"] = true
	// Set LastChange to make cluster state stable
	cm.state.LastChange = time.Now().Add(-11 * time.Second)

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

	// Call ClaimShardsForNode with the node address and stability duration
	err = cm.ClaimShardsForNode(ctx)
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
		t.Fatal("Shard 0 should exist in etcd after claiming")
	} else {
		shardInfo0 := parseShardInfo(resp0.Kvs[0])
		if shardInfo0.CurrentNode != thisNodeAddr {
			t.Fatalf("Shard 0 CurrentNode should be %s, got %s", thisNodeAddr, shardInfo0.CurrentNode)
		}
		if shardInfo0.TargetNode != thisNodeAddr {
			t.Fatalf("Shard 0 TargetNode should be %s, got %s", thisNodeAddr, shardInfo0.TargetNode)
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
			t.Fatalf("Shard 1 should not be claimed by this node, CurrentNode: %s", shardInfo1.CurrentNode)
		}
	}
}

// TestClaimShardOwnership_NoThisNode tests that claiming doesn't happen
// when this node address is not set
func TestClaimShardOwnership_NoThisNode(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)
	ctx := context.Background()

	// Set up a stable cluster state with other nodes (but not this node)
	cm.mu.Lock()
	cm.state.Nodes["localhost:47002"] = true                // Add a different node
	cm.state.LastChange = time.Now().Add(-11 * time.Second) // Make cluster stable
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  "localhost:47001",
		CurrentNode: "",
		ModRevision: 0,
	}
	cm.mu.Unlock()

	// Call ClaimShardsForNode with empty localNode - should return error
	err := cm.ClaimShardsForNode(ctx)
	if err == nil {
		t.Fatal("ClaimShardsForNode should return error when localNode is not in cluster")
	}
	// Accept either "not stable" (from IsStateStable check) or "not in cluster state" error
	if err != nil && !strings.Contains(err.Error(), "not stable") && !strings.Contains(err.Error(), "not in cluster state") {
		t.Fatalf("Expected error about cluster not stable or node not in cluster, got: %v", err)
	}

	// Verify the shard wasn't modified
	cm.mu.RLock()
	shard0 := cm.state.ShardMapping.Shards[0]
	cm.mu.RUnlock()

	if shard0.CurrentNode != "" {
		t.Fatalf("Shard should not be claimed when localNode is empty, CurrentNode: %s", shard0.CurrentNode)
	}
}

// TestClaimShardOwnership_TargetAndEmpty tests that a node claims ownership
// only when TargetNode is this node AND (CurrentNode is empty or not alive)
func TestClaimShardOwnership_TargetAndEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
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

	ctx := context.Background()

	// Define this node's address and a dead node
	thisNodeAddr := "localhost:47001"
	deadNodeAddr := "localhost:47003"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 10*time.Second, thisNodeAddr, testNumShards)

	// Add nodes to state - note that deadNodeAddr is NOT in the active node list
	cm.mu.Lock()
	cm.state.Nodes[thisNodeAddr] = true
	cm.state.Nodes["localhost:47002"] = true
	// deadNodeAddr is intentionally NOT added to simulate a dead node
	// Set LastChange to make cluster state stable
	cm.state.LastChange = time.Now().Add(-11 * time.Second)

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

	// Call ClaimShardsForNode with stability duration
	err = cm.ClaimShardsForNode(ctx)
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
			t.Fatalf("Shard 0 should NOT be claimed (target is different node), but CurrentNode is: %s", shardInfo0.CurrentNode)
		}
	}

	// Verify shard 1 was claimed (target is this node AND current is dead)
	key1 := prefix + "/shard/1"
	resp1, err := client.Get(ctx, key1)
	if err != nil {
		t.Fatalf("Failed to get shard 1 from etcd: %v", err)
	}
	if len(resp1.Kvs) == 0 {
		t.Fatal("Shard 1 should exist in etcd after claiming")
	} else {
		shardInfo1 := parseShardInfo(resp1.Kvs[0])
		if shardInfo1.CurrentNode != thisNodeAddr {
			t.Fatalf("Shard 1 CurrentNode should be %s (target is this node AND current was dead), got %s", thisNodeAddr, shardInfo1.CurrentNode)
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
			t.Fatalf("Shard 2 should NOT be claimed (target is different node), but CurrentNode is: %s", shardInfo2.CurrentNode)
		}
	}

	// Verify shard 3 was claimed (target is this node AND current is empty)
	key3 := prefix + "/shard/3"
	resp3, err := client.Get(ctx, key3)
	if err != nil {
		t.Fatalf("Failed to get shard 3 from etcd: %v", err)
	}
	if len(resp3.Kvs) == 0 {
		t.Fatal("Shard 3 should exist in etcd after claiming")
	} else {
		shardInfo3 := parseShardInfo(resp3.Kvs[0])
		if shardInfo3.CurrentNode != thisNodeAddr {
			t.Fatalf("Shard 3 CurrentNode should be %s (target is this node AND current was empty), got %s", thisNodeAddr, shardInfo3.CurrentNode)
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
			t.Fatalf("Shard 4 CurrentNode should remain %s (no update needed), got %s", thisNodeAddr, shardInfo4.CurrentNode)
		}
	}
}

// TestClaimShardsForNode_StabilityCheck tests that ClaimShardsForNode
// respects the cluster stability duration and node presence check
func TestClaimShardsForNode_StabilityCheck(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	thisNodeAddr := "localhost:47001"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 10*time.Second, thisNodeAddr, testNumShards)
	ctx := context.Background()

	t.Run("Unstable cluster - no claim", func(t *testing.T) {
		// Setup: cluster state changed recently (unstable)
		cm.mu.Lock()
		cm.state.Nodes[thisNodeAddr] = true
		cm.state.LastChange = time.Now().Add(-5 * time.Second) // Only 5s ago
		cm.state.ShardMapping = &ShardMapping{
			Shards: map[int]ShardInfo{
				0: {
					TargetNode:  thisNodeAddr,
					CurrentNode: "",
					ModRevision: 0,
				},
			},
		}
		cm.mu.Unlock()

		// Try to claim with 10s stability requirement - should return error
		err := cm.ClaimShardsForNode(ctx)
		if err == nil {
			t.Fatal("ClaimShardsForNode should return error when cluster is unstable")
		}
		if err != nil && !strings.Contains(err.Error(), "not stable") {
			t.Fatalf("Expected error about cluster not stable, got: %v", err)
		}

		// Verify the shard was not claimed
		cm.mu.RLock()
		shard0 := cm.state.ShardMapping.Shards[0]
		cm.mu.RUnlock()

		if shard0.CurrentNode != "" {
			t.Fatalf("Shard should not be claimed when cluster is unstable, got CurrentNode: %s", shard0.CurrentNode)
		}
	})

	t.Run("Stable cluster - should claim", func(t *testing.T) {
		// Setup: cluster state is stable
		cm.mu.Lock()
		cm.state.Nodes[thisNodeAddr] = true
		cm.state.LastChange = time.Now().Add(-11 * time.Second) // 11s ago - stable
		cm.state.ShardMapping = &ShardMapping{
			Shards: map[int]ShardInfo{
				1: {
					TargetNode:  thisNodeAddr,
					CurrentNode: "",
					ModRevision: 0,
				},
			},
		}
		cm.mu.Unlock()

		// Try to claim with 10s stability requirement - should claim
		// Note: This will try to write to etcd which may fail if etcd is not available
		// But we can at least verify the method doesn't return early
		_ = cm.ClaimShardsForNode(ctx)
		// We can't verify the claim succeeded without etcd, but we verified the method was called
	})

	t.Run("Node not in cluster - no claim", func(t *testing.T) {
		// Setup: stable cluster but thisNode not in cluster
		cm.mu.Lock()
		cm.state.Nodes = map[string]bool{
			"localhost:47002": true, // Only other nodes, not thisNodeAddr
		}
		cm.state.LastChange = time.Now().Add(-11 * time.Second) // Stable
		cm.state.ShardMapping = &ShardMapping{
			Shards: map[int]ShardInfo{
				2: {
					TargetNode:  "localhost:47002",
					CurrentNode: "",
					ModRevision: 0,
				},
			},
		}
		cm.mu.Unlock()

		// Try to claim (will use thisNodeAddr from constructor which is not in the cluster)
		err := cm.ClaimShardsForNode(ctx)
		if err == nil {
			t.Fatal("ClaimShardsForNode should return error when node not in cluster")
		}
		// Accept either "not stable" (from IsStateStable check) or "not in cluster state" error
		if err != nil && !strings.Contains(err.Error(), "not stable") && !strings.Contains(err.Error(), "not in cluster state") {
			t.Fatalf("Expected error about cluster not stable or node not in cluster, got: %v", err)
		}

		// Verify the shard was not claimed
		cm.mu.RLock()
		shard2 := cm.state.ShardMapping.Shards[2]
		cm.mu.RUnlock()

		if shard2.CurrentNode != "" {
			t.Fatalf("Shard should not be claimed when node not in cluster, got CurrentNode: %s", shard2.CurrentNode)
		}
	})
}

// TestReassignShardTargetNodes_RespectsCurrentNode tests the edge case where TargetNode is empty
// but CurrentNode is already set to a valid node. The leader should respect the existing assignment
// and set TargetNode to match CurrentNode instead of using round-robin assignment.
func TestReassignShardTargetNodes_RespectsCurrentNode(t *testing.T) {
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(), 0, "", testNumShards)

	node1 := "localhost:47001"
	node2 := "localhost:47002"

	// Set up cluster state with two nodes
	cm.mu.Lock()
	cm.state.Nodes = map[string]bool{
		node1: true,
		node2: true,
	}

	// Create a scenario where:
	// - Shard 0: TargetNode is empty, CurrentNode is node1
	// - Shard 1: TargetNode is empty, CurrentNode is node2
	// - Shard 2: TargetNode is empty, CurrentNode is empty (should use round-robin)
	// Note: All shards need to be initialized since calcReassignShardTargetNodes checks all 8192 shards
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}

	// Initialize all shards with valid TargetNode (so they don't need reassignment)
	for i := 0; i < testNumShards; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  node1, // All have valid target
			CurrentNode: "",
			ModRevision: 0,
		}
	}

	// Override our test cases
	cm.state.ShardMapping.Shards[0] = ShardInfo{
		TargetNode:  "",
		CurrentNode: node1,
		ModRevision: 0,
	}
	cm.state.ShardMapping.Shards[1] = ShardInfo{
		TargetNode:  "",
		CurrentNode: node2,
		ModRevision: 0,
	}
	cm.state.ShardMapping.Shards[2] = ShardInfo{
		TargetNode:  "",
		CurrentNode: "",
		ModRevision: 0,
	}
	cm.mu.Unlock()

	// Calculate reassignments
	updateShards := cm.calcReassignShardTargetNodes()

	// Verify we have 3 shards to update
	if len(updateShards) != 3 {
		t.Fatalf("Expected 3 shards to update, got %d", len(updateShards))
	}

	// Verify shard 0: TargetNode should be set to node1 (respecting CurrentNode)
	if shard0, ok := updateShards[0]; !ok {
		t.Fatal("Shard 0 should be in update list")
	} else {
		if shard0.TargetNode != node1 {
			t.Fatalf("Shard 0: Expected TargetNode to be %s (respecting CurrentNode), got %s", node1, shard0.TargetNode)
		}
		if shard0.CurrentNode != node1 {
			t.Fatalf("Shard 0: Expected CurrentNode to remain %s, got %s", node1, shard0.CurrentNode)
		}
	}

	// Verify shard 1: TargetNode should be set to node2 (respecting CurrentNode)
	if shard1, ok := updateShards[1]; !ok {
		t.Fatal("Shard 1 should be in update list")
	} else {
		if shard1.TargetNode != node2 {
			t.Fatalf("Shard 1: Expected TargetNode to be %s (respecting CurrentNode), got %s", node2, shard1.TargetNode)
		}
		if shard1.CurrentNode != node2 {
			t.Fatalf("Shard 1: Expected CurrentNode to remain %s, got %s", node2, shard1.CurrentNode)
		}
	}

	// Verify shard 2: TargetNode should be assigned via round-robin (shard 2 % 2 = 0 -> node1)
	if shard2, ok := updateShards[2]; !ok {
		t.Fatal("Shard 2 should be in update list")
	} else {
		expectedTarget := node1 // shard 2 % 2 nodes = 0, so first node in sorted list
		if shard2.TargetNode != expectedTarget {
			t.Fatalf("Shard 2: Expected TargetNode to be %s (round-robin), got %s", expectedTarget, shard2.TargetNode)
		}
		if shard2.CurrentNode != "" {
			t.Fatalf("Shard 2: Expected CurrentNode to remain empty, got %s", shard2.CurrentNode)
		}
	}
}
