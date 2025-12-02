package node

import (
	"context"
	"testing"
	"time"
)

// TestInspectorManager_Integration tests the full integration of InspectorManager with Node
func TestInspectorManager_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	node := NewNode("localhost:47100", testNumShards, "")

	// Register a test object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create objects before starting the node
	ctx := context.Background()
	err := node.createObject(ctx, "TestPersistentObject", "pre-start-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object before start: %v", err)
	}

	// Verify object was created
	node.objectsMu.RLock()
	_, exists := node.objects["pre-start-obj-1"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object should exist before start")
	}

	// Start the node - this should register existing objects with inspector manager
	err = node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Give some time for the manager to start
	time.Sleep(100 * time.Millisecond)

	// Verify existing object was registered with inspector manager
	if !node.inspectorManager.IsObjectTracked("pre-start-obj-1") {
		t.Fatal("Existing object should be tracked by inspector manager after start")
	}

	// Create a new object after start
	err = node.createObject(ctx, "TestPersistentObject", "post-start-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object after start: %v", err)
	}

	// Verify new object is tracked
	if !node.inspectorManager.IsObjectTracked("post-start-obj-1") {
		t.Fatal("New object should be tracked by inspector manager")
	}

	// Delete an object
	node.destroyObject("pre-start-obj-1")

	// Verify object is no longer tracked
	if node.inspectorManager.IsObjectTracked("pre-start-obj-1") {
		t.Fatal("Deleted object should not be tracked by inspector manager")
	}

	// Stop the node
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Verify manager stopped
	ctx2 := node.inspectorManager.GetContextForTesting()
	if ctx2 != nil {
		select {
		case <-ctx2.Done():
			// Expected - context should be done
		default:
			t.Fatal("Inspector manager context should be done after stop")
		}
	}
}

// TestInspectorManager_Integration_MultipleStartStop tests multiple start/stop cycles
func TestInspectorManager_Integration_MultipleStartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	node := NewNode("localhost:47101", testNumShards, "")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// First cycle
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("First start failed: %v", err)
	}

	err = node.createObject(ctx, "TestPersistentObject", "cycle1-obj")
	if err != nil {
		t.Fatalf("Failed to create object in cycle 1: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("First stop failed: %v", err)
	}

	// Verify all objects cleared
	node.objectsMu.RLock()
	count := len(node.objects)
	node.objectsMu.RUnlock()

	if count != 0 {
		t.Fatalf("Expected 0 objects after stop, got %d", count)
	}

	// Create new node for second cycle
	node = NewNode("localhost:47101", testNumShards, "")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Second cycle
	err = node.Start(ctx)
	if err != nil {
		t.Fatalf("Second start failed: %v", err)
	}

	err = node.createObject(ctx, "TestPersistentObject", "cycle2-obj")
	if err != nil {
		t.Fatalf("Failed to create object in cycle 2: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Second stop failed: %v", err)
	}
}

// TestInspectorManager_Integration_ConcurrentObjectOps tests concurrent object operations
func TestInspectorManager_Integration_ConcurrentObjectOps(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	node := NewNode("localhost:47102", testNumShards, "")
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Concurrently create objects
	numObjects := 50
	done := make(chan bool, numObjects)

	for i := 0; i < numObjects; i++ {
		go func(id int) {
			objID := "concurrent-integration-obj-" + string(rune(id))
			err := node.createObject(ctx, "TestPersistentObject", objID)
			if err != nil {
				t.Errorf("Failed to create object %s: %v", objID, err)
			}
			done <- true
		}(i)
	}

	// Wait for all objects to be created
	for i := 0; i < numObjects; i++ {
		<-done
	}

	// Verify all objects are tracked by inspector manager
	trackedCount := node.inspectorManager.ObjectCount()

	if trackedCount != numObjects {
		t.Fatalf("Expected %d tracked objects, got %d", numObjects, trackedCount)
	}

	// Concurrently delete half the objects
	for i := 0; i < numObjects/2; i++ {
		go func(id int) {
			objID := "concurrent-integration-obj-" + string(rune(id))
			node.destroyObject(objID)
			done <- true
		}(i)
	}

	// Wait for deletions
	for i := 0; i < numObjects/2; i++ {
		<-done
	}

	// Give time for notifications to propagate
	time.Sleep(100 * time.Millisecond)

	// Verify correct number of objects tracked
	trackedCount = node.inspectorManager.ObjectCount()

	expectedCount := numObjects - numObjects/2
	if trackedCount != expectedCount {
		t.Fatalf("Expected %d tracked objects after deletions, got %d", expectedCount, trackedCount)
	}

	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
