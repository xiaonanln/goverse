package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
)

// TestCreateObject_NoShardMapping tests that CreateObject fails when shard mapping is not available
func TestCreateObject_NoShardMapping(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_NoShardMapping")
	c.thisNode = node.NewNode("localhost:7000")

	// Register a simple object type for testing
	c.thisNode.RegisterObjectType((*testObject)(nil))

	// When shard mapping is not available, CreateObject should return error
	ctx := context.Background()
	_, err := c.CreateObject(ctx, "testObject", "testObject-123", nil)
	if err == nil {
		t.Error("CreateObject should fail when shard mapping is not available")
	}
	
	// Verify no object was created
	if c.thisNode.NumObjects() != 0 {
		t.Errorf("Expected 0 objects, got %d", c.thisNode.NumObjects())
	}
}

// TestCreateObject_GeneratedID_NoShardMapping tests CreateObject with auto-generated ID when shard mapping is not available
func TestCreateObject_GeneratedID_NoShardMapping(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_GeneratedID_NoShardMapping")
	c.thisNode = node.NewNode("localhost:7001")

	// Register a simple object type for testing
	c.thisNode.RegisterObjectType((*testObject)(nil))

	// Create object without specifying ID - should fail without shard mapping
	ctx := context.Background()
	_, err := c.CreateObject(ctx, "testObject", "", nil)
	if err == nil {
		t.Error("CreateObject should fail when shard mapping is not available")
	}

	// Verify no object was created
	if c.thisNode.NumObjects() != 0 {
		t.Errorf("Expected 0 objects, got %d", c.thisNode.NumObjects())
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



