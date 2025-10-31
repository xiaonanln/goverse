package sharding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
)

func TestCreateShardMapping(t *testing.T) {
	tests := []struct {
		name      string
		nodes     []string
		wantErr   bool
		expectMsg string
	}{
		{
			name:      "single node",
			nodes:     []string{"node1"},
			wantErr:   false,
			expectMsg: "",
		},
		{
			name:      "multiple nodes",
			nodes:     []string{"node1", "node2", "node3"},
			wantErr:   false,
			expectMsg: "",
		},
		{
			name:      "no nodes",
			nodes:     []string{},
			wantErr:   true,
			expectMsg: "cannot create shard mapping with no nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewShardMapper(nil)
			ctx := context.Background()

			mapping, err := sm.CreateShardMapping(ctx, tt.nodes)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateShardMapping() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("CreateShardMapping() unexpected error: %v", err)
				return
			}

			// Verify all shards are assigned
			if len(mapping.Shards) != NumShards {
				t.Errorf("CreateShardMapping() created %d shard assignments, want %d", len(mapping.Shards), NumShards)
			}

			// Verify all shards are assigned to valid nodes
			nodeSet := make(map[string]bool)
			for _, node := range tt.nodes {
				nodeSet[node] = true
			}

			for shardID := 0; shardID < NumShards; shardID++ {
				node, ok := mapping.Shards[shardID]
				if !ok {
					t.Errorf("Shard %d is not assigned to any node", shardID)
				}
				if !nodeSet[node] {
					t.Errorf("Shard %d is assigned to unknown node %s", shardID, node)
				}
			}

			// Verify version is set
			if mapping.Version != 1 {
				t.Errorf("CreateShardMapping() version = %d, want 1", mapping.Version)
			}
		})
	}
}

func TestCreateShardMapping_Distribution(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	nodes := []string{"node1", "node2", "node3"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() unexpected error: %v", err)
	}

	// Count shards per node
	shardCounts := make(map[string]int)
	for _, node := range mapping.Shards {
		shardCounts[node]++
	}

	// Verify relatively even distribution
	expectedPerNode := NumShards / len(nodes)
	for node, count := range shardCounts {
		// Allow some variance (within 1 of expected)
		if count < expectedPerNode-1 || count > expectedPerNode+1 {
			t.Errorf("Node %s has %d shards, expected around %d", node, count, expectedPerNode)
		}
	}

	// Verify all nodes have at least some shards
	if len(shardCounts) != len(nodes) {
		t.Errorf("Not all nodes have shard assignments: got %d nodes with shards, want %d", len(shardCounts), len(nodes))
	}
}

func TestCreateShardMapping_Deterministic(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	nodes := []string{"node1", "node2", "node3"}

	// Create mapping twice
	mapping1, err1 := sm.CreateShardMapping(ctx, nodes)
	if err1 != nil {
		t.Fatalf("CreateShardMapping() first call unexpected error: %v", err1)
	}

	mapping2, err2 := sm.CreateShardMapping(ctx, nodes)
	if err2 != nil {
		t.Fatalf("CreateShardMapping() second call unexpected error: %v", err2)
	}

	// Verify they produce the same shard assignments
	for shardID := 0; shardID < NumShards; shardID++ {
		node1 := mapping1.Shards[shardID]
		node2 := mapping2.Shards[shardID]
		if node1 != node2 {
			t.Errorf("Shard %d assigned to different nodes: %s vs %s", shardID, node1, node2)
		}
	}
}

func TestCreateShardMapping_NodeOrderIndependent(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create mapping with nodes in different order
	nodes1 := []string{"node1", "node2", "node3"}
	nodes2 := []string{"node3", "node1", "node2"}

	mapping1, err1 := sm.CreateShardMapping(ctx, nodes1)
	if err1 != nil {
		t.Fatalf("CreateShardMapping() first call unexpected error: %v", err1)
	}

	mapping2, err2 := sm.CreateShardMapping(ctx, nodes2)
	if err2 != nil {
		t.Fatalf("CreateShardMapping() second call unexpected error: %v", err2)
	}

	// Verify they produce the same shard assignments (sorted internally)
	for shardID := 0; shardID < NumShards; shardID++ {
		node1 := mapping1.Shards[shardID]
		node2 := mapping2.Shards[shardID]
		if node1 != node2 {
			t.Errorf("Shard %d assigned to different nodes with different input order: %s vs %s", shardID, node1, node2)
		}
	}
}

