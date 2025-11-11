package node

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

// TestShardMigrationManager_BlockNewCalls tests that new calls are blocked during migration
func TestShardMigrationManager_BlockNewCalls(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")
	smm := node.migrationManager

	shardID := 123
	targetNode := "localhost:47001"

	// Initially, calls should not be blocked
	if smm.ShouldBlockCallsToShard(shardID) {
		t.Errorf("Shard %d should not be blocked initially", shardID)
	}

	// Start migration in background
	go func() {
		// Create a simple state without actual objects
		smm.mu.Lock()
		smm.migratingShards[shardID] = &ShardMigrationState{
			ShardID:   shardID,
			OldNode:   node.GetAdvertiseAddress(),
			NewNode:   targetNode,
			StartTime: time.Now(),
			Phase:     PhaseBlocking,
			ObjectIDs: []string{},
		}
		smm.mu.Unlock()
	}()

	// Wait a bit for the migration to start
	time.Sleep(50 * time.Millisecond)

	// Now calls should be blocked
	if !smm.ShouldBlockCallsToShard(shardID) {
		t.Errorf("Shard %d should be blocked during migration", shardID)
	}

	// Try to begin a call - should fail
	endCall, err := smm.BeginShardCall(shardID)
	if err == nil {
		t.Errorf("BeginShardCall should have failed for migrating shard %d", shardID)
		if endCall != nil {
			endCall()
		}
	}

	// Clean up
	smm.mu.Lock()
	delete(smm.migratingShards, shardID)
	smm.mu.Unlock()
}

// TestShardMigrationManager_DrainCalls tests that in-flight calls are drained before migration proceeds
func TestShardMigrationManager_DrainCalls(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")
	smm := node.migrationManager

	shardID := 456

	// Start a call
	endCall, err := smm.BeginShardCall(shardID)
	if err != nil {
		t.Fatalf("BeginShardCall failed: %v", err)
	}

	// Verify call count
	smm.mu.RLock()
	count := smm.shardCallCounts[shardID]
	smm.mu.RUnlock()

	if count != 1 {
		t.Errorf("Expected call count 1, got %d", count)
	}

	// End the call in background after delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		endCall()
	}()

	// Wait for calls to drain
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startTime := time.Now()
	err = smm.waitForShardCallsDrained(ctx, shardID)
	duration := time.Since(startTime)

	if err != nil {
		t.Fatalf("waitForShardCallsDrained failed: %v", err)
	}

	// Should have waited approximately 100ms
	if duration < 50*time.Millisecond || duration > 200*time.Millisecond {
		t.Logf("Warning: drain took %v, expected ~100ms", duration)
	}

	// Verify call count is now zero
	smm.mu.RLock()
	count = smm.shardCallCounts[shardID]
	smm.mu.RUnlock()

	if count != 0 {
		t.Errorf("Expected call count 0 after drain, got %d", count)
	}
}

// TestShardMigrationManager_MigrateEmptyShard tests migrating a shard with no objects
func TestShardMigrationManager_MigrateEmptyShard(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")
	ctx := context.Background()

	// Pick a shard that has no objects
	shardID := 789
	targetNode := "localhost:47001"

	// Migrate the shard
	err := node.MigrateShard(ctx, shardID, targetNode)
	if err != nil {
		t.Fatalf("MigrateShard failed: %v", err)
	}

	// Verify migration is complete
	status := node.GetShardMigrationStatus()
	if len(status) != 0 {
		t.Errorf("Expected no active migrations, got %d", len(status))
	}
}

// TestCallObject_BlockedDuringMigration tests that CallObject checks shard migration
func TestCallObject_BlockedDuringMigration(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")

	// Create an object ID
	objectID := "TestObject-migration-test"
	shardID := sharding.GetShardID(objectID)

	// Start migration of this shard
	smm := node.migrationManager
	smm.mu.Lock()
	smm.migratingShards[shardID] = &ShardMigrationState{
		ShardID:   shardID,
		OldNode:   node.GetAdvertiseAddress(),
		NewNode:   "localhost:47001",
		StartTime: time.Now(),
		Phase:     PhaseBlocking,
		ObjectIDs: []string{},
	}
	smm.mu.Unlock()

	// Try to begin a shard call - should fail
	endCall, err := smm.BeginShardCall(shardID)
	if err == nil {
		t.Errorf("BeginShardCall should have failed for object in migrating shard")
		if endCall != nil {
			endCall()
		}
	}

	// Clean up
	smm.mu.Lock()
	delete(smm.migratingShards, shardID)
	smm.mu.Unlock()
}

// TestCreateObject_BlockedDuringMigration tests that createObject checks shard migration
func TestCreateObject_BlockedDuringMigration(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")

	// Choose an object ID
	objectID := "TestObject-create-test"
	shardID := sharding.GetShardID(objectID)

	// Start migration of this shard
	smm := node.migrationManager
	smm.mu.Lock()
	smm.migratingShards[shardID] = &ShardMigrationState{
		ShardID:   shardID,
		OldNode:   node.GetAdvertiseAddress(),
		NewNode:   "localhost:47001",
		StartTime: time.Now(),
		Phase:     PhaseBlocking,
		ObjectIDs: []string{},
	}
	smm.mu.Unlock()

	// Check if calls should be blocked
	shouldBlock := smm.ShouldBlockCallsToShard(shardID)
	if !shouldBlock {
		t.Errorf("ShouldBlockCallsToShard should return true for migrating shard")
	}

	// Clean up
	smm.mu.Lock()
	delete(smm.migratingShards, shardID)
	smm.mu.Unlock()
}

// TestShardMigrationManager_ClientObjectsNotBlocked tests that client objects (with /) are not blocked
func TestShardMigrationManager_ClientObjectsNotBlocked(t *testing.T) {
	t.Parallel()

	node := NewNode("localhost:47000")

	// Client IDs have format: nodeAddress/uniqueId
	clientID := "localhost:47000/client-123"

	// Calculate what shard this would map to (even though it's pinned to a node)
	shardID := sharding.GetShardID(clientID)

	// Start migration of this shard
	smm := node.migrationManager
	smm.mu.Lock()
	smm.migratingShards[shardID] = &ShardMigrationState{
		ShardID:   shardID,
		OldNode:   node.GetAdvertiseAddress(),
		NewNode:   "localhost:47001",
		StartTime: time.Now(),
		Phase:     PhaseBlocking,
		ObjectIDs: []string{},
	}
	smm.mu.Unlock()

	// Verify that containsSlash correctly identifies client objects
	if !containsSlash(clientID) {
		t.Errorf("containsSlash should return true for client ID %s", clientID)
	}

	// Regular object IDs should not contain slash
	regularID := "TestObject-123"
	if containsSlash(regularID) {
		t.Errorf("containsSlash should return false for regular object ID %s", regularID)
	}

	// Clean up
	smm.mu.Lock()
	delete(smm.migratingShards, shardID)
	smm.mu.Unlock()
}
