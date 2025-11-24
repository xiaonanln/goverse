package node

import (
	"context"
	"testing"
)

// TestDeleteObject_Integration is an end-to-end integration test showing the complete flow
// of creating an object, saving it to persistence, deleting it, and verifying removal
func TestDeleteObject_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a node with persistence
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Step 1: Create a persistent object
	err := node.createObject(ctx, "TestPersistentObject", "integration-test-obj", 0)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Fetch the object from the map
	node.objectsMu.RLock()
	obj := node.objects["integration-test-obj"]
	node.objectsMu.RUnlock()

	// Set some data on the object
	persistentObj := obj.(*TestPersistentObject)
	persistentObj.SetValue("important-data")

	// Step 2: Save the object to persistence
	err = node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Verify the object is in memory
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object in memory, got %d", node.NumObjects())
	}

	// Verify the object is in persistence
	if !provider.HasStoredData("integration-test-obj") {
		t.Fatal("Object should be stored in persistence")
	}

	// Step 3: Delete the object using DeleteObject
	err = node.DeleteObject(ctx, "integration-test-obj", 0)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Step 4: Verify the object is removed from memory
	if node.NumObjects() != 0 {
		t.Fatalf("Expected 0 objects in memory after deletion, got %d", node.NumObjects())
	}

	// Step 5: Verify the object is removed from persistence
	if provider.HasStoredData("integration-test-obj") {
		t.Fatal("Object should be removed from persistence after deletion")
	}

	// Step 6: Verify deletion is idempotent - can delete non-existent object without error
	err = node.DeleteObject(ctx, "integration-test-obj", 0)
	if err != nil {
		t.Fatalf("Expected no error when deleting already-deleted object (idempotent), got: %v", err)
	}
}

// TestDeleteObject_Integration_NonPersistent demonstrates deletion of non-persistent objects
func TestDeleteObject_Integration_NonPersistent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a node with persistence
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	ctx := context.Background()

	// Step 1: Create a non-persistent object
	err := node.createObject(ctx, "TestNonPersistentObject", "non-persist-integration-obj", 0)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Fetch the object from the map
	node.objectsMu.RLock()
	obj := node.objects["non-persist-integration-obj"]
	node.objectsMu.RUnlock()

	// Set some data on the object
	nonPersistentObj := obj.(*TestNonPersistentObject)
	nonPersistentObj.Value = "temporary-data"

	// Verify object is in memory
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object in memory, got %d", node.NumObjects())
	}

	// Step 2: Delete the object
	err = node.DeleteObject(ctx, "non-persist-integration-obj", 0)
	if err != nil {
		t.Fatalf("Failed to delete non-persistent object: %v", err)
	}

	// Step 3: Verify the object is removed from memory
	if node.NumObjects() != 0 {
		t.Fatalf("Expected 0 objects in memory after deletion, got %d", node.NumObjects())
	}

	// Step 4: Verify nothing was stored in persistence (non-persistent objects shouldn't be stored)
	if provider.GetStorageCount() != 0 {
		t.Fatalf("Expected 0 items in persistence storage, got %d", provider.GetStorageCount())
	}
}

// TestDeleteObject_Integration_MixedObjects demonstrates deletion in a scenario with both
// persistent and non-persistent objects
func TestDeleteObject_Integration_MixedObjects(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	// Create a node with persistence
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	ctx := context.Background()

	// Create 3 persistent objects
	for i := 1; i <= 3; i++ {
		id := objID("persist", i)
		err := node.createObject(ctx, "TestPersistentObject", id, 0)
		if err != nil {
			t.Fatalf("Failed to create persistent object %d: %v", i, err)
		}
		// Fetch the object from the map
		node.objectsMu.RLock()
		obj := node.objects[id]
		node.objectsMu.RUnlock()
		persistentObj := obj.(*TestPersistentObject)
		persistentObj.SetValue(objValue("value", i))
	}

	// Create 2 non-persistent objects
	for i := 1; i <= 2; i++ {
		id := objID("non-persist", i)
		err := node.createObject(ctx, "TestNonPersistentObject", id, 0)
		if err != nil {
			t.Fatalf("Failed to create non-persistent object %d: %v", i, err)
		}
		// Fetch the object from the map
		node.objectsMu.RLock()
		obj := node.objects[id]
		node.objectsMu.RUnlock()
		nonPersistentObj := obj.(*TestNonPersistentObject)
		nonPersistentObj.Value = objValue("temp", i)
	}

	// Save all objects to persistence
	err := node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Verify initial state
	if node.NumObjects() != 5 {
		t.Fatalf("Expected 5 objects in memory, got %d", node.NumObjects())
	}
	if provider.GetStorageCount() != 3 {
		t.Fatalf("Expected 3 objects in persistence, got %d", provider.GetStorageCount())
	}

	// Delete one persistent object
	err = node.DeleteObject(ctx, "persist-2", 0)
	if err != nil {
		t.Fatalf("Failed to delete persistent object: %v", err)
	}

	// Delete one non-persistent object
	err = node.DeleteObject(ctx, "non-persist-1", 0)
	if err != nil {
		t.Fatalf("Failed to delete non-persistent object: %v", err)
	}

	// Verify final state
	if node.NumObjects() != 3 {
		t.Fatalf("Expected 3 objects remaining in memory, got %d", node.NumObjects())
	}
	if provider.GetStorageCount() != 2 {
		t.Fatalf("Expected 2 objects remaining in persistence, got %d", provider.GetStorageCount())
	}

	// Verify correct objects remain in persistence
	if !provider.HasStoredData("persist-1") {
		t.Fatal("persist-1 should still be in persistence")
	}
	if provider.HasStoredData("persist-2") {
		t.Fatal("persist-2 should be deleted from persistence")
	}
	if !provider.HasStoredData("persist-3") {
		t.Fatal("persist-3 should still be in persistence")
	}
}

// Helper function to generate object IDs
func objID(prefix string, num int) string {
	return prefix + "-" + string(rune('0'+num))
}

// Helper function to generate object values
func objValue(prefix string, num int) string {
	return prefix + "-" + string(rune('0'+num))
}