func TestShardMapping_Serialization(t *testing.T) {
	mapping := &ShardMapping{
		Shards:  make(map[int]string),
		Version: 5,
	}

	// Add some sample shard assignments
	mapping.Shards[0] = "node1"
	mapping.Shards[1] = "node2"
	mapping.Shards[100] = "node3"

	// Serialize
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	// Deserialize
	var decoded ShardMapping
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}

	// Verify
	if decoded.Version != mapping.Version {
		t.Errorf("Version = %d, want %d", decoded.Version, mapping.Version)
	}

	if len(decoded.Shards) != len(mapping.Shards) {
		t.Errorf("Shards length = %d, want %d", len(decoded.Shards), len(mapping.Shards))
	}

	for shardID, node := range mapping.Shards {
		decodedNode, ok := decoded.Shards[shardID]
		if !ok {
			t.Errorf("Shard %d missing in decoded mapping", shardID)
		}
		if decodedNode != node {
			t.Errorf("Shard %d node = %s, want %s", shardID, decodedNode, node)
		}
	}
}

func TestGetNodeForShard(t *testing.T) {
	sm := NewShardMapper(nil)

	// Create a test mapping with all shards assigned
	mapping := &ShardMapping{
		Shards:  make(map[int]string),
		Version: 1,
	}

	// Assign all shards for testing
	for i := 0; i < NumShards; i++ {
		if i%3 == 0 {
			mapping.Shards[i] = "node1"
		} else if i%3 == 1 {
			mapping.Shards[i] = "node2"
		} else {
			mapping.Shards[i] = "node3"
		}
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	ctx := context.Background()

	tests := []struct {
		name      string
		shardID   int
		wantNode  string
		wantErr   bool
		expectMsg string
	}{
		{
			name:     "valid shard 0",
			shardID:  0,
			wantNode: "node1",
			wantErr:  false,
		},
		{
			name:     "valid shard 1",
			shardID:  1,
			wantNode: "node2",
			wantErr:  false,
		},
		{
			name:     "valid shard 100",
			shardID:  100,
			wantNode: "node2", // 100 % 3 = 1
			wantErr:  false,
		},
		{
			name:      "invalid shard -1",
			shardID:   -1,
			wantErr:   true,
			expectMsg: "invalid shard ID",
		},
		{
			name:      "invalid shard too large",
			shardID:   NumShards,
			wantErr:   true,
			expectMsg: "invalid shard ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := sm.GetNodeForShard(ctx, tt.shardID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetNodeForShard() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetNodeForShard() unexpected error: %v", err)
				return
			}

			if node != tt.wantNode {
				t.Errorf("GetNodeForShard() = %s, want %s", node, tt.wantNode)
			}
		})
	}
}

