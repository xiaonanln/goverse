package node

import (
	"context"
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

// TestCreateObject_DuplicateID tests that creating an object with an existing ID fails
func TestCreateObject_DuplicateID(t *testing.T) {
	node := NewNode("test-node:1234")
	
	// Register a test object type
	node.RegisterObjectType((*TestObject)(nil))
	
	ctx := context.Background()
	
	// Create first object
	_, err := node.CreateObject(ctx, "TestObject", "duplicate-id", nil)
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}
	
	// Try to create second object with same ID
	_, err = node.CreateObject(ctx, "TestObject", "duplicate-id", nil)
	if err == nil {
		t.Fatal("Expected error when creating object with duplicate ID, got nil")
	}
	
	// Error message should indicate the object already exists
	expectedSubstring := "already exists"
	if !contains(err.Error(), expectedSubstring) {
		t.Errorf("Expected error message to contain '%s', got '%s'", expectedSubstring, err.Error())
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
	
	expectedSubstring := "unknown object type"
	if !contains(err.Error(), expectedSubstring) {
		t.Errorf("Expected error message to contain '%s', got '%s'", expectedSubstring, err.Error())
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
