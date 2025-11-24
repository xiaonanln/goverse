package node

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"

	"github.com/xiaonanln/goverse/object"
)

// waitForObjectCreated waits for an object to be created on the specified node
func waitForObjectCreated(t *testing.T, n *Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not created on node within %v", objID, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestObject is a simple test object for testing
type TestObject struct {
	object.BaseObject
}

func (obj *TestObject) OnCreated() {
	// No-op for testing
}

// TestCreateObject_RequiresID tests that CreateObject requires a non-empty ID
func TestCreateObject_RequiresID(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	// Register a test object type
	node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()

	// Test 1: Empty ID should fail
	_, err := node.CreateObject(ctx, "TestObject", "")
	if err == nil {
		t.Fatal("Expected error when creating object with empty ID, got nil")
	}
	expectedMsg := "object ID must be specified"
	if err.Error() != expectedMsg {
		t.Fatalf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}

	// Test 2: Non-empty ID should succeed
	id, err := node.CreateObject(ctx, "TestObject", "test-obj-123")
	if err != nil {
		t.Fatalf("Expected success when creating object with valid ID, got error: %v", err)
	}
	if id != "test-obj-123" {
		t.Fatalf("Expected ID 'test-obj-123', got '%s'", id)
	}

	// Test 3: Verify object was created
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-123"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object should exist after creation")
	}
	if obj.Id() != "test-obj-123" {
		t.Fatalf("Object ID should be 'test-obj-123', got '%s'", obj.Id())
	}
}

// TestCreateObject_DuplicateID tests that creating an object with the same ID and type returns the existing object
func TestCreateObject_DuplicateID(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	// Register a test object type
	node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()

	// Create first object
	id1, err := node.CreateObject(ctx, "TestObject", "duplicate-id")
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}
	if id1 != "duplicate-id" {
		t.Fatalf("Expected ID 'duplicate-id', got '%s'", id1)
	}

	// Try to create second object with same ID and type - should succeed and return existing object
	id2, err := node.CreateObject(ctx, "TestObject", "duplicate-id")
	if err != nil {
		t.Fatalf("Expected success when creating object with duplicate ID and same type, got error: %v", err)
	}
	if id2 != "duplicate-id" {
		t.Fatalf("Expected ID 'duplicate-id', got '%s'", id2)
	}

	// Verify only one object exists
	node.objectsMu.RLock()
	count := 0
	for id := range node.objects {
		if id == "duplicate-id" {
			count++
		}
	}
	node.objectsMu.RUnlock()

	if count != 1 {
		t.Fatalf("Expected exactly 1 object with ID 'duplicate-id', got %d", count)
	}
}

// TestObject2 is a second test object type for testing type mismatches
type TestObject2 struct {
	object.BaseObject
}

func (obj *TestObject2) OnCreated() {
	// No-op for testing
}

// TestCreateObject_DuplicateID_DifferentType tests that creating an object with same ID but different type fails
func TestCreateObject_DuplicateID_DifferentType(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	// Register both test object types
	node.RegisterObjectType((*TestObject)(nil))
	node.RegisterObjectType((*TestObject2)(nil))

	ctx := context.Background()

	// Create object with first type
	_, err := node.CreateObject(ctx, "TestObject", "same-id")
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}

	// Try to create object with same ID but different type - should fail
	_, err = node.CreateObject(ctx, "TestObject2", "same-id")
	if err == nil {
		t.Fatal("Expected error when creating object with same ID but different type, got nil")
	}

	// Error message should indicate type mismatch
	if !strings.Contains(err.Error(), "different type") {
		t.Fatalf("Expected error message to contain 'different type', got '%s'", err.Error())
	}
}

// TestCreateObject_UnknownType tests that creating an object with unknown type fails
func TestCreateObject_UnknownType(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	ctx := context.Background()

	// Try to create object with unregistered type
	_, err := node.CreateObject(ctx, "UnknownType", "test-obj-456")
	if err == nil {
		t.Fatal("Expected error when creating object with unknown type, got nil")
	}

	if !strings.Contains(err.Error(), "unknown object type") {
		t.Fatalf("Expected error message to contain 'unknown object type', got '%s'", err.Error())
	}
}

