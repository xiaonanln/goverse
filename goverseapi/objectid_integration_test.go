package goverseapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// TestObject is a simple test object
type TestObject struct {
	object.BaseObject
	value string
}

func (o *TestObject) GetValue(ctx context.Context, req *EmptyRequest) (*ValueResponse, error) {
	return &ValueResponse{Value: o.value}, nil
}

func (o *TestObject) SetValue(ctx context.Context, req *ValueRequest) (*EmptyResponse, error) {
	o.value = req.Value
	return &EmptyResponse{}, nil
}

func init() {
	// Register the test object type
	// This will be used in integration tests
	cluster.This().GetThisNode().RegisterObjectType((*TestObject)(nil))
}

// EmptyRequest for testing
type EmptyRequest struct{}

func (m *EmptyRequest) Reset()         { *m = EmptyRequest{} }
func (m *EmptyRequest) String() string { return "EmptyRequest{}" }
func (m *EmptyRequest) ProtoMessage()  {}

// ValueRequest for testing
type ValueRequest struct {
	Value string
}

func (m *ValueRequest) Reset()         { *m = ValueRequest{} }
func (m *ValueRequest) String() string { return fmt.Sprintf("ValueRequest{Value: %s}", m.Value) }
func (m *ValueRequest) ProtoMessage()  {}

// ValueResponse for testing
type ValueResponse struct {
	Value string
}

func (m *ValueResponse) Reset()         { *m = ValueResponse{} }
func (m *ValueResponse) String() string { return fmt.Sprintf("ValueResponse{Value: %s}", m.Value) }
func (m *ValueResponse) ProtoMessage()  {}

// EmptyResponse for testing
type EmptyResponse struct{}

func (m *EmptyResponse) Reset()         { *m = EmptyResponse{} }
func (m *EmptyResponse) String() string { return "EmptyResponse{}" }
func (m *EmptyResponse) ProtoMessage()  {}

func TestCreateObjectID_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Prepare etcd
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create node
	nodeAddr := testutil.GetFreeAddress()
	nodeConfig := &node.Config{
		ListenAddress:    nodeAddr,
		AdvertiseAddress: nodeAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
		NumShards:        testutil.TestNumShards,
	}

	n, err := node.NewNode(nodeConfig)
	if err != nil {
		t.Skipf("Skipping - failed to create node: %v", err)
		return
	}

	// Register object type
	n.RegisterObjectType((*TestObject)(nil))

	// Start node
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop()

	// Wait for cluster to be ready
	nodeCluster := n.GetCluster()
	testutil.WaitForClustersReady(t, nodeCluster)

	// Test 1: Create object with normal ID
	t.Run("NormalObjectID", func(t *testing.T) {
		objID := CreateObjectID()
		if objID == "" {
			t.Fatal("CreateObjectID returned empty string")
		}

		// Create object
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object with normal ID: %v", err)
		}

		// Verify object exists
		exists := n.HasObject(objID)
		if !exists {
			t.Fatalf("Object %s should exist but doesn't", objID)
		}
	})

	// Test 2: Create object with fixed shard ID
	t.Run("FixedShardObjectID", func(t *testing.T) {
		targetShardID := 5
		objID := CreateObjectIDOnShard(targetShardID)

		// Verify format
		expectedPrefix := fmt.Sprintf("shard#%d/", targetShardID)
		if !strings.HasPrefix(objID, expectedPrefix) {
			t.Fatalf("Object ID %s should start with %s", objID, expectedPrefix)
		}

		// Verify shard routing
		actualShardID := sharding.GetShardID(objID, testutil.TestNumShards)
		if actualShardID != targetShardID {
			t.Fatalf("Object ID %s hashes to shard %d, want %d", objID, actualShardID, targetShardID)
		}

		// Create object
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object with fixed shard ID: %v", err)
		}

		// Verify object exists
		exists := n.HasObject(objID)
		if !exists {
			t.Fatalf("Object %s should exist but doesn't", objID)
		}
	})

	// Test 3: Create object with fixed node ID
	t.Run("FixedNodeObjectID", func(t *testing.T) {
		objID := CreateObjectIDOnNode(nodeAddr)

		// Verify format
		expectedPrefix := nodeAddr + "/"
		if !strings.HasPrefix(objID, expectedPrefix) {
			t.Fatalf("Object ID %s should start with %s", objID, expectedPrefix)
		}

		// Create object
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create object with fixed node ID: %v", err)
		}

		// Verify object exists
		exists := n.HasObject(objID)
		if !exists {
			t.Fatalf("Object %s should exist but doesn't", objID)
		}

		// Verify it's on the correct node by checking cluster routing
		currentNode, err := nodeCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("Failed to get current node for object: %v", err)
		}
		if currentNode != nodeAddr {
			t.Fatalf("Object %s routed to node %s, want %s", objID, currentNode, nodeAddr)
		}
	})
}

