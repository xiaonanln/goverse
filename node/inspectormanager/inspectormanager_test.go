package inspectormanager

import (
	"context"
	"testing"
	"time"
)

func TestInspectorManager_NewInspectorManager(t *testing.T) {
	t.Parallel()

	nodeAddr := "localhost:47000"
	mgr := NewInspectorManager(nodeAddr)

	if mgr == nil {
		t.Fatal("NewInspectorManager returned nil")
	}

	if mgr.nodeAddress != nodeAddr {
		t.Fatalf("Expected nodeAddress %s, got %s", nodeAddr, mgr.nodeAddress)
	}

	if mgr.objects == nil {
		t.Fatal("Objects map should be initialized")
	}

	if mgr.logger == nil {
		t.Fatal("Logger should be initialized")
	}
}

func TestInspectorManager_StartStop(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")
	ctx := context.Background()

	// Start should not fail even if inspector is not available
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Allow some time for the management goroutine to start
	time.Sleep(100 * time.Millisecond)

	// Stop should not fail
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Verify context was canceled
	select {
	case <-mgr.ctx.Done():
		// Expected - context should be canceled
	default:
		t.Fatal("Context should be canceled after Stop")
	}
}

func TestInspectorManager_NotifyObjectAdded(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	objectID := "test-object-1"
	objectType := "TestObject"

	// Add an object
	mgr.NotifyObjectAdded(objectID, objectType)

	// Verify object was stored
	mgr.mu.RLock()
	obj, exists := mgr.objects[objectID]
	mgr.mu.RUnlock()

	if !exists {
		t.Fatal("Object should be stored in manager")
	}

	if obj.Id != objectID {
		t.Fatalf("Expected object ID %s, got %s", objectID, obj.Id)
	}

	if obj.Class != objectType {
		t.Fatalf("Expected object type %s, got %s", objectType, obj.Class)
	}
}

func TestInspectorManager_NotifyObjectRemoved(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	objectID := "test-object-1"
	objectType := "TestObject"

	// Add an object
	mgr.NotifyObjectAdded(objectID, objectType)

	// Verify object was stored
	mgr.mu.RLock()
	_, exists := mgr.objects[objectID]
	mgr.mu.RUnlock()

	if !exists {
		t.Fatal("Object should be stored in manager")
	}

	// Remove the object
	mgr.NotifyObjectRemoved(objectID)

	// Verify object was removed
	mgr.mu.RLock()
	_, exists = mgr.objects[objectID]
	mgr.mu.RUnlock()

	if exists {
		t.Fatal("Object should be removed from manager")
	}
}

func TestInspectorManager_MultipleObjectsTracking(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	objects := []struct {
		id  string
		typ string
	}{
		{"obj-1", "Type1"},
		{"obj-2", "Type2"},
		{"obj-3", "Type3"},
	}

	// Add multiple objects
	for _, obj := range objects {
		mgr.NotifyObjectAdded(obj.id, obj.typ)
	}

	// Verify all objects are tracked
	mgr.mu.RLock()
	count := len(mgr.objects)
	mgr.mu.RUnlock()

	if count != len(objects) {
		t.Fatalf("Expected %d objects, got %d", len(objects), count)
	}

	// Remove one object
	mgr.NotifyObjectRemoved("obj-2")

	// Verify count decreased
	mgr.mu.RLock()
	count = len(mgr.objects)
	mgr.mu.RUnlock()

	if count != len(objects)-1 {
		t.Fatalf("Expected %d objects after removal, got %d", len(objects)-1, count)
	}

	// Verify the right object was removed
	mgr.mu.RLock()
	_, exists := mgr.objects["obj-2"]
	mgr.mu.RUnlock()

	if exists {
		t.Fatal("obj-2 should be removed")
	}

	// Verify other objects still exist
	mgr.mu.RLock()
	_, exists1 := mgr.objects["obj-1"]
	_, exists3 := mgr.objects["obj-3"]
	mgr.mu.RUnlock()

	if !exists1 || !exists3 {
		t.Fatal("Other objects should still exist")
	}
}

func TestInspectorManager_StartStopMultipleTimes(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")
	ctx := context.Background()

	// First start/stop cycle
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("First Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("First Stop failed: %v", err)
	}

	// Create a new manager for second cycle (as the old one is stopped)
	mgr = NewInspectorManager("localhost:47000")

	// Second start/stop cycle
	err = mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Second Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Second Stop failed: %v", err)
	}
}

func TestInspectorManager_ConcurrentNotifications(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	// Concurrently add objects
	numObjects := 100
	done := make(chan bool, numObjects)

	for i := 0; i < numObjects; i++ {
		go func(id int) {
			objID := "concurrent-obj-" + string(rune(id))
			mgr.NotifyObjectAdded(objID, "TestType")
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numObjects; i++ {
		<-done
	}

	// Verify all objects were added (no race conditions)
	mgr.mu.RLock()
	count := len(mgr.objects)
	mgr.mu.RUnlock()

	if count != numObjects {
		t.Fatalf("Expected %d objects, got %d (possible race condition)", numObjects, count)
	}
}

func TestInspectorManager_NotifyBeforeStart(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	// Add objects before starting the manager
	mgr.NotifyObjectAdded("obj-1", "Type1")
	mgr.NotifyObjectAdded("obj-2", "Type2")

	// Verify objects are tracked
	mgr.mu.RLock()
	count := len(mgr.objects)
	mgr.mu.RUnlock()

	if count != 2 {
		t.Fatalf("Expected 2 objects before start, got %d", count)
	}

	// Start should succeed with existing objects
	ctx := context.Background()
	err := mgr.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Objects should still be tracked after start
	mgr.mu.RLock()
	count = len(mgr.objects)
	mgr.mu.RUnlock()

	if count != 2 {
		t.Fatalf("Expected 2 objects after start, got %d", count)
	}

	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestInspectorManager_ShardIDComputation(t *testing.T) {
	t.Parallel()

	mgr := NewInspectorManager("localhost:47000")

	// Add an object
	objectID := "test-object-with-shard"
	objectType := "TestObject"

	mgr.NotifyObjectAdded(objectID, objectType)

	// Verify object was stored with shard ID
	mgr.mu.RLock()
	obj, exists := mgr.objects[objectID]
	mgr.mu.RUnlock()

	if !exists {
		t.Fatal("Object should be stored in manager")
	}

	// The shard ID should be set (computed by sharding.GetShardID)
	// Note: ShardID can be 0, which is a valid shard, so we just verify it's in range

	if obj.ShardId < 0 || obj.ShardId >= 8192 {
		t.Fatalf("ShardId should be in range [0, 8192), got %d", obj.ShardId)
	}
}
