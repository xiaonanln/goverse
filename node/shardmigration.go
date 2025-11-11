package node

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/logger"
)

// ShardMigrationManager handles safe removal of objects when shards move to different nodes
type ShardMigrationManager struct {
	node   *Node
	logger *logger.Logger

	// migratingShards tracks shards that are being migrated away from this node
	// Key: shard ID, Value: migration state
	mu               sync.RWMutex
	migratingShards  map[int]*ShardMigrationState
	shardCallCounts  map[int]int // Number of active calls per shard
	shardCallWaiters map[int][]chan struct{}
}

// ShardMigrationState tracks the state of a shard migration
type ShardMigrationState struct {
	ShardID    int
	OldNode    string
	NewNode    string
	StartTime  time.Time
	Phase      MigrationPhase
	ObjectIDs  []string // Objects that need to be migrated
}

// MigrationPhase represents the current phase of shard migration
type MigrationPhase int

const (
	// PhaseBlocking - New calls to this shard are blocked
	PhaseBlocking MigrationPhase = iota
	// PhaseDraining - Waiting for in-flight calls to complete
	PhaseDraining
	// PhasePersisting - Saving object state
	PhasePersisting
	// PhaseRemoving - Removing objects from memory
	PhaseRemoving
	// PhaseComplete - Migration complete
	PhaseComplete
)

func (p MigrationPhase) String() string {
	switch p {
	case PhaseBlocking:
		return "Blocking"
	case PhaseDraining:
		return "Draining"
	case PhasePersisting:
		return "Persisting"
	case PhaseRemoving:
		return "Removing"
	case PhaseComplete:
		return "Complete"
	default:
		return fmt.Sprintf("Unknown(%d)", p)
	}
}

// NewShardMigrationManager creates a new shard migration manager
func NewShardMigrationManager(n *Node) *ShardMigrationManager {
	return &ShardMigrationManager{
		node:             n,
		logger:           logger.NewLogger("ShardMigration"),
		migratingShards:  make(map[int]*ShardMigrationState),
		shardCallCounts:  make(map[int]int),
		shardCallWaiters: make(map[int][]chan struct{}),
	}
}

// IsShardMigrating returns true if the given shard is currently being migrated away
func (smm *ShardMigrationManager) IsShardMigrating(shardID int) bool {
	smm.mu.RLock()
	defer smm.mu.RUnlock()
	_, exists := smm.migratingShards[shardID]
	return exists
}

// ShouldBlockCallsToShard returns true if calls to this shard should be blocked
func (smm *ShardMigrationManager) ShouldBlockCallsToShard(shardID int) bool {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	state, exists := smm.migratingShards[shardID]
	if !exists {
		return false
	}

	// Block calls during all phases except complete
	return state.Phase != PhaseComplete
}

// BeginShardCall registers the start of a call to an object in the given shard
// Returns an error if the shard is being migrated and new calls should be blocked
func (smm *ShardMigrationManager) BeginShardCall(shardID int) (func(), error) {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	// Check if shard is migrating
	if state, exists := smm.migratingShards[shardID]; exists {
		// Block new calls during migration
		return nil, fmt.Errorf("shard %d is being migrated to %s (phase: %s)", 
			shardID, state.NewNode, state.Phase)
	}

	// Increment call count
	smm.shardCallCounts[shardID]++

	// Return a cleanup function that will be called when the operation completes
	return func() {
		smm.endShardCall(shardID)
	}, nil
}

// endShardCall decrements the call count for a shard and notifies waiters if count reaches zero
func (smm *ShardMigrationManager) endShardCall(shardID int) {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	count := smm.shardCallCounts[shardID]
	if count > 0 {
		smm.shardCallCounts[shardID]--
		count--
	}

	// If count reached zero and there are waiters, notify them
	if count == 0 {
		waiters := smm.shardCallWaiters[shardID]
		for _, waiter := range waiters {
			close(waiter)
		}
		delete(smm.shardCallWaiters, shardID)
	}
}

