package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/object"
)

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
