package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
)

// TestStop_RaceWithCreateObject tests that CreateObject operations in flight when Stop is called
// complete safely and that new CreateObject calls after Stop fail gracefully
func TestStop_RaceWithCreateObject(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Launch multiple concurrent CreateObject operations
	const numGoroutines = 20
	results := make(chan struct {
		id  string
		err error
	}, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			objID := "stop-race-obj"
			if index < 10 {
				objID = "stop-race-obj-early"
			}
			// Add a small delay to let some operations start before Stop
			if index > 5 {
				time.Sleep(10 * time.Millisecond)
			}
			id, err := node.CreateObject(ctx, "TestPersistentObject", objID)
			results <- struct {
				id  string
				err error
			}{id, err}
		}(i)
	}

	// Wait a bit for some operations to start
	time.Sleep(5 * time.Millisecond)

	// Stop the node while operations are in flight
	stopErr := node.Stop(ctx)
	if stopErr != nil {
		t.Fatalf("Failed to stop node: %v", stopErr)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect results
	successCount := 0
	stoppedCount := 0
	for result := range results {
		if result.err == nil {
			successCount++
		} else if result.err.Error() == "node is stopped" {
			stoppedCount++
		} else {
			t.Logf("Unexpected error: %v", result.err)
		}
	}

	t.Logf("Results: %d successful, %d stopped", successCount, stoppedCount)

	// At least some operations should have been stopped
	if stoppedCount == 0 {
		t.Error("Expected at least some operations to be stopped")
	}

	// Verify node is properly stopped - objects should be cleared
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after stop, got %d", node.NumObjects())
	}
}

// TestStop_RaceWithCallObject tests that CallObject operations in flight when Stop is called
// complete safely and that new CallObject calls after Stop fail gracefully
func TestStop_RaceWithCallObject(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Pre-create an object
	err = node.createObject(ctx, "TestPersistentObjectWithMethod", "call-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Launch multiple concurrent CallObject operations
	const numGoroutines = 20
	results := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			// Add a small delay to let some operations start before Stop
			if index > 5 {
				time.Sleep(10 * time.Millisecond)
			}
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", "call-obj-1", "GetValue", &emptypb.Empty{})
			results <- err
		}(i)
	}

	// Wait a bit for some operations to start
	time.Sleep(5 * time.Millisecond)

	// Stop the node while operations are in flight
	stopErr := node.Stop(ctx)
	if stopErr != nil {
		t.Fatalf("Failed to stop node: %v", stopErr)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect results
	successCount := 0
	stoppedCount := 0
	for err := range results {
		if err == nil {
			successCount++
		} else if err.Error() == "node is stopped" {
			stoppedCount++
		} else {
			t.Logf("Unexpected error: %v", err)
		}
	}

	t.Logf("Results: %d successful, %d stopped", successCount, stoppedCount)

	// At least some operations should have been stopped
	if stoppedCount == 0 {
		t.Error("Expected at least some operations to be stopped")
	}

	// Verify node is properly stopped
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after stop, got %d", node.NumObjects())
	}
}

// TestStop_RaceWithDeleteObject tests that DeleteObject operations in flight when Stop is called
// complete safely and that new DeleteObject calls after Stop fail gracefully
func TestStop_RaceWithDeleteObject(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Pre-create multiple objects
	for i := 0; i < 10; i++ {
		err = node.createObject(ctx, "TestPersistentObject", "delete-race-obj")
		if err != nil {
			t.Fatalf("Failed to create object %d: %v", i, err)
		}
	}

	// Save them to persistence
	err = node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Launch multiple concurrent DeleteObject operations
	const numGoroutines = 20
	results := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			// Add a small delay to let some operations start before Stop
			if index > 5 {
				time.Sleep(10 * time.Millisecond)
			}
			err := node.DeleteObject(ctx, "delete-race-obj")
			results <- err
		}(i)
	}

	// Wait a bit for some operations to start
	time.Sleep(5 * time.Millisecond)

	// Stop the node while operations are in flight
	stopErr := node.Stop(ctx)
	if stopErr != nil {
		t.Fatalf("Failed to stop node: %v", stopErr)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect results
	// With idempotent DeleteObject, all operations should succeed:
	// - Operations that complete before Stop will delete the object
	// - Operations that run after Stop will succeed because node is stopped (idempotent)
	// - Operations that run after first delete will succeed because object doesn't exist (idempotent)
	successCount := 0
	for err := range results {
		if err == nil {
			successCount++
		} else {
			t.Errorf("Unexpected error from DeleteObject: %v", err)
		}
	}

	t.Logf("Results: %d successful deletions (idempotent)", successCount)

	// All operations should succeed due to idempotency
	if successCount != 20 {
		t.Errorf("Expected all 20 DeleteObject calls to succeed, got %d", successCount)
	}

	// Verify node is properly stopped
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after stop, got %d", node.NumObjects())
	}
}

