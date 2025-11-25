package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/structpb"
)

// waitForObjectCreatedOnNode waits for an object to be created on the specified node
func waitForObjectCreatedOnNode(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
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
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForObjectDeletedOnNode waits for an object to be deleted from the specified node
func waitForObjectDeletedOnNode(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		objectFound := false
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				objectFound = true
				break
			}
		}
		if !objectFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not deleted from node within %v", objID, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestGateNodeObject is a simple test object with methods for testing
type TestGateNodeObject struct {
	object.BaseObject
	callCount int // Track how many times methods are called
}

func (o *TestGateNodeObject) OnCreated() {}

// Echo is a simple method that echoes back the message and tracks call count
// Request format: {message: string}
// Response format: {message: string, callCount: number}
func (o *TestGateNodeObject) Echo(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	o.callCount++

	message := ""
	if req != nil && req.Fields["message"] != nil {
		message = req.Fields["message"].GetStringValue()
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"message":   structpb.NewStringValue("Echo: " + message),
			"callCount": structpb.NewNumberValue(float64(o.callCount)),
		},
	}, nil
}

// Increment is a method that adds to an internal counter
// Request format: {value: number}
// Response format: {result: number}
func (o *TestGateNodeObject) Increment(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	value := 0
	if req != nil && req.Fields["value"] != nil {
		value = int(req.Fields["value"].GetNumberValue())
	}

	o.callCount += value

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"result": structpb.NewNumberValue(float64(o.callCount)),
		},
	}, nil
}

