package node

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
)

// TestKeyLockIntegration_CreateDeleteRace tests that create and delete operations
// on the same object are properly serialized
func TestKeyLockIntegration_CreateDeleteRace(t *testing.T) {
	node := NewNode("localhost:47000")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	const numGoroutines = 20
	const numIterations = 10
	var successCount atomic.Int32

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				objID := "race-obj"

				// Try to create
				_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
				if err == nil {
					successCount.Add(1)

					// Delete it
					err = node.DeleteObject(ctx, objID)
					if err != nil {
						// Object might have been deleted by another goroutine
						t.Logf("Delete failed: %v", err)
					}
				}

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Successfully created object %d times", successCount.Load())

	// Should have at least some successful creates
	if successCount.Load() == 0 {
		t.Fatal("Expected at least some successful creates")
	}
}

// TestKeyLockIntegration_CallDuringDelete tests that calling an object method
// while it's being deleted is properly handled
func TestKeyLockIntegration_CallDuringDelete(t *testing.T) {
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

	const numObjects = 10
	const numCallers = 5

	// Create objects
	for i := 0; i < numObjects; i++ {
		objID := "call-delete-obj"
		err := node.createObject(ctx, "TestPersistentObjectWithMethod", objID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(numCallers + 1)

	// Launch callers
	for i := 0; i < numCallers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", "call-delete-obj", "GetValue", &emptypb.Empty{})
				if err != nil {
					// Object might have been deleted
					if err.Error() != "object call-delete-obj not found" {
						t.Logf("Call error: %v", err)
					}
				}
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// Launch deleter
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond) // Let some calls happen first
		err := node.DeleteObject(ctx, "call-delete-obj")
		if err != nil {
			t.Logf("Delete error: %v", err)
		}
	}()

	wg.Wait()

	// Verify object was deleted from persistence
	if provider.HasStoredData("call-delete-obj") {
		t.Fatal("Object should have been deleted from persistence")
	}
}

// TestKeyLockIntegration_SaveDuringDelete tests that saving objects
// while some are being deleted is properly handled
func TestKeyLockIntegration_SaveDuringDelete(t *testing.T) {
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

	// Create multiple objects
	for i := 0; i < 10; i++ {
		objID := "save-delete-obj"
		err := node.createObject(ctx, "TestPersistentObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Launch saver
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			err := node.SaveAllObjects(ctx)
			if err != nil {
				t.Logf("Save error: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Launch deleter
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		for i := 0; i < 5; i++ {
			err := node.DeleteObject(ctx, "save-delete-obj")
			if err != nil {
				t.Logf("Delete error: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestKeyLockIntegration_ConcurrentCallsSameObject tests that multiple concurrent
// calls to the same object work correctly
func TestKeyLockIntegration_ConcurrentCallsSameObject(t *testing.T) {
	node := NewNode("localhost:47000")
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create object
	err = node.createObject(ctx, "TestPersistentObjectWithMethod", "concurrent-call-obj")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	const numCallers = 20
	const numCalls = 50

	var successCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(numCallers)

	for i := 0; i < numCallers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numCalls; j++ {
				_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", "concurrent-call-obj", "GetValue", &emptypb.Empty{})
				if err == nil {
					successCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	expectedCalls := numCallers * numCalls
	if successCount.Load() != int32(expectedCalls) {
		t.Fatalf("Expected %d successful calls, got %d", expectedCalls, successCount.Load())
	}
}

// TestKeyLockIntegration_CreateCallDeleteSequence tests a realistic sequence
// of create, call, and delete operations
func TestKeyLockIntegration_CreateCallDeleteSequence(t *testing.T) {
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

	const numSequences = 50

	for i := 0; i < numSequences; i++ {
		objID := "sequence-obj"

		// Create
		_, err := node.CreateObject(ctx, "TestPersistentObjectWithMethod", objID)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}

		// Wait for object to be created (CreateObject is async)
		waitForObjectCreated(t, node, objID, 5*time.Second)

		// Call multiple times
		for j := 0; j < 5; j++ {
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", objID, "GetValue", &emptypb.Empty{})
			if err != nil {
				t.Fatalf("Failed to call object: %v", err)
			}
		}

		// Save
		err = node.SaveAllObjects(ctx)
		if err != nil {
			t.Fatalf("Failed to save objects: %v", err)
		}

		// Delete
		err = node.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to delete object: %v", err)
		}

		// Verify deleted from persistence
		if provider.HasStoredData(objID) {
			t.Fatal("Object should be deleted from persistence")
		}
	}

	t.Logf("Successfully completed %d create-call-delete sequences", numSequences)
}

// TestKeyLockIntegration_NoLockLeaks tests that per-key locks are properly cleaned up
func TestKeyLockIntegration_NoLockLeaks(t *testing.T) {
	node := NewNode("localhost:47000")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	initialLocks := node.keyLock.Len()

	// Create and delete many objects
	for i := 0; i < 100; i++ {
		objID := "leak-test-obj"
		_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
		if err == nil {
			// Wait for object to be created (CreateObject is async)
			waitForObjectCreated(t, node, objID, 5*time.Second)

			err = node.DeleteObject(ctx, objID)
			if err != nil {
				t.Logf("Delete error: %v", err)
			}
		}
	}

	// Give some time for cleanup
	time.Sleep(10 * time.Millisecond)

	finalLocks := node.keyLock.Len()
	if finalLocks > initialLocks {
		t.Fatalf("Lock leak detected: initial=%d, final=%d", initialLocks, finalLocks)
	}
}