// TestStop_RaceWithSaveAllObjects tests that SaveAllObjects operations in flight when Stop is called
// complete safely and that new SaveAllObjects calls after Stop fail gracefully
func TestStop_RaceWithSaveAllObjects(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Pre-create multiple objects
	for i := 0; i < 5; i++ {
		err := node.createObject(ctx, "TestPersistentObject", "save-race-obj")
		if err != nil {
			t.Fatalf("Failed to create object %d: %v", i, err)
		}
		// Fetch the object from the map
		node.objectsMu.RLock()
		obj := node.objects["save-race-obj"]
		node.objectsMu.RUnlock()
		persistentObj := obj.(*TestPersistentObject)
		persistentObj.SetValue("test-value")
	}

	// Launch multiple concurrent SaveAllObjects operations
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			// Add a small delay to let some operations start before Stop
			if index > 3 {
				time.Sleep(10 * time.Millisecond)
			}
			err := node.SaveAllObjects(ctx)
			results <- err
		}(i)
	}

	// Wait a bit for some operations to start
	time.Sleep(5 * time.Millisecond)

	// Stop the node while operations are in flight
	stopErr := node.Stop(ctx)
	if stopErr != nil {
		t.Fatalf("Failed to stop node: %v", stopErr)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(results)

	// Collect results
	successCount := 0
	stoppedCount := 0
	for err := range results {
		if err == nil {
			successCount++
		} else if err.Error() == "node is stopped" {
			stoppedCount++
		} else {
			t.Logf("Unexpected error: %v", err)
		}
	}

	t.Logf("Results: %d successful, %d stopped", successCount, stoppedCount)

	// At least some operations should have been stopped
	if stoppedCount == 0 {
		t.Error("Expected at least some operations to be stopped")
	}

	// Verify node is properly stopped
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after stop, got %d", node.NumObjects())
	}

	// Verify objects were saved (either by the operations or by Stop's final save)
	if provider.GetStorageCount() == 0 {
		t.Error("Expected objects to be saved to persistence")
	}
}

// TestStop_NoNewOperationsAfterStop tests that no operations can start after Stop completes
func TestStop_NoNewOperationsAfterStop(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Stop the node
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Try CreateObject - should fail
	_, err = node.CreateObject(ctx, "TestPersistentObject", "after-stop-obj")
	if err == nil {
		t.Error("Expected CreateObject to fail after stop")
	} else if err.Error() != "node is stopped" {
		t.Errorf("Expected 'node is stopped' error, got: %v", err)
	}

	// Try CallObject - should fail
	_, err = node.CallObject(ctx, "TestPersistentObjectWithMethod", "after-stop-obj", "GetValue", &emptypb.Empty{})
	if err == nil {
		t.Error("Expected CallObject to fail after stop")
	} else if err.Error() != "node is stopped" {
		t.Errorf("Expected 'node is stopped' error, got: %v", err)
	}

	// Try DeleteObject - should succeed (idempotent: node stopped = objects cleared)
	// Unlike other operations, DeleteObject succeeds when node is stopped because
	// the desired state (object not existing) is already achieved
	err = node.DeleteObject(ctx, "after-stop-obj")
	if err != nil {
		t.Errorf("Expected DeleteObject to succeed after stop (idempotent), got error: %v", err)
	}

	// Try SaveAllObjects - should fail
	err = node.SaveAllObjects(ctx)
	if err == nil {
		t.Error("Expected SaveAllObjects to fail after stop")
	} else if err.Error() != "node is stopped" {
		t.Errorf("Expected 'node is stopped' error, got: %v", err)
	}
}

// TestStop_ObjectsPersistedBeforeClearing tests that objects are saved before being cleared
func TestStop_ObjectsPersistedBeforeClearing(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create multiple objects
	objectIDs := []string{"persist-obj-1", "persist-obj-2", "persist-obj-3"}
	for _, id := range objectIDs {
		err := node.createObject(ctx, "TestPersistentObject", id)
		if err != nil {
			t.Fatalf("Failed to create object %s: %v", id, err)
		}
		// Fetch the object from the map
		node.objectsMu.RLock()
		obj := node.objects[id]
		node.objectsMu.RUnlock()
		persistentObj := obj.(*TestPersistentObject)
		persistentObj.SetValue(id + "-value")
	}

	// Stop the node
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Verify all objects were persisted
	for _, id := range objectIDs {
		if !provider.HasStoredData(id) {
			t.Errorf("Object %s should be persisted before clearing", id)
		}
	}

	// Verify objects were cleared from memory
	if node.NumObjects() != 0 {
		t.Errorf("Expected 0 objects after stop, got %d", node.NumObjects())
	}
}

// TestStop_MultipleStopCalls tests that calling Stop multiple times is safe
func TestStop_MultipleStopCalls(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// First stop
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("First stop failed: %v", err)
	}

	// Second stop - should not panic or cause issues
	err = node.Stop(ctx)
	if err != nil {
		// It's okay if it returns an error, as long as it doesn't panic
		t.Logf("Second stop returned error: %v", err)
	}
}
