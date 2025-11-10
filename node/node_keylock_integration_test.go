package node

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
)

// TestNode_CreateDelete_RaceCondition tests that create and delete are properly serialized
func TestNode_CreateDelete_RaceCondition(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	const numOps = 50
	var wg sync.WaitGroup
	var createCount atomic.Int32
	var deleteCount atomic.Int32
	var createErrors atomic.Int32
	var deleteErrors atomic.Int32

	objID := "race-test-obj"

	// Launch concurrent creates and deletes
	for i := 0; i < numOps; i++ {
		wg.Add(2)

		// Create goroutine
		go func() {
			defer wg.Done()
			_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
			if err != nil {
				if err.Error() != "node is stopped" {
					createErrors.Add(1)
				}
			} else {
				createCount.Add(1)
			}
		}()

		// Delete goroutine
		go func() {
			defer wg.Done()
			err := node.DeleteObject(ctx, objID)
			if err != nil {
				if err.Error() != "node is stopped" && err.Error() != "object "+objID+" not found" {
					deleteErrors.Add(1)
				}
			} else {
				deleteCount.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Creates: %d, Deletes: %d, Create errors: %d, Delete errors: %d",
		createCount.Load(), deleteCount.Load(), createErrors.Load(), deleteErrors.Load())

	// All operations should complete without errors (except "not found" which is expected)
	if createErrors.Load() > 0 {
		t.Errorf("Expected no create errors, got %d", createErrors.Load())
	}
	if deleteErrors.Load() > 0 {
		t.Errorf("Expected no delete errors, got %d", deleteErrors.Load())
	}
}

// TestNode_CallDelete_RaceCondition tests that call and delete are properly serialized
func TestNode_CallDelete_RaceCondition(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	objID := "call-delete-race-obj"

	// Pre-create the object
	_, err = node.CreateObject(ctx, "TestPersistentObjectWithMethod", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	const numOps = 50
	var wg sync.WaitGroup
	var callSuccess atomic.Int32
	var callErrors atomic.Int32
	var deleteSuccess atomic.Int32
	var deleteErrors atomic.Int32

	// Launch concurrent calls and deletes
	for i := 0; i < numOps; i++ {
		wg.Add(2)

		// Call goroutine
		go func() {
			defer wg.Done()
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", objID, "GetValue", &emptypb.Empty{})
			if err != nil {
				// Expected errors: object deleted or node stopped
				if err.Error() != "node is stopped" && err.Error() != "object "+objID+" was deleted" {
					callErrors.Add(1)
					t.Logf("Unexpected call error: %v", err)
				}
			} else {
				callSuccess.Add(1)
			}
		}()

		// Delete goroutine
		go func() {
			defer wg.Done()
			err := node.DeleteObject(ctx, objID)
			if err != nil {
				// Expected errors: already deleted or node stopped
				if err.Error() != "node is stopped" && err.Error() != "object "+objID+" not found" {
					deleteErrors.Add(1)
					t.Logf("Unexpected delete error: %v", err)
				}
			} else {
				deleteSuccess.Add(1)
			}
		}()

		// Small delay to let some operations complete
		if i%10 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	wg.Wait()

	t.Logf("Call success: %d, Call errors: %d, Delete success: %d, Delete errors: %d",
		callSuccess.Load(), callErrors.Load(), deleteSuccess.Load(), deleteErrors.Load())

	// Should have no unexpected errors
	if callErrors.Load() > 0 {
		t.Errorf("Expected no unexpected call errors, got %d", callErrors.Load())
	}
	if deleteErrors.Load() > 0 {
		t.Errorf("Expected no unexpected delete errors, got %d", deleteErrors.Load())
	}
}

// TestNode_SaveDelete_RaceCondition tests that save and delete are properly serialized
func TestNode_SaveDelete_RaceCondition(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create some objects
	for i := 0; i < 5; i++ {
		objID := "save-delete-race-obj"
		_, err := node.createObject(ctx, "TestPersistentObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}
	}

	const numOps = 20
	var wg sync.WaitGroup
	var saveSuccess atomic.Int32
	var saveErrors atomic.Int32
	var deleteSuccess atomic.Int32
	var deleteErrors atomic.Int32

	objID := "save-delete-race-obj"

	// Launch concurrent saves and deletes
	for i := 0; i < numOps; i++ {
		wg.Add(2)

		// Save goroutine
		go func() {
			defer wg.Done()
			err := node.SaveAllObjects(ctx)
			if err != nil {
				if err.Error() != "node is stopped" {
					saveErrors.Add(1)
					t.Logf("Unexpected save error: %v", err)
				}
			} else {
				saveSuccess.Add(1)
			}
		}()

		// Delete goroutine
		go func() {
			defer wg.Done()
			err := node.DeleteObject(ctx, objID)
			if err != nil {
				if err.Error() != "node is stopped" && err.Error() != "object "+objID+" not found" {
					deleteErrors.Add(1)
					t.Logf("Unexpected delete error: %v", err)
				}
			} else {
				deleteSuccess.Add(1)
			}
		}()

		// Small delay
		if i%5 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	wg.Wait()

	t.Logf("Save success: %d, Save errors: %d, Delete success: %d, Delete errors: %d",
		saveSuccess.Load(), saveErrors.Load(), deleteSuccess.Load(), deleteErrors.Load())

	// Should have no unexpected errors
	if saveErrors.Load() > 0 {
		t.Errorf("Expected no unexpected save errors, got %d", saveErrors.Load())
	}
	if deleteErrors.Load() > 0 {
		t.Errorf("Expected no unexpected delete errors, got %d", deleteErrors.Load())
	}
}

// TestNode_ConcurrentCreatesSameID tests that concurrent creates of the same ID are properly serialized
func TestNode_ConcurrentCreatesSameID(t *testing.T) {
	node := NewNode("localhost:47000")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	const numGoroutines = 50
	objID := "concurrent-create-same-id"

	var wg sync.WaitGroup
	results := make(chan struct {
		id  string
		err error
		obj Object
	}, numGoroutines)

	// Launch concurrent CreateObject calls for the same ID
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := node.CreateObject(ctx, "TestPersistentObject", objID)

			// Get the object
			node.objectsMu.RLock()
			obj := node.objects[objID]
			node.objectsMu.RUnlock()

			results <- struct {
				id  string
				err error
				obj Object
			}{id, err, obj}
		}()
	}

	wg.Wait()
	close(results)

	// Collect results
	var successCount int
	var firstObj Object
	for result := range results {
		if result.err == nil {
			successCount++
			if firstObj == nil {
				firstObj = result.obj
			} else if result.obj != firstObj {
				t.Errorf("Different object instances returned for same ID")
			}
		}
	}

	// All should succeed (idempotent)
	if successCount != numGoroutines {
		t.Errorf("Expected all %d creates to succeed, got %d", numGoroutines, successCount)
	}

	// Verify only one object exists
	node.objectsMu.RLock()
	count := 0
	for id := range node.objects {
		if id == objID {
			count++
		}
	}
	node.objectsMu.RUnlock()

	if count != 1 {
		t.Errorf("Expected exactly 1 object with ID '%s', got %d", objID, count)
	}
}

// TestNode_KeyLockMemoryLeak tests that per-key locks are properly cleaned up
func TestNode_KeyLockMemoryLeak(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create and delete many objects
	for i := 0; i < 100; i++ {
		objID := "leak-test-obj"
		_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object %d: %v", i, err)
		}

		err = node.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to delete object %d: %v", i, err)
		}
	}

	// Check that KeyLock map is empty (no memory leak)
	node.keyLock.mu.Lock()
	lockCount := len(node.keyLock.locks)
	node.keyLock.mu.Unlock()

	if lockCount > 0 {
		t.Errorf("Expected KeyLock map to be empty, but found %d entries (memory leak)", lockCount)
	}
}

// TestNode_ConcurrentCallsSameObject tests concurrent calls to the same object don't interfere
func TestNode_ConcurrentCallsSameObject(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	objID := "concurrent-call-obj"

	// Pre-create the object
	_, err = node.CreateObject(ctx, "TestPersistentObjectWithMethod", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	const numCalls = 100
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Launch concurrent calls to the same object
	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", objID, "GetValue", &emptypb.Empty{})
			if err != nil {
				errorCount.Add(1)
				t.Logf("Call error: %v", err)
			} else {
				successCount.Add(1)
			}
		}()
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("Expected no errors, got %d errors", errorCount.Load())
	}
	if successCount.Load() != numCalls {
		t.Errorf("Expected %d successful calls, got %d", numCalls, successCount.Load())
	}
}
