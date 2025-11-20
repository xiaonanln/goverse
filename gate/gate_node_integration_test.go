package gate

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/structpb"
)

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

// TestGateNodeIntegrationSimple tests basic gate-to-node integration:
// - Creates a mock gate server and a mock node server
// - Creates an object via the gate and verifies it's created on the node
// - Uses 1 gate and 1 node for simplicity
//
// This test does NOT require etcd - it tests direct node communication
func TestGateNodeIntegrationSimple(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:48001"
	testNode := node.NewNode(nodeAddr)
	err := testNode.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { testNode.Stop(ctx) })
	t.Logf("Created node at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeObject)(nil))

	// Start mock gRPC server for the node to handle inter-cluster communication
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err = nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create gateway (note: gateway needs etcd config but we're not using cluster features in this simple test)
	gateAddr := "localhost:48002"
	gwConfig := &GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-node-simple",
	}
	gw, err := NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	t.Cleanup(func() { gw.Stop() })
	t.Logf("Created gateway at %s", gateAddr)

	// Start the gateway
	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gateway: %v", err)
	}

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	t.Run("CreateObjectViaNode", func(t *testing.T) {
		// Test creating an object directly on the node
		objID := "test-node-direct-1"

		// Create the object directly on the node
		createdID, err := testNode.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject on node failed for %s: %v", objID, err)
		}

		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		// Wait for object creation
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully created object %s directly on node", objID)
	})

	t.Run("CallObjectOnNode", func(t *testing.T) {
		// First create an object to call methods on
		objID := "test-node-call-1"

		// Create the object directly on the node
		_, err := testNode.CreateObject(ctx, "TestGateNodeObject", objID)
		if err != nil {
			t.Fatalf("CreateObject on node failed for %s: %v", objID, err)
		}

		// Wait for object creation
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Test Echo method
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from test"),
			},
		}
		result, err := testNode.CallObject(ctx, "TestGateNodeObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject Echo on node failed: %v", err)
		}

		echoResp, ok := result.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", result)
		}

		expectedMsg := "Echo: Hello from test"
		actualMsg := echoResp.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
		}

		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1, got %d", callCount)
		}

		t.Logf("Successfully called Echo on node, response: %s (call count: %d)", actualMsg, callCount)
	})
}

// Note: For full gate-to-node integration tests with cluster routing,
// see cluster/cluster_gate_node_createobject_integration_test.go
// which tests the complete flow with gate cluster routing to nodes.
