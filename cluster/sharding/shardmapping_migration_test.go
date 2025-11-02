package sharding

import (
	"context"
	"testing"
)

func TestShardMapper_UpdateShardState(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Test updating shard state
	shardID := 100
	err = sm.UpdateShardState(ctx, shardID, ShardStateMigrating, "node2")
	if err != nil {
		t.Fatalf("UpdateShardState() error: %v", err)
	}

	// Verify the state was updated
	shardInfo, err := sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateMigrating {
		t.Errorf("Shard state = %s, want MIGRATING", shardInfo.State.String())
	}

	if shardInfo.CurrentNode != "node2" {
		t.Errorf("Shard current node = %s, want node2", shardInfo.CurrentNode)
	}
}

func TestShardMapper_MarkShardForMigration(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Test marking a shard for migration
	shardID := 50
	err = sm.MarkShardForMigration(ctx, shardID)
	if err != nil {
		t.Fatalf("MarkShardForMigration() error: %v", err)
	}

	// Verify the shard is marked as migrating
	shardInfo, err := sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateMigrating {
		t.Errorf("Shard state = %s, want MIGRATING", shardInfo.State.String())
	}

	// Test marking an already migrating shard (should be idempotent)
	err = sm.MarkShardForMigration(ctx, shardID)
	if err != nil {
		t.Errorf("MarkShardForMigration() on already migrating shard error: %v", err)
	}
}

func TestShardMapper_CompleteMigration(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Mark a shard for migration
	shardID := 75
	err = sm.MarkShardForMigration(ctx, shardID)
	if err != nil {
		t.Fatalf("MarkShardForMigration() error: %v", err)
	}

	// Complete the migration
	err = sm.CompleteMigration(ctx, shardID)
	if err != nil {
		t.Fatalf("CompleteMigration() error: %v", err)
	}

	// Verify the shard is now available on the target node
	shardInfo, err := sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateAvailable {
		t.Errorf("Shard state = %s, want AVAILABLE", shardInfo.State.String())
	}

	if shardInfo.CurrentNode != shardInfo.TargetNode {
		t.Errorf("Shard current node = %s, want target node %s", shardInfo.CurrentNode, shardInfo.TargetNode)
	}
}

func TestShardMapper_GetShardInfo(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2", "node3"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Test getting shard info
	shardID := 123
	shardInfo, err := sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.ShardID != shardID {
		t.Errorf("Shard ID = %d, want %d", shardInfo.ShardID, shardID)
	}

	if shardInfo.State != ShardStateAvailable {
		t.Errorf("Initial shard state = %s, want AVAILABLE", shardInfo.State.String())
	}

	if shardInfo.CurrentNode != shardInfo.TargetNode {
		t.Errorf("Initial current node = %s, want target node %s", shardInfo.CurrentNode, shardInfo.TargetNode)
	}

	// Test invalid shard ID
	_, err = sm.GetShardInfo(ctx, -1)
	if err == nil {
		t.Error("GetShardInfo() with invalid shard ID should return error")
	}

	_, err = sm.GetShardInfo(ctx, NumShards)
	if err == nil {
		t.Error("GetShardInfo() with shard ID >= NumShards should return error")
	}
}

func TestShardMapper_GetShardsInState(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Initially all shards should be AVAILABLE
	availableShards, err := sm.GetShardsInState(ctx, ShardStateAvailable)
	if err != nil {
		t.Fatalf("GetShardsInState(AVAILABLE) error: %v", err)
	}

	if len(availableShards) != NumShards {
		t.Errorf("Expected %d available shards, got %d", NumShards, len(availableShards))
	}

	// Mark some shards for migration
	shardsToMigrate := []int{10, 20, 30}
	for _, shardID := range shardsToMigrate {
		err = sm.MarkShardForMigration(ctx, shardID)
		if err != nil {
			t.Fatalf("MarkShardForMigration(%d) error: %v", shardID, err)
		}
	}

	// Check migrating shards
	migratingShards, err := sm.GetShardsInState(ctx, ShardStateMigrating)
	if err != nil {
		t.Fatalf("GetShardsInState(MIGRATING) error: %v", err)
	}

	if len(migratingShards) != len(shardsToMigrate) {
		t.Errorf("Expected %d migrating shards, got %d", len(shardsToMigrate), len(migratingShards))
	}

	// Verify the specific shards are in the result
	migratingSet := make(map[int]bool)
	for _, shardID := range migratingShards {
		migratingSet[shardID] = true
	}

	for _, expectedShardID := range shardsToMigrate {
		if !migratingSet[expectedShardID] {
			t.Errorf("Expected shard %d to be in migrating state", expectedShardID)
		}
	}

	// Check offline shards (should be none)
	offlineShards, err := sm.GetShardsInState(ctx, ShardStateOffline)
	if err != nil {
		t.Fatalf("GetShardsInState(OFFLINE) error: %v", err)
	}

	if len(offlineShards) != 0 {
		t.Errorf("Expected 0 offline shards, got %d", len(offlineShards))
	}
}

