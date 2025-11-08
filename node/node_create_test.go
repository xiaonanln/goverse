package node

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/object"
)

// TestObject is a simple test object for testing
type TestObject struct {
	object.BaseObject
}

func (obj *TestObject) OnCreated() {
	// No-op for testing
}

// TestCreateObject_RequiresID tests that CreateObject requires a non-empty ID
func TestCreateObject_RequiresID(t *testing.T) {
	node := NewNode("test-node:1234")

	// Register a test object type
	node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()

	// Test 1: Empty ID should fail
	_, err := node.CreateObject(ctx, "TestObject", "", nil)
	if err == nil {
		t.Fatal("Expected error when creating object with empty ID, got nil")
	}
	expectedMsg := "object ID must be specified"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}

	// Test 2: Non-empty ID should succeed
	id, err := node.CreateObject(ctx, "TestObject", "test-obj-123", nil)
	if err != nil {
		t.Fatalf("Expected success when creating object with valid ID, got error: %v", err)
	}
	if id != "test-obj-123" {
		t.Errorf("Expected ID 'test-obj-123', got '%s'", id)
	}

	// Test 3: Verify object was created
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-123"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object should exist after creation")
	}
	if obj.Id() != "test-obj-123" {
		t.Errorf("Object ID should be 'test-obj-123', got '%s'", obj.Id())
	}
}

// TestCreateObject_DuplicateID tests that creating an object with the same ID and type returns the existing object
func TestCreateObject_DuplicateID(t *testing.T) {
	node := NewNode("test-node:1234")

	// Register a test object type
	node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()

	// Create first object
	id1, err := node.CreateObject(ctx, "TestObject", "duplicate-id", nil)
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}
	if id1 != "duplicate-id" {
		t.Errorf("Expected ID 'duplicate-id', got '%s'", id1)
	}

	// Try to create second object with same ID and type - should succeed and return existing object
	id2, err := node.CreateObject(ctx, "TestObject", "duplicate-id", nil)
	if err != nil {
		t.Fatalf("Expected success when creating object with duplicate ID and same type, got error: %v", err)
	}
	if id2 != "duplicate-id" {
		t.Errorf("Expected ID 'duplicate-id', got '%s'", id2)
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
		t.Errorf("Expected exactly 1 object with ID 'duplicate-id', got %d", count)
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
	node := NewNode("test-node:1234")

	// Register both test object types
	node.RegisterObjectType((*TestObject)(nil))
	node.RegisterObjectType((*TestObject2)(nil))

	ctx := context.Background()

	// Create object with first type
	_, err := node.CreateObject(ctx, "TestObject", "same-id", nil)
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}

	// Try to create object with same ID but different type - should fail
	_, err = node.CreateObject(ctx, "TestObject2", "same-id", nil)
	if err == nil {
		t.Fatal("Expected error when creating object with same ID but different type, got nil")
	}

	// Error message should indicate type mismatch
	if !strings.Contains(err.Error(), "different type") {
		t.Errorf("Expected error message to contain 'different type', got '%s'", err.Error())
	}
}

// TestCreateObject_UnknownType tests that creating an object with unknown type fails
func TestCreateObject_UnknownType(t *testing.T) {
	node := NewNode("test-node:1234")

	ctx := context.Background()

	// Try to create object with unregistered type
	_, err := node.CreateObject(ctx, "UnknownType", "test-obj-456", nil)
	if err == nil {
		t.Fatal("Expected error when creating object with unknown type, got nil")
	}

	if !strings.Contains(err.Error(), "unknown object type") {
		t.Errorf("Expected error message to contain 'unknown object type', got '%s'", err.Error())
	}
}
