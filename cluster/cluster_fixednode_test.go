package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestCreateObject_FixedNodeAddress tests creating objects with fixed node addresses
func TestCreateObject_FixedNodeAddress(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_FixedNodeAddress")
	testNode := node.NewNode("localhost:7000")
	c.thisNode = testNode

	// Initialize shard mapper (required for GetNodeForObject to work)
	c.shardMapper = sharding.NewShardMapper(nil)

	// Register a simple object type for testing
	testNode.RegisterObjectType((*testObject)(nil))

	ctx := context.Background()

	// Test creating an object with a fixed node address pointing to this node
	objID := "localhost:7000/test-object-123"
	createdID, err := c.CreateObject(ctx, "testObject", objID, nil)
	if err != nil {
		t.Fatalf("CreateObject with fixed node address failed: %v", err)
	}

	if createdID != objID {
		t.Errorf("CreateObject returned ID %s, want %s", createdID, objID)
	}

	// Verify object was created locally
	if testNode.NumObjects() != 1 {
		t.Errorf("Expected 1 object on node, got %d", testNode.NumObjects())
	}

	// Verify the object can be found
	objs := testNode.ListObjects()
	found := false
	for _, obj := range objs {
		if obj.Id == objID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Object %s not found on node", objID)
	}
}

// TestCallObject_FixedNodeAddress tests calling object methods with fixed node addresses
func TestCallObject_FixedNodeAddress(t *testing.T) {
	c := newClusterForTesting("TestCallObject_FixedNodeAddress")
	testNode := node.NewNode("localhost:7000")
	c.thisNode = testNode

	// Initialize shard mapper (required for GetNodeForObject to work)
	c.shardMapper = sharding.NewShardMapper(nil)

	// Register and create an echo object type for testing
	testNode.RegisterObjectType((*echoObject)(nil))

	ctx := context.Background()

	// Create an object with a fixed node address
	objID := "localhost:7000/echo-obj"
	_, err := testNode.CreateObject(ctx, "echoObject", objID, nil)
	if err != nil {
		t.Fatalf("Failed to create test object: %v", err)
	}

	// Call the object using the fixed node address
	req := &emptypb.Empty{}
	resp, err := c.CallObject(ctx, objID, "Echo", req)
	if err != nil {
		t.Fatalf("CallObject with fixed node address failed: %v", err)
	}

	if resp == nil {
		t.Error("CallObject returned nil response")
	}
}

// TestCreateObject_FixedNodeAddress_WrongNode tests that objects with fixed addresses
// are not created on the wrong node
func TestCreateObject_FixedNodeAddress_WrongNode(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_FixedNodeAddress_WrongNode")
	testNode := node.NewNode("localhost:7000")
	c.thisNode = testNode

	// Register object type
	testNode.RegisterObjectType((*testObject)(nil))

	ctx := context.Background()

	// Try to create an object with a fixed node address pointing to a different node
	// This should fail because node connections are not set up
	objID := "localhost:7001/test-object-456"
	_, err := c.CreateObject(ctx, "testObject", objID, nil)
	if err == nil {
		t.Error("CreateObject should fail when trying to route to a different node without node connections")
	}

	// Verify object was NOT created locally
	if testNode.NumObjects() != 0 {
		t.Errorf("Expected 0 objects on local node, got %d", testNode.NumObjects())
	}
}

// TestCallObject_FixedNodeAddress_WrongNode tests that calls with fixed addresses
// fail appropriately when routing to a different node without connections
func TestCallObject_FixedNodeAddress_WrongNode(t *testing.T) {
	c := newClusterForTesting("TestCallObject_FixedNodeAddress_WrongNode")
	testNode := node.NewNode("localhost:7000")
	c.thisNode = testNode

	ctx := context.Background()

	// Try to call an object on a different node without node connections set up
	objID := "localhost:7001/remote-obj"
	_, err := c.CallObject(ctx, objID, "Echo", nil)
	if err == nil {
		t.Error("CallObject should fail when trying to route to a different node without node connections")
	}
}

// TestCreateObject_FixedNodeAddress_Format tests various fixed node address formats
func TestCreateObject_FixedNodeAddress_Format(t *testing.T) {
	c := newClusterForTesting("TestCreateObject_FixedNodeAddress_Format")
	testNode := node.NewNode("localhost:7000")
	c.thisNode = testNode

	// Initialize shard mapper (required for GetNodeForObject to work)
	c.shardMapper = sharding.NewShardMapper(nil)

	testNode.RegisterObjectType((*testObject)(nil))

	ctx := context.Background()

	tests := []struct {
		name        string
		objectID    string
		shouldLocal bool
	}{
		{
			name:        "standard format",
			objectID:    "localhost:7000/obj-1",
			shouldLocal: true,
		},
		{
			name:        "IP address format",
			objectID:    "localhost:7000/obj-2",
			shouldLocal: true,
		},
		{
			name:        "complex object part",
			objectID:    "localhost:7000/type-subtype-uuid-123",
			shouldLocal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.CreateObject(ctx, "testObject", tt.objectID, nil)
			if tt.shouldLocal {
				if err != nil {
					t.Errorf("CreateObject failed for local fixed node: %v", err)
				}
			}
		})
	}
}

// echoObject is a test object that echoes back requests
type echoObject struct {
	object.BaseObject
}

func (o *echoObject) OnCreated() {}

func (o *echoObject) Echo(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	return req, nil
}