// TestCreateObject_ConcurrentCalls tests that concurrent CreateObject calls are safe and idempotent
func TestCreateObject_ConcurrentCalls(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	// Register test object type
	node.RegisterObjectType((*TestConcurrencyObject)(nil))

	ctx := context.Background()

	// Test with 50 concurrent goroutines all trying to create the same object
	const numGoroutines = 50
	objectID := "concurrent-test-obj"

	// Channel to collect results
	results := make(chan struct {
		id  string
		err error
		obj Object
	}, numGoroutines)

	// Launch concurrent CreateObject calls
	for i := 0; i < numGoroutines; i++ {
		go func() {
			id, err := node.CreateObject(ctx, "TestConcurrencyObject", objectID)
			results <- struct {
				id  string
				err error
				obj Object
			}{id, err, nil}
		}()
	}

	// Collect all results
	var errors []error
	var ids []string
	for i := 0; i < numGoroutines; i++ {
		result := <-results
		ids = append(ids, result.id)
		if result.err != nil {
			errors = append(errors, result.err)
		}
	}

	// Wait for object to be fully created before accessing it
	waitForObjectCreated(t, node, objectID, 5*time.Second)

	// Now get the objects for verification
	var objects []Object
	for i := 0; i < numGoroutines; i++ {
		node.objectsMu.RLock()
		obj := node.objects[objectID]
		node.objectsMu.RUnlock()
		objects = append(objects, obj)
	}

	// All calls should succeed
	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %d errors: %v", len(errors), errors)
	}

	// All should return the same ID
	for _, id := range ids {
		if id != objectID {
			t.Fatalf("Expected ID '%s', got '%s'", objectID, id)
		}
	}

	// All should return the same object instance (critical for race condition fix)
	if len(objects) > 0 {
		firstObj := objects[0]
		if firstObj == nil {
			t.Fatal("First object is nil")
		}
		for i, obj := range objects {
			if obj == nil {
				t.Fatalf("Object %d is nil", i)
			} else if obj != firstObj {
				t.Fatalf("Object %d (%p) is not the same instance as first object (%p)", i, obj, firstObj)
			}
		}
	}

	// Verify only one object exists in the map
	node.objectsMu.RLock()
	count := 0
	for id := range node.objects {
		if id == objectID {
			count++
		}
	}
	node.objectsMu.RUnlock()

	if count != 1 {
		t.Fatalf("Expected exactly 1 object with ID '%s', got %d", objectID, count)
	}
} // TestCreateObject_ConcurrentDifferentObjects tests concurrent creation of different objects

func TestCreateObject_ConcurrentDifferentObjects(t *testing.T) {
	node := NewNode("test-node:1234", sharding.NumShards)

	// Register test object type
	node.RegisterObjectType((*TestConcurrencyObject)(nil))

	ctx := context.Background()

	// Create 20 different objects concurrently
	const numObjects = 20
	results := make(chan struct {
		id  string
		err error
	}, numObjects)

	// Track all object IDs for waiting
	var createdIDs []string
	for i := 0; i < numObjects; i++ {
		objectID := strings.Repeat("a", i+1) // Different length IDs: "a", "aa", "aaa", etc.
		createdIDs = append(createdIDs, objectID)
		go func(id string) {
			returnedID, err := node.CreateObject(ctx, "TestConcurrencyObject", id)
			results <- struct {
				id  string
				err error
			}{returnedID, err}
		}(objectID)
	}

	// Collect all results
	var errors []error
	successCount := 0
	for i := 0; i < numObjects; i++ {
		result := <-results
		if result.err != nil {
			errors = append(errors, result.err)
		} else {
			successCount++
		}
	}

	// All calls should succeed
	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %d errors: %v", len(errors), errors)
	}

	if successCount != numObjects {
		t.Fatalf("Expected %d successful creations, got %d", numObjects, successCount)
	}

	// Wait for all objects to be fully created
	for _, objID := range createdIDs {
		waitForObjectCreated(t, node, objID, 5*time.Second)
	}

	// Verify all objects exist
	node.objectsMu.RLock()
	actualCount := len(node.objects)
	node.objectsMu.RUnlock()

	if actualCount != numObjects {
		t.Fatalf("Expected %d objects in registry, got %d", numObjects, actualCount)
	}
}

// TestConcurrencyObject is a simple test object for testing concurrency
type TestConcurrencyObject struct {
	object.BaseObject
}

func (tco *TestConcurrencyObject) OnInit(obj object.Object, id string) {
	tco.BaseObject.OnInit(obj, id)
	// Introduce a small, variable busy-wait delay to increase contention in tests.
	// Use object ID length to vary delay without importing extra packages.
	delay := 50000 * (1 + len(tco.Id())%5) // adjustable small work amount
	for i := 0; i < delay; i++ {
		_ = i * i
	}
}

func (tco *TestConcurrencyObject) OnCreated() {
	// No-op for testing
}