func TestObjectIDFormats_MultipleObjects(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Prepare etcd
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create node
	nodeAddr := testutil.GetFreeAddress()
	nodeConfig := &node.Config{
		ListenAddress:    nodeAddr,
		AdvertiseAddress: nodeAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       prefix,
		NumShards:        testutil.TestNumShards,
	}

	n, err := node.NewNode(nodeConfig)
	if err != nil {
		t.Skipf("Skipping - failed to create node: %v", err)
		return
	}

	// Register object type
	n.RegisterObjectType((*TestObject)(nil))

	// Start node
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop()

	// Wait for cluster to be ready
	nodeCluster := n.GetCluster()
	testutil.WaitForClustersReady(t, nodeCluster)

	// Create multiple objects using different ID formats
	objectIDs := make([]string, 0, 10)

	// Normal IDs
	for i := 0; i < 3; i++ {
		objID := CreateObjectID()
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create normal object %d: %v", i, err)
		}
		objectIDs = append(objectIDs, objID)
	}

	// Fixed shard IDs
	for i := 0; i < 3; i++ {
		objID := CreateObjectIDOnShard(i)
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create fixed shard object %d: %v", i, err)
		}
		objectIDs = append(objectIDs, objID)
	}

	// Fixed node IDs
	for i := 0; i < 3; i++ {
		objID := CreateObjectIDOnNode(nodeAddr)
		_, err := n.CreateObject(ctx, "TestObject", objID)
		if err != nil {
			t.Fatalf("Failed to create fixed node object %d: %v", i, err)
		}
		objectIDs = append(objectIDs, objID)
	}

	// Verify all objects exist
	for _, objID := range objectIDs {
		if !n.HasObject(objID) {
			t.Fatalf("Object %s should exist but doesn't", objID)
		}
	}

	// Verify we can call methods on all objects
	for _, objID := range objectIDs {
		obj, err := n.GetObject(objID)
		if err != nil {
			t.Fatalf("Failed to get object %s: %v", objID, err)
		}

		testObj, ok := obj.(*TestObject)
		if !ok {
			t.Fatalf("Object %s is not a TestObject", objID)
		}

		// Set a value
		testValue := fmt.Sprintf("test-%s", objID[:8])
		req := &ValueRequest{Value: testValue}
		_, err = testObj.SetValue(ctx, req)
		if err != nil {
			t.Fatalf("Failed to set value on object %s: %v", objID, err)
		}

		// Get the value back
		resp, err := testObj.GetValue(ctx, &EmptyRequest{})
		if err != nil {
			t.Fatalf("Failed to get value from object %s: %v", objID, err)
		}

		if resp.Value != testValue {
			t.Fatalf("Object %s returned value %s, want %s", objID, resp.Value, testValue)
		}
	}

	t.Logf("Successfully created and tested %d objects with different ID formats", len(objectIDs))
}

// Ensure test types implement proto.Message
var (
	_ proto.Message = (*EmptyRequest)(nil)
	_ proto.Message = (*ValueRequest)(nil)
	_ proto.Message = (*ValueResponse)(nil)
	_ proto.Message = (*EmptyResponse)(nil)
)
