package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
)

// TestCreateObject_LocalNode tests CreateObject when the object should be created on the local node
func TestCreateObject_LocalNode(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_LocalNode")
	c.thisNode = node.NewNode("localhost:7000")

	// Register a simple object type for testing
	c.thisNode.RegisterObjectType((*testObject)(nil))

	// When shard mapping is not available, CreateObject should create locally
	ctx := context.Background()
	objID, err := c.CreateObject(ctx, "testObject", "testObject-123", nil)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	if objID == "" {
		t.Error("CreateObject returned empty ID")
	}

	// Verify the object was created
	if c.thisNode.NumObjects() != 1 {
		t.Errorf("Expected 1 object, got %d", c.thisNode.NumObjects())
	}
}

// TestCreateObject_GeneratedID tests CreateObject with auto-generated ID
func TestCreateObject_GeneratedID(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_GeneratedID")
	c.thisNode = node.NewNode("localhost:7001")

	// Register a simple object type for testing
	c.thisNode.RegisterObjectType((*testObject)(nil))

	// Create object without specifying ID
	ctx := context.Background()
	objID, err := c.CreateObject(ctx, "testObject", "", nil)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	if objID == "" {
		t.Error("CreateObject should generate an ID")
	}

	// Verify the object was created
	if c.thisNode.NumObjects() != 1 {
		t.Errorf("Expected 1 object, got %d", c.thisNode.NumObjects())
	}
}

// TestCreateObject_NoNode tests that CreateObject fails when thisNode is not set
func TestCreateObject_NoNode(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_NoNode")

	ctx := context.Background()
	_, err := c.CreateObject(ctx, "testObject", "testObject-123", nil)
	if err == nil {
		t.Error("CreateObject should fail when thisNode is not set")
	}
}

// TestCallObject_NoNode tests that CallObject fails when thisNode is not set
func TestCallObject_NoNode(t *testing.T) {
	c := newClusterForTesting("TestCallObject_NoNode")

	ctx := context.Background()
	_, err := c.CallObject(ctx, "test-obj", "Echo", nil)
	if err == nil {
		t.Error("CallObject should fail when thisNode is not set")
	}
}

// Mock object type for testing
type testObject struct {
	object.BaseObject
}

func (o *testObject) OnCreated() {}