// waitForShardCallsDrained waits for all in-flight calls to the given shard to complete
func (smm *ShardMigrationManager) waitForShardCallsDrained(ctx context.Context, shardID int) error {
	smm.mu.Lock()

	// Check current call count
	count := smm.shardCallCounts[shardID]
	if count == 0 {
		smm.mu.Unlock()
		return nil
	}

	// Create a waiter channel
	waiter := make(chan struct{})
	smm.shardCallWaiters[shardID] = append(smm.shardCallWaiters[shardID], waiter)
	smm.mu.Unlock()

	// Wait for either the waiter to be notified or context cancellation
	select {
	case <-waiter:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MigrateShard initiates migration of a shard from this node to another node
// This performs all phases: blocking, draining, persisting, and removing
func (smm *ShardMigrationManager) MigrateShard(ctx context.Context, shardID int, targetNode string) error {
	smm.mu.Lock()
	
	// Check if already migrating
	if _, exists := smm.migratingShards[shardID]; exists {
		smm.mu.Unlock()
		return fmt.Errorf("shard %d is already being migrated", shardID)
	}

	// Create migration state
	state := &ShardMigrationState{
		ShardID:   shardID,
		OldNode:   smm.node.GetAdvertiseAddress(),
		NewNode:   targetNode,
		StartTime: time.Now(),
		Phase:     PhaseBlocking,
	}

	// Collect objects in this shard
	smm.node.objectsMu.RLock()
	for id, obj := range smm.node.objects {
		if sharding.GetShardID(id) == shardID {
			state.ObjectIDs = append(state.ObjectIDs, obj.Id())
		}
	}
	smm.node.objectsMu.RUnlock()

	smm.migratingShards[shardID] = state
	smm.mu.Unlock()

	smm.logger.Infof("Starting migration of shard %d to %s (%d objects)", 
		shardID, targetNode, len(state.ObjectIDs))

	// Phase 1: Blocking - new calls are already blocked by BeginShardCall
	smm.logger.Infof("Shard %d: Phase 1 - Blocking new calls", shardID)

	// Phase 2: Draining - wait for in-flight calls to complete
	smm.updatePhase(shardID, PhaseDraining)
	smm.logger.Infof("Shard %d: Phase 2 - Draining in-flight calls", shardID)
	
	drainCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	if err := smm.waitForShardCallsDrained(drainCtx, shardID); err != nil {
		smm.logger.Errorf("Shard %d: Failed to drain calls: %v", shardID, err)
		smm.abortMigration(shardID)
		return fmt.Errorf("failed to drain shard %d: %w", shardID, err)
	}

	// Phase 3: Persisting - save all objects to persistence
	smm.updatePhase(shardID, PhasePersisting)
	smm.logger.Infof("Shard %d: Phase 3 - Persisting %d objects", shardID, len(state.ObjectIDs))
	
	if err := smm.persistShardObjects(ctx, state.ObjectIDs); err != nil {
		smm.logger.Errorf("Shard %d: Failed to persist objects: %v", shardID, err)
		smm.abortMigration(shardID)
		return fmt.Errorf("failed to persist shard %d objects: %w", shardID, err)
	}

	// Phase 4: Removing - delete objects from memory
	smm.updatePhase(shardID, PhaseRemoving)
	smm.logger.Infof("Shard %d: Phase 4 - Removing %d objects from memory", shardID, len(state.ObjectIDs))
	
	if err := smm.removeShardObjects(ctx, state.ObjectIDs); err != nil {
		smm.logger.Errorf("Shard %d: Failed to remove objects: %v", shardID, err)
		smm.abortMigration(shardID)
		return fmt.Errorf("failed to remove shard %d objects: %w", shardID, err)
	}

	// Phase 5: Complete
	smm.updatePhase(shardID, PhaseComplete)
	smm.logger.Infof("Shard %d: Migration complete (took %v)", 
		shardID, time.Since(state.StartTime))

	// Clean up migration state
	smm.mu.Lock()
	delete(smm.migratingShards, shardID)
	delete(smm.shardCallCounts, shardID)
	smm.mu.Unlock()

	return nil
}

// updatePhase updates the migration phase for a shard
func (smm *ShardMigrationManager) updatePhase(shardID int, phase MigrationPhase) {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	if state, exists := smm.migratingShards[shardID]; exists {
		state.Phase = phase
	}
}

// abortMigration aborts a migration and removes the migration state
func (smm *ShardMigrationManager) abortMigration(shardID int) {
	smm.mu.Lock()
	defer smm.mu.Unlock()

	delete(smm.migratingShards, shardID)
	smm.logger.Warnf("Aborted migration of shard %d", shardID)
}

// persistShardObjects persists all objects in a shard
func (smm *ShardMigrationManager) persistShardObjects(ctx context.Context, objectIDs []string) error {
	smm.node.persistenceProviderMu.RLock()
	provider := smm.node.persistenceProvider
	smm.node.persistenceProviderMu.RUnlock()

	if provider == nil {
		// No persistence configured, skip this step
		smm.logger.Infof("No persistence provider configured, skipping persist phase")
		return nil
	}

	// Save each object
	errorCount := 0
	for _, objectID := range objectIDs {
		// Acquire per-key read lock to prevent concurrent delete
		unlockKey := smm.node.keyLock.RLock(objectID)

		// Get the object
		smm.node.objectsMu.RLock()
		obj, exists := smm.node.objects[objectID]
		smm.node.objectsMu.RUnlock()

		if !exists {
			// Object was deleted, skip it
			unlockKey()
			continue
		}

		// Get object data
		data, err := obj.ToData()
		if err != nil {
			unlockKey()
			// Skip non-persistent objects
			continue
		}

		// Save the object using the helper from object package
		err = object.SaveObject(ctx, provider, obj.Id(), obj.Type(), data)
		unlockKey()

		if err != nil {
			smm.logger.Errorf("Failed to save object %s: %v", obj, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("failed to save %d objects", errorCount)
	}

	return nil
}

// removeShardObjects removes all objects in a shard from memory
func (smm *ShardMigrationManager) removeShardObjects(ctx context.Context, objectIDs []string) error {
	errorCount := 0

	for _, objectID := range objectIDs {
		// Use the node's DeleteObject method which handles locking and persistence deletion
		err := smm.node.DeleteObject(ctx, objectID)
		if err != nil {
			smm.logger.Errorf("Failed to delete object %s: %v", objectID, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("failed to delete %d objects", errorCount)
	}

	return nil
}

// GetMigrationStatus returns the current status of all shard migrations
func (smm *ShardMigrationManager) GetMigrationStatus() map[int]*ShardMigrationState {
	smm.mu.RLock()
	defer smm.mu.RUnlock()

	status := make(map[int]*ShardMigrationState)
	for shardID, state := range smm.migratingShards {
		// Return a copy to avoid race conditions
		stateCopy := *state
		status[shardID] = &stateCopy
	}

	return status
}
