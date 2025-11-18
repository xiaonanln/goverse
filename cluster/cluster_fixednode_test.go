package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestCreateObject_FixedNodeAddress tests creating objects with fixed node addresses
func TestCreateObject_FixedNodeAddress(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestCreateObject_FixedNodeAddress")

	// Note: Fixed node addresses bypass consensus manager routing
	// The object ID format "nodeAddress/objectID" causes GetCurrentNodeForObject
	// to return the node address directly without consulting shard mapping

	// Register a simple object type for testing
	testNode.RegisterObjectType((*testObject)(nil))

	// Test creating an object with a fixed node address pointing to this node
	objID := "localhost:7000/test-object-123"
	createdID, err := c.CreateObject(ctx, "testObject", objID)
	if err != nil {
		t.Fatalf("CreateObject with fixed node address failed: %v", err)
	}

	if createdID != objID {
		t.Fatalf("CreateObject returned ID %s, want %s", createdID, objID)
	}

	// Wait for object to be created
	waitForObjectCreated(t, testNode, objID, 1*time.Second)

	// Verify object was created locally
	if testNode.NumObjects() != 1 {
		t.Fatalf("Expected 1 object on node, got %d", testNode.NumObjects())
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
		t.Fatalf("Object %s not found on node", objID)
	}
}

// TestCallObject_FixedNodeAddress tests calling object methods with fixed node addresses
func TestCallObject_FixedNodeAddress(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestCallObject_FixedNodeAddress")

	// Note: Fixed node addresses bypass consensus manager routing

	// Register and create an echo object type for testing
	testNode.RegisterObjectType((*echoObject)(nil))

	// Create an object with a fixed node address
	objID := "localhost:7000/echo-obj"
	_, err := testNode.CreateObject(ctx, "echoObject", objID)
	if err != nil {
		t.Fatalf("Failed to create test object: %v", err)
	}

	// Call the object using the fixed node address
	req := &emptypb.Empty{}
	resp, err := c.CallObject(ctx, "echoObject", objID, "Echo", req)
	if err != nil {
		t.Fatalf("CallObject with fixed node address failed: %v", err)
	}

	if resp == nil {
		t.Fatal("CallObject returned nil response")
	}
}

// TestCreateObject_FixedNodeAddress_WrongNode tests that objects with fixed addresses
// are not created on the wrong node
func TestCreateObject_FixedNodeAddress_WrongNode(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestCreateObject_FixedNodeAddress_WrongNode")

	// Register object type
	testNode.RegisterObjectType((*testObject)(nil))

	// Try to create an object with a fixed node address pointing to a different node
	// This should fail because node connections are not set up
	objID := "localhost:7001/test-object-456"
	_, err := c.CreateObject(ctx, "testObject", objID)
	if err == nil {
		t.Fatal("CreateObject should fail when trying to route to a different node without node connections")
	}

	// Verify object was NOT created locally
	if testNode.NumObjects() != 0 {
		t.Fatalf("Expected 0 objects on local node, got %d", testNode.NumObjects())
	}
}

// TestCallObject_FixedNodeAddress_WrongNode tests that calls with fixed addresses
// fail appropriately when routing to a different node without connections
func TestCallObject_FixedNodeAddress_WrongNode(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestCallObject_FixedNodeAddress_WrongNode")

	// Try to call an object on a different node without node connections set up
	objID := "localhost:7001/remote-obj"
	_, err := c.CallObject(ctx, "echoObject", objID, "Echo", nil)
	if err == nil {
		t.Fatal("CallObject should fail when trying to route to a different node without node connections")
	}
}

// TestCreateObject_FixedNodeAddress_Format tests various fixed node address formats
func TestCreateObject_FixedNodeAddress_Format(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestCreateObject_FixedNodeAddress_Format")

	// Note: Fixed node addresses bypass consensus manager routing

	testNode.RegisterObjectType((*testObject)(nil))

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
			_, err := c.CreateObject(ctx, "testObject", tt.objectID)
			if tt.shouldLocal {
				if err != nil {
					t.Fatalf("CreateObject failed for local fixed node: %v", err)
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

// Mock object type for testing
type testObject struct {
	object.BaseObject
}

func (o *testObject) OnCreated() {}
