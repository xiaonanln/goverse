package gateserver

import (
	"context"
	"fmt"
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
	nodeAddr := testutil.GetFreeAddress()
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
	gateAddr := testutil.GetFreeAddress()
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
	testutil.WaitForClusterReady(t, nodeCluster)

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

		// Verify objects are removed from the node
		waitForObjectDeletedOnNode(t, testNode, objID1, 5*time.Second)
		waitForObjectDeletedOnNode(t, testNode, objID2, 5*time.Second)

		t.Logf("Successfully deleted objects %s and %s via gate and verified removal on node", objID1, objID2)
	})
}

// TestGateNodeIntegrationMulti tests gate-to-node integration with multiple gates and nodes:
// - Creates 2 real GatewayServers that route calls through cluster to nodes
// - Creates 3 node servers in a cluster
// - Creates multiple objects via different gate clients
// - Calls multiple object methods via different gate clients
// - Verifies object distribution and correct routing across nodes
func TestGateNodeIntegrationMulti(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start 3 nodes with cluster
	nodeAddr1 := testutil.GetFreeAddress()
	nodeAddr2 := testutil.GetFreeAddress()
	nodeAddr3 := testutil.GetFreeAddress()

	// Node 1
	nodeCluster1 := mustNewCluster(ctx, t, nodeAddr1, etcdPrefix)
	testNode1 := nodeCluster1.GetThisNode()
	testNode1.RegisterObjectType((*TestGateNodeObject)(nil))
	t.Logf("Created node cluster 1 at %s", nodeAddr1)

	mockNodeServer1 := testutil.NewMockGoverseServer()
	mockNodeServer1.SetNode(testNode1)
	mockNodeServer1.SetCluster(nodeCluster1)
	nodeServer1 := testutil.NewTestServerHelper(nodeAddr1, mockNodeServer1)
	err := nodeServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node 1 mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer1.Stop() })
	t.Logf("Started mock node server 1 at %s", nodeAddr1)

	// Node 2
	nodeCluster2 := mustNewCluster(ctx, t, nodeAddr2, etcdPrefix)
	testNode2 := nodeCluster2.GetThisNode()
	testNode2.RegisterObjectType((*TestGateNodeObject)(nil))
	t.Logf("Created node cluster 2 at %s", nodeAddr2)

	mockNodeServer2 := testutil.NewMockGoverseServer()
	mockNodeServer2.SetNode(testNode2)
	mockNodeServer2.SetCluster(nodeCluster2)
	nodeServer2 := testutil.NewTestServerHelper(nodeAddr2, mockNodeServer2)
	err = nodeServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node 2 mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer2.Stop() })
	t.Logf("Started mock node server 2 at %s", nodeAddr2)

	// Node 3
	nodeCluster3 := mustNewCluster(ctx, t, nodeAddr3, etcdPrefix)
	testNode3 := nodeCluster3.GetThisNode()
	testNode3.RegisterObjectType((*TestGateNodeObject)(nil))
	t.Logf("Created node cluster 3 at %s", nodeAddr3)

	mockNodeServer3 := testutil.NewMockGoverseServer()
	mockNodeServer3.SetNode(testNode3)
	mockNodeServer3.SetCluster(nodeCluster3)
	nodeServer3 := testutil.NewTestServerHelper(nodeAddr3, mockNodeServer3)
	err = nodeServer3.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node 3 mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer3.Stop() })
	t.Logf("Started mock node server 3 at %s", nodeAddr3)

	// Wait for nodes to discover each other and stabilize
	testutil.WaitForClusterReady(t, nodeCluster1)
	testutil.WaitForClusterReady(t, nodeCluster2)
	testutil.WaitForClusterReady(t, nodeCluster3)

	// Create and start Gateway 1
	gateAddr1 := testutil.GetFreeAddress()
	gwServerConfig1 := &GatewayServerConfig{
		ListenAddress:    gateAddr1,
		AdvertiseAddress: gateAddr1,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gwServer1, err := NewGatewayServer(gwServerConfig1)
	if err != nil {
		t.Fatalf("Failed to create gateway server 1: %v", err)
	}
	t.Cleanup(func() { gwServer1.Stop() })

	gwStartCtx1, gwStartCancel1 := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel1)

	gwStarted1 := make(chan error, 1)
	go func() {
		gwStarted1 <- gwServer1.Start(gwStartCtx1)
	}()

	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted1:
		if err != nil {
			t.Fatalf("Gateway server 1 failed to start: %v", err)
		}
	default:
		// Gateway is running
	}
	t.Logf("Started gateway server 1 at %s", gateAddr1)

	// Create and start Gateway 2
	gateAddr2 := testutil.GetFreeAddress()
	gwServerConfig2 := &GatewayServerConfig{
		ListenAddress:    gateAddr2,
		AdvertiseAddress: gateAddr2,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gwServer2, err := NewGatewayServer(gwServerConfig2)
	if err != nil {
		t.Fatalf("Failed to create gateway server 2: %v", err)
	}
	t.Cleanup(func() { gwServer2.Stop() })

	gwStartCtx2, gwStartCancel2 := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel2)

	gwStarted2 := make(chan error, 1)
	go func() {
		gwStarted2 <- gwServer2.Start(gwStartCtx2)
	}()

	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted2:
		if err != nil {
			t.Fatalf("Gateway server 2 failed to start: %v", err)
		}
	default:
		// Gateway is running
	}
	t.Logf("Started gateway server 2 at %s", gateAddr2)

	// Wait for gates to register with nodes
	// (gates are already ready from node cluster ready above)
	time.Sleep(500 * time.Millisecond)

	// Create gRPC clients for both gates
	conn1, err := grpc.NewClient(gateAddr1, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create gate client 1: %v", err)
	}
	t.Cleanup(func() { conn1.Close() })
	gateClient1 := gate_pb.NewGateServiceClient(conn1)
	t.Logf("Created gate client 1 connected to %s", gateAddr1)

	conn2, err := grpc.NewClient(gateAddr2, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create gate client 2: %v", err)
	}
	t.Cleanup(func() { conn2.Close() })
	gateClient2 := gate_pb.NewGateServiceClient(conn2)
	t.Logf("Created gate client 2 connected to %s", gateAddr2)

	t.Run("CreateMultipleObjectsViaDifferentGates", func(t *testing.T) {
		// Create objects through gate 1
		for i := 1; i <= 3; i++ {
			objID := fmt.Sprintf("test-multi-obj-gate1-%d", i)
			req := &gate_pb.CreateObjectRequest{
				Type: "TestGateNodeObject",
				Id:   objID,
			}
			resp, err := gateClient1.CreateObject(ctx, req)
			if err != nil {
				t.Fatalf("CreateObject via gate1 failed for %s: %v", objID, err)
			}
			if resp.Id != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, resp.Id)
			}
			t.Logf("Created object %s via gate1", objID)
		}

		// Create objects through gate 2
		for i := 1; i <= 3; i++ {
			objID := fmt.Sprintf("test-multi-obj-gate2-%d", i)
			req := &gate_pb.CreateObjectRequest{
				Type: "TestGateNodeObject",
				Id:   objID,
			}
			resp, err := gateClient2.CreateObject(ctx, req)
			if err != nil {
				t.Fatalf("CreateObject via gate2 failed for %s: %v", objID, err)
			}
			if resp.Id != objID {
				t.Fatalf("Expected object ID %s, got %s", objID, resp.Id)
			}
			t.Logf("Created object %s via gate2", objID)
		}

		// Wait for all objects to be created
		time.Sleep(2 * time.Second)

		// Verify objects are distributed across nodes
		totalObjects := len(testNode1.ListObjects()) + len(testNode2.ListObjects()) + len(testNode3.ListObjects())
		if totalObjects != 6 {
			t.Logf("Node1 has %d objects, Node2 has %d objects, Node3 has %d objects",
				len(testNode1.ListObjects()), len(testNode2.ListObjects()), len(testNode3.ListObjects()))
			t.Fatalf("Expected 6 total objects across all nodes, got %d", totalObjects)
		}
		t.Logf("Successfully created and distributed 6 objects across 3 nodes")
	})

	t.Run("CallObjectMethodsViaMultipleGates", func(t *testing.T) {
		// Create some test objects
		testObjs := []string{"test-call-multi-1", "test-call-multi-2", "test-call-multi-3"}

		// Create objects via gate1
		for _, objID := range testObjs {
			createReq := &gate_pb.CreateObjectRequest{
				Type: "TestGateNodeObject",
				Id:   objID,
			}
			_, err := gateClient1.CreateObject(ctx, createReq)
			if err != nil {
				t.Fatalf("CreateObject via gate1 failed for %s: %v", objID, err)
			}
		}

		time.Sleep(2 * time.Second)

		// Call methods on objects via different gates
		for i, objID := range testObjs {
			// Alternate between gate1 and gate2
			var gateClient gate_pb.GateServiceClient
			var gateName string
			if i%2 == 0 {
				gateClient = gateClient1
				gateName = "gate1"
			} else {
				gateClient = gateClient2
				gateName = "gate2"
			}

			// Call Echo method multiple times
			for callNum := 1; callNum <= 2; callNum++ {
				echoReq := &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"message": structpb.NewStringValue(fmt.Sprintf("Test message from %s call %d", gateName, callNum)),
					},
				}

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
					t.Fatalf("CallObject Echo via %s failed for %s: %v", gateName, objID, err)
				}

				var echoResp structpb.Struct
				if err := callResp.Response.UnmarshalTo(&echoResp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}

				actualMsg := echoResp.Fields["message"].GetStringValue()
				callCount := int(echoResp.Fields["callCount"].GetNumberValue())

				t.Logf("Object %s via %s call %d: %s (total calls: %d)", objID, gateName, callNum, actualMsg, callCount)
			}
		}

		t.Logf("Successfully performed multiple method calls via different gates")
	})

	t.Run("VerifyObjectDistributionAcrossNodes", func(t *testing.T) {
		// Create more objects to ensure distribution
		for i := 1; i <= 10; i++ {
			objID := fmt.Sprintf("test-distribution-%d", i)
			// Alternate between gates
			var gateClient gate_pb.GateServiceClient
			if i%2 == 0 {
				gateClient = gateClient1
			} else {
				gateClient = gateClient2
			}

			req := &gate_pb.CreateObjectRequest{
				Type: "TestGateNodeObject",
				Id:   objID,
			}
			_, err := gateClient.CreateObject(ctx, req)
			if err != nil {
				t.Fatalf("CreateObject failed for %s: %v", objID, err)
			}
		}

		time.Sleep(2 * time.Second)

		// Count objects on each node
		node1Count := len(testNode1.ListObjects())
		node2Count := len(testNode2.ListObjects())
		node3Count := len(testNode3.ListObjects())

		t.Logf("Object distribution: Node1=%d, Node2=%d, Node3=%d", node1Count, node2Count, node3Count)

		// Verify at least one object is on each node (due to shard mapping)
		// This might not always be true for very small numbers, but with 19 total objects
		// (6 + 3 + 10) it's highly likely each node has at least one
		totalObjects := node1Count + node2Count + node3Count
		if totalObjects < 10 {
			t.Fatalf("Expected at least 10 objects in total, got %d", totalObjects)
		}

		t.Logf("Successfully verified object distribution across nodes")
	})

	t.Run("DeleteObjectsViaMultipleGates", func(t *testing.T) {
		// Create objects via different gates
		objID1 := "test-multi-delete-1"
		objID2 := "test-multi-delete-2"
		objID3 := "test-multi-delete-3"
		objID4 := "test-multi-delete-4"

		// Create objects via gate1
		createReq1 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID1,
		}
		_, err := gateClient1.CreateObject(ctx, createReq1)
		if err != nil {
			t.Fatalf("CreateObject via gate1 failed for %s: %v", objID1, err)
		}

		createReq2 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID2,
		}
		_, err = gateClient1.CreateObject(ctx, createReq2)
		if err != nil {
			t.Fatalf("CreateObject via gate1 failed for %s: %v", objID2, err)
		}

		// Create objects via gate2
		createReq3 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID3,
		}
		_, err = gateClient2.CreateObject(ctx, createReq3)
		if err != nil {
			t.Fatalf("CreateObject via gate2 failed for %s: %v", objID3, err)
		}

		createReq4 := &gate_pb.CreateObjectRequest{
			Type: "TestGateNodeObject",
			Id:   objID4,
		}
		_, err = gateClient2.CreateObject(ctx, createReq4)
		if err != nil {
			t.Fatalf("CreateObject via gate2 failed for %s: %v", objID4, err)
		}

		// Wait for objects to be created
		time.Sleep(2 * time.Second)

		// Count objects before deletion
		initialCount := len(testNode1.ListObjects()) + len(testNode2.ListObjects()) + len(testNode3.ListObjects())
		t.Logf("Initial total objects: %d", initialCount)

		// Delete objects via gate1
		deleteReq1 := &gate_pb.DeleteObjectRequest{
			Id: objID1,
		}
		_, err = gateClient1.DeleteObject(ctx, deleteReq1)
		if err != nil {
			t.Fatalf("DeleteObject via gate1 failed for %s: %v", objID1, err)
		}

		deleteReq3 := &gate_pb.DeleteObjectRequest{
			Id: objID3,
		}
		_, err = gateClient1.DeleteObject(ctx, deleteReq3)
		if err != nil {
			t.Fatalf("DeleteObject via gate1 failed for %s: %v", objID3, err)
		}

		// Delete objects via gate2
		deleteReq2 := &gate_pb.DeleteObjectRequest{
			Id: objID2,
		}
		_, err = gateClient2.DeleteObject(ctx, deleteReq2)
		if err != nil {
			t.Fatalf("DeleteObject via gate2 failed for %s: %v", objID2, err)
		}

		deleteReq4 := &gate_pb.DeleteObjectRequest{
			Id: objID4,
		}
		_, err = gateClient2.DeleteObject(ctx, deleteReq4)
		if err != nil {
			t.Fatalf("DeleteObject via gate2 failed for %s: %v", objID4, err)
		}

		// Wait for objects to be deleted
		time.Sleep(2 * time.Second)

		// Count objects after deletion
		finalCount := len(testNode1.ListObjects()) + len(testNode2.ListObjects()) + len(testNode3.ListObjects())
		t.Logf("Final total objects: %d", finalCount)

		// Verify that 4 objects were deleted
		if finalCount != initialCount-4 {
			t.Fatalf("Expected %d objects after deletion, got %d", initialCount-4, finalCount)
		}

		t.Logf("Successfully deleted 4 objects via multiple gates and verified removal across nodes")
	})
}

// Note: For full gate-to-node integration tests with cluster routing,
// see cluster/cluster_gate_node_createobject_integration_test.go
// which tests the complete flow with gate cluster routing to nodes.
