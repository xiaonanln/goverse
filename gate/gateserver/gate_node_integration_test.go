package gateserver

import (
	"context"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
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

// waitForObjectDeletedOnNode waits for an object to be deleted on the specified node
func waitForObjectDeletedOnNode(t *testing.T, n *node.Node, objID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		found := false
		for _, obj := range n.ListObjects() {
			if obj.Id == objID {
				found = true
				break
			}
		}
		if !found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Object %s was not deleted from node within %v", objID, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestGateNodeIntegrationSimple tests basic gate-to-node integration:
// - Creates a real GatewayServer that routes calls through cluster to a node
// - Creates objects via the gate client and verifies they're created on the node
// - Calls object methods via the gate client and verifies responses
// - Uses 1 gate and 1 node for simplicity
func TestGateNodeIntegrationSimple(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster (so it registers with etcd)
	nodeAddr := "localhost:48001"
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeObject)(nil))

	// Start mock gRPC server for the node to handle inter-node communication
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create and start the real GatewayServer
	gateAddr := "localhost:48002"
	gwServerConfig := &GatewayServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gwServer, err := NewGatewayServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gateway server in a goroutine
	gwStartCtx, gwStartCancel := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel)

	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gwServer.Start(gwStartCtx)
	}()

	// Wait for gateway to be ready
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gateway server failed to start: %v", err)
		}
	default:
		// Gateway is running
	}
	t.Logf("Started real gateway server at %s", gateAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create a gRPC client to connect to the gate
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create gate client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	gateClient := gate_pb.NewGateServiceClient(conn)
	t.Logf("Created gate client connected to %s", gateAddr)

	t.Run("CreateObjectViaGate", func(t *testing.T) {
		// Test creating an object through the gate server
		objID := "test-gate-object-1"

		// Create the object via the gate
		req := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID,
		}
		resp, err := gateClient.CreateObject(ctx, req)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		if resp.Id != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, resp.Id)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully created object %s via gate and verified on node", objID)
	})

	t.Run("CallObjectViaGate", func(t *testing.T) {
		// First create an object to call methods on
		objID := "test-gate-call-1"

		// Create the object via the gate
		createReq := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID,
		}
		_, err := gateClient.CreateObject(ctx, createReq)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID, err)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Test Echo method via the gate
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from gate"),
			},
		}

		// Marshal the request to Any
		reqAny := &anypb.Any{}
		if err := reqAny.MarshalFrom(echoReq); err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		callReq := &gate_pb.CallObjectRequest{
			Type:    "TestGateNodeObject",
			Id:      objID,
			Method:  "Echo",
			Request: reqAny,
		}
		callResp, err := gateClient.CallObject(ctx, callReq)
		if err != nil {
			t.Fatalf("CallObject Echo via gate failed: %v", err)
		}

		// Unmarshal the response
		var echoResp structpb.Struct
		if err := callResp.Response.UnmarshalTo(&echoResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
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
	})

	t.Run("DeleteObjectViaGate", func(t *testing.T) {
		// Create multiple objects to delete
		objID1 := "test-gate-delete-1"
		objID2 := "test-gate-delete-2"

		// Create first object via the gate
		createReq1 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID1,
		}
		_, err := gateClient.CreateObject(ctx, createReq1)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID1, err)
		}

		// Create second object via the gate
		createReq2 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID2,
		}
		_, err = gateClient.CreateObject(ctx, createReq2)
		if err != nil {
			t.Fatalf("CreateObject via gate failed for %s: %v", objID2, err)
		}

		// Wait for objects to be created on the node
		waitForObjectCreatedOnNode(t, testNode, objID1, 5*time.Second)
		waitForObjectCreatedOnNode(t, testNode, objID2, 5*time.Second)
		t.Logf("Created objects %s and %s via gate and verified on node", objID1, objID2)

		// Delete first object via the gate
		deleteReq1 := &gate_pb.DeleteObjectRequest{
			Id: objID1,
		}
		_, err = gateClient.DeleteObject(ctx, deleteReq1)
		if err != nil {
			t.Fatalf("DeleteObject via gate failed for %s: %v", objID1, err)
		}

		// Delete second object via the gate
		deleteReq2 := &gate_pb.DeleteObjectRequest{
			Id: objID2,
		}
		_, err = gateClient.DeleteObject(ctx, deleteReq2)
		if err != nil {
			t.Fatalf("DeleteObject via gate failed for %s: %v", objID2, err)
		}

		// DeleteObject is async, so give it a moment to initiate
		time.Sleep(200 * time.Millisecond)

		// Verify objects are removed from the node
		waitForObjectDeletedOnNode(t, testNode, objID1, 5*time.Second)
		waitForObjectDeletedOnNode(t, testNode, objID2, 5*time.Second)

		// Double-check by listing all objects and ensuring they're not present
		objects := testNode.ListObjects()
		for _, obj := range objects {
			if obj.Id == objID1 || obj.Id == objID2 {
				t.Fatalf("Object %s still exists on node after deletion", obj.Id)
			}
		}

		t.Logf("Successfully deleted objects %s and %s via gate and verified removal on node", objID1, objID2)
	})
}

// Note: For full gate-to-node integration tests with cluster routing,
// see cluster/cluster_gate_node_createobject_integration_test.go
// which tests the complete flow with gate cluster routing to nodes.