func TestGetNodeForObject(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create a test mapping with all shards assigned
	nodes := []string{"node1", "node2", "node3"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	testCases := []string{
		"object1",
		"object2",
		"user-12345",
		"session-abc",
		"",
	}

	for _, objectID := range testCases {
		t.Run(objectID, func(t *testing.T) {
			node, err := sm.GetNodeForObject(ctx, objectID)
			if err != nil {
				t.Errorf("GetNodeForObject(%s) error: %v", objectID, err)
				return
			}

			// Verify the node is one of the valid nodes
			validNode := false
			for _, n := range nodes {
				if n == node {
					validNode = true
					break
				}
			}

			if !validNode {
				t.Errorf("GetNodeForObject(%s) = %s, not a valid node", objectID, node)
			}

			// Verify consistency - same object ID should always map to same node
			node2, err2 := sm.GetNodeForObject(ctx, objectID)
			if err2 != nil {
				t.Errorf("GetNodeForObject(%s) second call error: %v", objectID, err2)
				return
			}

			if node != node2 {
				t.Errorf("GetNodeForObject(%s) not consistent: %s vs %s", objectID, node, node2)
			}
		})
	}
}

func TestUpdateShardMapping(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	initialNodes := []string{"node1", "node2", "node3"}
	initialMapping, err := sm.CreateShardMapping(ctx, initialNodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Store in cache to simulate it being loaded from etcd
	sm.mu.Lock()
	sm.mapping = initialMapping
	sm.mu.Unlock()

	// Update with same nodes (should NOT increment version since no changes)
	updatedMapping, err := sm.UpdateShardMapping(ctx, initialNodes)
	if err != nil {
		t.Fatalf("UpdateShardMapping() error: %v", err)
	}

	if updatedMapping.Version != initialMapping.Version {
		t.Errorf("UpdateShardMapping() version = %d, want %d (no change expected)", updatedMapping.Version, initialMapping.Version)
	}

	// Verify shards stayed on same nodes
	for shardID := 0; shardID < NumShards; shardID++ {
		initialNode := initialMapping.Shards[shardID]
		updatedNode := updatedMapping.Shards[shardID]
		if initialNode != updatedNode {
			t.Errorf("Shard %d moved from %s to %s when it should have stayed", shardID, initialNode, updatedNode)
		}
	}
}

func TestUpdateShardMapping_NodeRemoved(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping with 3 nodes
	initialNodes := []string{"node1", "node2", "node3"}
	initialMapping, err := sm.CreateShardMapping(ctx, initialNodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Store in cache to simulate it being loaded from etcd
	sm.mu.Lock()
	sm.mapping = initialMapping
	sm.mu.Unlock()

	// Update with only 2 nodes (node3 removed)
	updatedNodes := []string{"node1", "node2"}
	updatedMapping, err := sm.UpdateShardMapping(ctx, updatedNodes)
	if err != nil {
		t.Fatalf("UpdateShardMapping() error: %v", err)
	}

	// Verify all shards are assigned to remaining nodes
	for shardID := 0; shardID < NumShards; shardID++ {
		node := updatedMapping.Shards[shardID]
		if node != "node1" && node != "node2" {
			t.Errorf("Shard %d assigned to removed node %s", shardID, node)
		}
	}

	// Count how many shards stayed on their original nodes
	stableCount := 0
	for shardID := 0; shardID < NumShards; shardID++ {
		initialNode := initialMapping.Shards[shardID]
		updatedNode := updatedMapping.Shards[shardID]

		if initialNode == "node1" || initialNode == "node2" {
			if initialNode == updatedNode {
				stableCount++
			}
		}
	}

	// With 3 nodes initially and round-robin, ~2/3 of shards were on node1 or node2
	// All of those should remain stable
	expectedStable := (NumShards / 3) * 2
	// Allow for rounding differences
	if stableCount < expectedStable-2 || stableCount > expectedStable+2 {
		t.Errorf("Expected around %d stable shard assignments, got %d", expectedStable, stableCount)
	}

	t.Logf("Stable assignments: %d out of %d shards", stableCount, NumShards)
}

func TestUpdateShardMapping_NodeAdded(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping with 2 nodes
	initialNodes := []string{"node1", "node2"}
	initialMapping, err := sm.CreateShardMapping(ctx, initialNodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Store in cache to simulate it being loaded from etcd
	sm.mu.Lock()
	sm.mapping = initialMapping
	sm.mu.Unlock()

	// Update with 3 nodes (node3 added)
	updatedNodes := []string{"node1", "node2", "node3"}
	updatedMapping, err := sm.UpdateShardMapping(ctx, updatedNodes)
	if err != nil {
		t.Fatalf("UpdateShardMapping() error: %v", err)
	}

	// Verify all shards are assigned to valid nodes
	validNodes := map[string]bool{"node1": true, "node2": true, "node3": true}
	for shardID := 0; shardID < NumShards; shardID++ {
		node := updatedMapping.Shards[shardID]
		if !validNodes[node] {
			t.Errorf("Shard %d assigned to invalid node %s", shardID, node)
		}
	}

	// Verify shards on node1 or node2 stayed there (stability)
	movedCount := 0
	for shardID := 0; shardID < NumShards; shardID++ {
		initialNode := initialMapping.Shards[shardID]
		updatedNode := updatedMapping.Shards[shardID]

		if initialNode != updatedNode {
			movedCount++
		}
	}

	// All shards should stay on their original nodes when adding a new node
	// because the update logic preserves existing assignments
	if movedCount != 0 {
		t.Errorf("Expected 0 shards to move when adding a node, got %d", movedCount)
	}
}

func TestInvalidateCache(t *testing.T) {
	sm := NewShardMapper(nil)

	// Set a mapping in cache
	mapping := &ShardMapping{
		Shards:  make(map[int]string),
		Version: 1,
	}
	mapping.Shards[0] = "node1"

	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Verify cache is set
	sm.mu.RLock()
	if sm.mapping == nil {
		t.Fatalf("Cache should be set")
	}
	sm.mu.RUnlock()

	// Invalidate cache
	sm.InvalidateCache()

	// Verify cache is cleared
	sm.mu.RLock()
	if sm.mapping != nil {
		t.Errorf("Cache should be nil after invalidation")
	}
	sm.mu.RUnlock()
}

func TestNewShardMapper(t *testing.T) {
	etcdMgr, err := etcdmanager.NewEtcdManager("localhost:2379", "")
	if err != nil {
		t.Fatalf("NewEtcdManager() error: %v", err)
	}

	sm := NewShardMapper(etcdMgr)
	if sm == nil {
		t.Errorf("NewShardMapper() returned nil")
	}

	if sm.etcdManager != etcdMgr {
		t.Errorf("NewShardMapper() etcdManager not set correctly")
	}

	if sm.logger == nil {
		t.Errorf("NewShardMapper() logger not initialized")
	}
}

func TestGetNodeForObject_FixedNodeAddress(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		objectID     string
		wantNode     string
		wantErr      bool
		setupMapping bool
	}{
		{
			name:     "fixed node address format",
			objectID: "localhost:7001/object-123",
			wantNode: "localhost:7001",
			wantErr:  false,
		},
		{
			name:     "different fixed node",
			objectID: "localhost:7002/user-456",
			wantNode: "localhost:7002",
			wantErr:  false,
		},
		{
			name:     "complex object ID with fixed node",
			objectID: "192.168.1.100:8080/session-abc-def-123",
			wantNode: "192.168.1.100:8080",
			wantErr:  false,
		},
		{
			name:         "regular object ID without fixed node",
			objectID:     "object-without-slash",
			wantNode:     "", // Will be determined by shard mapping
			wantErr:      false,
			setupMapping: true,
		},
		{
			name:     "invalid format - empty node",
			objectID: "/object-123",
			wantNode: "", // Falls back to shard-based mapping
			wantErr:  false,
			setupMapping: true,
		},
		{
			name:     "invalid format - empty object part",
			objectID: "localhost:7001/",
			wantNode: "", // Falls back to shard-based mapping
			wantErr:  false,
			setupMapping: true,
		},
		{
			name:     "slash in middle of regular object ID",
			objectID: "type/subtype-123",
			wantNode: "type",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupMapping {
				// Create a test mapping for cases that need it
				nodes := []string{"node1", "node2", "node3"}
				mapping, err := sm.CreateShardMapping(ctx, nodes)
				if err != nil {
					t.Fatalf("CreateShardMapping() error: %v", err)
				}
				sm.mu.Lock()
				sm.mapping = mapping
				sm.mu.Unlock()
			}

			node, err := sm.GetNodeForObject(ctx, tt.objectID)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetNodeForObject() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetNodeForObject() unexpected error: %v", err)
				return
			}

			if tt.wantNode != "" && node != tt.wantNode {
				t.Errorf("GetNodeForObject(%s) = %s, want %s", tt.objectID, node, tt.wantNode)
			}

			// For cases where we expect a valid node from mapping
			if tt.setupMapping && tt.wantNode == "" {
				// Just verify we got a valid node from the mapping
				validNodes := []string{"node1", "node2", "node3"}
				found := false
				for _, validNode := range validNodes {
					if node == validNode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("GetNodeForObject(%s) returned invalid node: %s", tt.objectID, node)
				}
			}
		})
	}
}

func TestGetNodeForObject_ConsistencyWithFixedNode(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Test that the same fixed node address always returns the same result
	objectID := "localhost:7001/my-object"

	node1, err1 := sm.GetNodeForObject(ctx, objectID)
	if err1 != nil {
		t.Fatalf("GetNodeForObject() first call error: %v", err1)
	}

	node2, err2 := sm.GetNodeForObject(ctx, objectID)
	if err2 != nil {
		t.Fatalf("GetNodeForObject() second call error: %v", err2)
	}

	if node1 != node2 {
		t.Errorf("GetNodeForObject() not consistent: %s vs %s", node1, node2)
	}

	if node1 != "localhost:7001" {
		t.Errorf("GetNodeForObject() = %s, want localhost:7001", node1)
	}
}