func TestShardMapper_GetShardsOnNode(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2", "node3"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	// Get shards on each node
	for _, node := range nodes {
		shards, err := sm.GetShardsOnNode(ctx, node)
		if err != nil {
			t.Fatalf("GetShardsOnNode(%s) error: %v", node, err)
		}

		// Verify each shard is actually on the node
		for _, shardID := range shards {
			shardInfo, err := sm.GetShardInfo(ctx, shardID)
			if err != nil {
				t.Fatalf("GetShardInfo(%d) error: %v", shardID, err)
			}

			if shardInfo.CurrentNode != node {
				t.Errorf("Shard %d current node = %s, want %s", shardID, shardInfo.CurrentNode, node)
			}
		}

		t.Logf("Node %s has %d shards", node, len(shards))
	}

	// Test with non-existent node
	noShards, err := sm.GetShardsOnNode(ctx, "non-existent-node")
	if err != nil {
		t.Fatalf("GetShardsOnNode(non-existent) error: %v", err)
	}

	if len(noShards) != 0 {
		t.Errorf("Expected 0 shards on non-existent node, got %d", len(noShards))
	}
}

func TestShardState_String(t *testing.T) {
	tests := []struct {
		state ShardState
		want  string
	}{
		{ShardStateAvailable, "AVAILABLE"},
		{ShardStateMigrating, "MIGRATING"},
		{ShardStateOffline, "OFFLINE"},
		{ShardState(99), "UNKNOWN(99)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("ShardState.String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShardMapper_MigrationWorkflow(t *testing.T) {
	sm := NewShardMapper(nil)
	ctx := context.Background()

	// Create initial mapping
	nodes := []string{"node1", "node2"}
	mapping, err := sm.CreateShardMapping(ctx, nodes)
	if err != nil {
		t.Fatalf("CreateShardMapping() error: %v", err)
	}

	// Set the mapping in cache
	sm.mu.Lock()
	sm.mapping = mapping
	sm.mu.Unlock()

	shardID := 200

	// Step 1: Shard starts as AVAILABLE on its target node
	shardInfo, err := sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateAvailable {
		t.Fatalf("Initial state = %s, want AVAILABLE", shardInfo.State.String())
	}

	originalNode := shardInfo.CurrentNode
	t.Logf("Step 1: Shard %d is AVAILABLE on %s", shardID, originalNode)

	// Step 2: Mark shard for migration (simulating node removal/rebalancing)
	err = sm.MarkShardForMigration(ctx, shardID)
	if err != nil {
		t.Fatalf("MarkShardForMigration() error: %v", err)
	}

	shardInfo, err = sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateMigrating {
		t.Fatalf("State after marking = %s, want MIGRATING", shardInfo.State.String())
	}

	t.Logf("Step 2: Shard %d marked for MIGRATING from %s to %s",
		shardID, shardInfo.CurrentNode, shardInfo.TargetNode)

	// Step 3: Complete migration
	err = sm.CompleteMigration(ctx, shardID)
	if err != nil {
		t.Fatalf("CompleteMigration() error: %v", err)
	}

	shardInfo, err = sm.GetShardInfo(ctx, shardID)
	if err != nil {
		t.Fatalf("GetShardInfo() error: %v", err)
	}

	if shardInfo.State != ShardStateAvailable {
		t.Fatalf("State after completion = %s, want AVAILABLE", shardInfo.State.String())
	}

	if shardInfo.CurrentNode != shardInfo.TargetNode {
		t.Fatalf("Current node = %s, want target node %s", shardInfo.CurrentNode, shardInfo.TargetNode)
	}

	t.Logf("Step 3: Shard %d migration completed, now AVAILABLE on %s", shardID, shardInfo.CurrentNode)
}