// TestGateNodeIntegration tests the complete gate-to-node integration flow:
// 1. CreateObject via gate cluster - verifies objects are created on correct nodes
// 2. CallObject via gate cluster - verifies methods are executed on nodes and responses returned
// 3. State persistence - verifies object state is maintained across method calls
// 4. Multiple methods - verifies different methods can be called on the same object
// 5. Multiple objects - verifies independent state across different objects
// 6. DeleteObject via gate cluster - verifies objects are properly deleted from nodes
// 7. Delete and recreate - verifies objects can be recreated with fresh state after deletion
//
// This test requires a running etcd instance at localhost:2379
func TestGateNodeIntegration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create gate cluster
	gateAddr := testutil.GetFreeAddress()
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	// Create gate cluster with test values
	gateCluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	// Start the gate cluster
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	t.Cleanup(func() {
		gateCluster.Stop(ctx)
		gw.Stop()
	})
	t.Logf("Created gate cluster at %s", gateAddr)

	// Create node cluster using mustNewCluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeObject)(nil))

	// Wait for clusters to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC server for the node to handle inter-cluster communication
	mockServer := testutil.NewMockGoverseServer()
	mockServer.SetNode(testNode) // Assign the actual node to the mock server
	testServer := testutil.NewTestServerHelper(nodeAddr, mockServer)
	err = testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer.Stop() })

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Wait for shard mapping to be initialized
	testutil.WaitForClusterReady(t, nodeCluster)
	testutil.WaitForClusterReady(t, gateCluster)

	// Verify shard mapping is ready
	_ = gateCluster.GetShardMapping(ctx)

	t.Run("CreateObjectViaGate", func(t *testing.T) {
		// Test creating an object through the gate cluster
		objID := "test-gate-object-1"

		// Get the target node for this object
		targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
		}
		t.Logf("Creating object %s from gate, expect target node %s", objID, targetNode)

		// Create the object via the gate cluster - it will be routed to the node
		createdID, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		// Verify the object was created on the node
		if targetNode != nodeAddr {
			t.Fatalf("Expected target node %s, got %s", nodeAddr, targetNode)
		}

		// Wait for async object creation to complete
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
	})

	t.Run("CreateMultipleObjectsViaGate", func(t *testing.T) {
		// Test creating multiple objects through the gate cluster
		for i := 1; i <= 5; i++ {
			objID := fmt.Sprintf("test-gate-object-%d", i+1)

			// Get the target node for this object
			targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
			if err != nil {
				t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
			}
			t.Logf("Creating object %s from gate, expect target node %s", objID, targetNode)

			// Create the object via the gate cluster
			createdID, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
			if err != nil {
				t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
			}

			if createdID != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, createdID)
			}

			// Verify the object was created on the correct node
			if targetNode != nodeAddr {
				t.Fatalf("Expected target node %s, got %s", nodeAddr, targetNode)
			}

			// Wait for async object creation to complete
			waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

			t.Logf("Successfully created and verified object %s on node %s", objID, targetNode)
		}
	})

	t.Run("CallObjectViaGate", func(t *testing.T) {
		// First create an object to call methods on
		objID := "test-call-object-1"

		// Get the target node for this object
		targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
		}
		t.Logf("Creating object %s for CallObject test, target node %s", objID, targetNode)

		// Create the object via the gate cluster
		_, err = gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		// Wait for async object creation to complete
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Test Echo method
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from gate"),
			},
		}
		result, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject Echo via gate failed: %v", err)
		}

		echoResp, ok := result.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result)
		}

		expectedMsg := "Echo: Hello from gate"
		actualMsg := echoResp.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
		}

		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1, got %d", callCount)
		}

		t.Logf("Successfully called Echo via gate, response: %s (call count: %d)", actualMsg, callCount)

		// Call Echo again to verify state persistence
		echoReq2 := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Second call"),
			},
		}
		result2, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Echo", echoReq2)
		if err != nil {
			t.Fatalf("CallObject Echo (second call) via gate failed: %v", err)
		}

		echoResp2, ok := result2.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result2)
		}

		callCount2 := int(echoResp2.Fields["callCount"].GetNumberValue())
		if callCount2 != 2 {
			t.Fatalf("Expected call count 2 (state persisted), got %d", callCount2)
		}

		t.Logf("Successfully verified state persistence: call count is %d", callCount2)
	})

	t.Run("CallMultipleMethodsViaGate", func(t *testing.T) {
		// Create an object for testing multiple method calls
		objID := "test-multi-method-object"

		// Get the target node for this object
		targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
		}
		t.Logf("Creating object %s for multi-method test, target node %s", objID, targetNode)

		// Create the object via the gate cluster
		_, err = gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		// Wait for async object creation to complete
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Test Increment method
		incReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(5),
			},
		}
		result, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Increment", incReq)
		if err != nil {
			t.Fatalf("CallObject Increment via gate failed: %v", err)
		}

		incResp, ok := result.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result)
		}

		incResult := int(incResp.Fields["result"].GetNumberValue())
		if incResult != 5 {
			t.Fatalf("Expected result 5, got %d", incResult)
		}

		t.Logf("Successfully called Increment via gate, result: %d", incResult)

		// Call Increment again
		incReq2 := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(10),
			},
		}
		result2, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Increment", incReq2)
		if err != nil {
			t.Fatalf("CallObject Increment (second call) via gate failed: %v", err)
		}

		incResp2, ok := result2.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result2)
		}

		incResult2 := int(incResp2.Fields["result"].GetNumberValue())
		if incResult2 != 15 {
			t.Fatalf("Expected result 15 (5+10), got %d", incResult2)
		}

		t.Logf("Successfully verified cumulative increment: result is %d", incResult2)

		// Call Echo method on the same object to verify we can call different methods
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Testing different method"),
			},
		}
		result3, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject Echo via gate failed: %v", err)
		}

		echoResp, ok := result3.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result3)
		}

		// The call count should be 16 (15 from Increment + 1 from Echo)
		echoCallCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if echoCallCount != 16 {
			t.Fatalf("Expected call count 16 (shared state), got %d", echoCallCount)
		}

		t.Logf("Successfully called Echo on same object, call count: %d (state shared across methods)", echoCallCount)
	})

	t.Run("CallObjectOnMultipleObjects", func(t *testing.T) {
		// Test calling methods on multiple different objects
		numObjects := 3
		for i := 1; i <= numObjects; i++ {
			objID := fmt.Sprintf("test-multi-object-%d", i)

			// Create the object
			_, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
			if err != nil {
				t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
			}

			// Wait for object creation
			waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

			// Call Echo with unique message
			echoReq := &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"message": structpb.NewStringValue(fmt.Sprintf("Message from object %d", i)),
				},
			}
			result, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Echo", echoReq)
			if err != nil {
				t.Fatalf("CallObject Echo via gate failed for %s: %v", objID, err)
			}

			echoResp, ok := result.(*structpb.Struct)
			if !ok {
				t.Fatalf("Expected *structpb.Struct, got %T", result)
			}

			actualMsg := echoResp.Fields["message"].GetStringValue()
			expectedMsg := fmt.Sprintf("Echo: Message from object %d", i)
			if actualMsg != expectedMsg {
				t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
			} // Each object should have its own state (call count = 1)
			eoCallCount := int(echoResp.Fields["callCount"].GetNumberValue())
			if eoCallCount != 1 {
				t.Fatalf("Expected call count 1 for object %s, got %d (objects should have independent state)", objID, eoCallCount)
			}

			t.Logf("Successfully called Echo on object %s, response: %s", objID, actualMsg)
		}

		t.Logf("Successfully verified %d objects maintain independent state", numObjects)
	})

	t.Run("DeleteObjectViaGate", func(t *testing.T) {
		// Create an object to delete
		objID := "test-delete-object-1"

		// Get the target node for this object
		targetNode, err := gateCluster.GetCurrentNodeForObject(ctx, objID)
		if err != nil {
			t.Fatalf("GetCurrentNodeForObject failed for %s: %v", objID, err)
		}
		t.Logf("Creating object %s for deletion test, target node %s", objID, targetNode)

		// Create the object via the gate cluster
		_, err = gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		// Wait for async object creation to complete
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Verify object exists on node before deletion
		objectExists := false
		for _, obj := range testNode.ListObjects() {
			if obj.Id == objID {
				objectExists = true
				break
			}
		}
		if !objectExists {
			t.Fatalf("Object %s should exist on node before deletion", objID)
		}
		t.Logf("Verified object %s exists on node before deletion", objID)

		// Delete the object via the gate cluster
		err = gateCluster.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("DeleteObject via gate failed for %s: %v", objID, err)
		}

		// Wait for async object deletion to complete
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully deleted object %s via gate and verified deletion on node", objID)
	})

	t.Run("DeleteMultipleObjectsViaGate", func(t *testing.T) {
		// Test deleting multiple objects through the gate cluster
		numObjects := 3
		objectIDs := make([]string, numObjects)

		// Create multiple objects
		for i := 0; i < numObjects; i++ {
			objID := fmt.Sprintf("test-delete-multi-%d", i+1)
			objectIDs[i] = objID

			_, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
			if err != nil {
				t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
			}

			waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
			t.Logf("Created object %s for deletion test", objID)
		}

		// Delete all objects
		for _, objID := range objectIDs {
			err := gateCluster.DeleteObject(ctx, objID)
			if err != nil {
				t.Fatalf("DeleteObject via gate failed for %s: %v", objID, err)
			}
			t.Logf("Deleted object %s via gate", objID)
		}

		// Verify all objects are deleted from the node
		for _, objID := range objectIDs {
			waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
			t.Logf("Verified object %s deleted from node", objID)
		}

		t.Logf("Successfully deleted and verified %d objects", numObjects)
	})

	t.Run("DeleteAndRecreateObject", func(t *testing.T) {
		// Test that we can delete and recreate an object with the same ID
		objID := "test-delete-recreate-1"

		// Create the object
		_, err := gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Created object %s", objID)

		// Call a method to set state
		incReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"value": structpb.NewNumberValue(42),
			},
		}
		result, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Increment", incReq)
		if err != nil {
			t.Fatalf("CallObject Increment via gate failed: %v", err)
		}

		incResp, ok := result.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result)
		}

		incResult := int(incResp.Fields["result"].GetNumberValue())
		if incResult != 42 {
			t.Fatalf("Expected result 42, got %d", incResult)
		}
		t.Logf("Object %s has state: callCount=%d", objID, incResult)

		// Delete the object
		err = gateCluster.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("DeleteObject via gate failed for %s: %v", objID, err)
		}

		// Wait for deletion
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Deleted object %s", objID)

		// Recreate the object with the same ID
		_, err = gateCluster.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("Failed to recreate object %s: %v", objID, err)
		}

		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Recreated object %s", objID)

		// Call the method again - state should be reset to initial values
		result2, err := gateCluster.CallObject(ctx, "TestGateNodeObject", objID, "Increment", incReq)
		if err != nil {
			t.Fatalf("CallObject Increment via gate failed after recreate: %v", err)
		}

		incResp2, ok := result2.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result2)
		}

		incResult2 := int(incResp2.Fields["result"].GetNumberValue())
		if incResult2 != 42 {
			t.Fatalf("Expected result 42 (fresh object), got %d", incResult2)
		}

		t.Logf("Successfully verified recreated object %s has fresh state: callCount=%d", objID, incResult2)
	})
}
