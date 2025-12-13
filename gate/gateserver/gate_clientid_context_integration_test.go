package gateserver

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestClientIDObject is a simple test object that captures the client ID from the context
type TestClientIDObject struct {
	goverseapi.BaseObject
	mu               sync.Mutex
	lastReceivedFrom string // Stores the client ID from the last call
}

// OnCreated initializes the object
func (o *TestClientIDObject) OnCreated() {
	o.Logger.Infof("TestClientIDObject %s created", o.Id())
}

// TestMethod is a method that extracts client_id from the context
// It uses wrapperspb.StringValue for request/response to avoid creating custom proto messages
func (o *TestClientIDObject) TestMethod(ctx context.Context, req *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Extract client ID from context
	clientID := goverseapi.GetClientID(ctx)
	o.lastReceivedFrom = clientID

	o.Logger.Infof("TestMethod called with message: %s, clientID: %s", req.GetValue(), clientID)

	// Return the client ID in the response
	return wrapperspb.String(clientID), nil
}

// GetLastReceivedFrom returns the client ID from the last call
func (o *TestClientIDObject) GetLastReceivedFrom() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.lastReceivedFrom
}

// TestClientIDInContext tests that client_id is properly propagated to object methods via context
func TestClientIDInContext(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Register the test object type
	nodeCluster.GetThisNode().RegisterObjectType((*TestClientIDObject)(nil))

	// Start mock gRPC server for the node
	// The mock server delegates CallObject/CreateObject to the actual node and cluster
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode()) // Delegate object operations to the node
	mockNodeServer.SetCluster(nodeCluster)            // Delegate cluster operations to the cluster
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })

	// 2. Create and start the real GatewayServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gateway server (non-blocking)
	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gateway server failed to start: %v", err)
	}

	// Wait for shard mapping to be initialized
	testutil.WaitForClusterReady(t, nodeCluster)

	// 3. Client connects to gate
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	gateClient := gate_pb.NewGateServiceClient(conn)

	// Start client register stream to get client_id
	registerStream, err := gateClient.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Failed to call Register: %v", err)
	}

	// Receive RegisterResponse with client_id
	registerAnyMsg, err := registerStream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive RegisterResponse: %v", err)
	}

	var registerResp gate_pb.RegisterResponse
	err = registerAnyMsg.UnmarshalTo(&registerResp)
	if err != nil {
		t.Fatalf("Failed to unmarshal RegisterResponse: %v", err)
	}

	clientID := registerResp.ClientId
	if clientID == "" {
		t.Fatal("Received empty client_id from gate")
	}
	t.Logf("Client registered with ID: %s", clientID)

	// 4. Create a test object
	objectID := "TestClientIDObject-123"
	createReq := &gate_pb.CreateObjectRequest{
		Type: "TestClientIDObject",
		Id:   objectID,
	}
	_, err = gateClient.CreateObject(ctx, createReq)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}
	t.Logf("Created object: %s", objectID)

	// 5. Call the object method with client_id in the request
	testReq := wrapperspb.String("hello from client")
	testReqAny, err := protohelper.MsgToAny(testReq)
	if err != nil {
		t.Fatalf("Failed to marshal test request: %v", err)
	}

	callReq := &gate_pb.CallObjectRequest{
		ClientId: clientID,
		Type:     "TestClientIDObject",
		Id:       objectID,
		Method:   "TestMethod",
		Request:  testReqAny,
	}

	callResp, err := gateClient.CallObject(ctx, callReq)
	if err != nil {
		t.Fatalf("Failed to call object: %v", err)
	}

	// 6. Verify the response contains the client_id
	var testResp wrapperspb.StringValue
	err = callResp.Response.UnmarshalTo(&testResp)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if testResp.GetValue() != clientID {
		t.Errorf("Expected client_id %q in response, got %q", clientID, testResp.GetValue())
	}

	t.Logf("Successfully received response with client_id: %s", testResp.GetValue())
}

// TestCallFromCluster tests that calls from other objects don't have client_id
func TestCallFromCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

	// Register the test object type
	nodeCluster.GetThisNode().RegisterObjectType((*TestClientIDObject)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	// Create a test object
	objectID := "TestClientIDObject-456"
	_, err := nodeCluster.CreateObject(ctx, "TestClientIDObject", objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Call the object method directly from the cluster (no client_id)
	testReq := wrapperspb.String("hello from cluster")

	result, err := nodeCluster.CallObject(ctx, "TestClientIDObject", objectID, "TestMethod", proto.Message(testReq))
	if err != nil {
		t.Fatalf("Failed to call object: %v", err)
	}

	testResp, ok := result.(*wrapperspb.StringValue)
	if !ok {
		t.Fatalf("Expected *wrapperspb.StringValue, got %T", result)
	}

	// Verify no client_id was passed (should be empty)
	if testResp.GetValue() != "" {
		t.Errorf("Expected empty client_id for cluster call, got %q", testResp.GetValue())
	}

	t.Logf("Successfully verified cluster call has no client_id")
}

// TestObjectA is a test object that calls another object (ObjectB)
type TestObjectA struct {
	goverseapi.BaseObject
}

func (o *TestObjectA) OnCreated() {
	o.Logger.Infof("TestObjectA %s created", o.Id())
}

// CallObjectB calls TestObjectB and returns the client ID it received
func (o *TestObjectA) CallObjectB(ctx context.Context, req *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	// Extract client ID from context for logging
	clientID := goverseapi.GetClientID(ctx)
	o.Logger.Infof("TestObjectA.CallObjectB called with message: %s, clientID: %s", req.GetValue(), clientID)

	// Call TestObjectB and pass along the context (which should contain client_id)
	objectBID := req.GetValue() // The request contains the ID of ObjectB to call
	result, err := goverseapi.CallObject(ctx, "TestObjectB", objectBID, "ReceiveCall", wrapperspb.String("called from A"))
	if err != nil {
		return nil, err
	}

	// Return the response from ObjectB
	resp, ok := result.(*wrapperspb.StringValue)
	if !ok {
		o.Logger.Errorf("Expected *wrapperspb.StringValue from ObjectB, got %T", result)
		return wrapperspb.String(""), nil
	}

	return resp, nil
}

// TestObjectB is a test object that receives calls and returns the client ID from context
type TestObjectB struct {
	goverseapi.BaseObject
}

func (o *TestObjectB) OnCreated() {
	o.Logger.Infof("TestObjectB %s created", o.Id())
}

// ReceiveCall is called by ObjectA and should receive the client ID from context
func (o *TestObjectB) ReceiveCall(ctx context.Context, req *wrapperspb.StringValue) (*wrapperspb.StringValue, error) {
	// Extract client ID from context
	clientID := goverseapi.GetClientID(ctx)
	o.Logger.Infof("TestObjectB.ReceiveCall called with message: %s, clientID: %s", req.GetValue(), clientID)

	// Return the client ID
	return wrapperspb.String(clientID), nil
}

// TestClientIDPropagationThroughObjects tests that client_id is propagated when:
// - Client calls ObjectA
// - ObjectA calls ObjectB
// - ObjectB should still receive the original client_id
//
// This test covers two scenarios in one:
// 1. ObjectA and ObjectB on the same node
// 2. ObjectA and ObjectB on different nodes
func TestClientIDPropagationThroughObjects(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create and start two nodes with cluster
	node1Addr := testutil.GetFreeAddress()
	node1Cluster := mustNewCluster(ctx, t, node1Addr, testPrefix)
	defer node1Cluster.Stop(ctx)

	node2Addr := testutil.GetFreeAddress()
	node2Cluster := mustNewCluster(ctx, t, node2Addr, testPrefix)
	defer node2Cluster.Stop(ctx)

	// Set node1Cluster as the global cluster so objects can use goverseapi.CallObject
	// In a real deployment, there would be only one cluster per process
	// Both node1 and node2 clusters are connected and can route to each other via gRPC
	cluster.SetThis(node1Cluster)
	t.Cleanup(func() { cluster.SetThis(nil) })

	// Register the test object types on both nodes
	node1Cluster.GetThisNode().RegisterObjectType((*TestObjectA)(nil))
	node1Cluster.GetThisNode().RegisterObjectType((*TestObjectB)(nil))
	node2Cluster.GetThisNode().RegisterObjectType((*TestObjectA)(nil))
	node2Cluster.GetThisNode().RegisterObjectType((*TestObjectB)(nil))

	// Start mock gRPC servers for both nodes
	mockNode1Server := testutil.NewMockGoverseServer()
	mockNode1Server.SetNode(node1Cluster.GetThisNode())
	mockNode1Server.SetCluster(node1Cluster)
	node1Server := testutil.NewTestServerHelper(node1Addr, mockNode1Server)
	err := node1Server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1 mock server: %v", err)
	}
	t.Cleanup(func() { node1Server.Stop() })

	mockNode2Server := testutil.NewMockGoverseServer()
	mockNode2Server.SetNode(node2Cluster.GetThisNode())
	mockNode2Server.SetCluster(node2Cluster)
	node2Server := testutil.NewTestServerHelper(node2Addr, mockNode2Server)
	err = node2Server.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2 mock server: %v", err)
	}
	t.Cleanup(func() { node2Server.Stop() })

	// Create and start the real GatewayServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gateway server (non-blocking)
	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gateway server failed to start: %v", err)
	}

	// Wait for shard mapping to be initialized
	testutil.WaitForClustersReady(t, node1Cluster, node2Cluster)

	// Client connects to gate
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	gateClient := gate_pb.NewGateServiceClient(conn)

	// Start client register stream to get client_id
	registerStream, err := gateClient.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Failed to call Register: %v", err)
	}

	// Receive RegisterResponse with client_id
	registerAnyMsg, err := registerStream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive RegisterResponse: %v", err)
	}

	var registerResp gate_pb.RegisterResponse
	err = registerAnyMsg.UnmarshalTo(&registerResp)
	if err != nil {
		t.Fatalf("Failed to unmarshal RegisterResponse: %v", err)
	}

	clientID := registerResp.ClientId
	if clientID == "" {
		t.Fatal("Received empty client_id from gate")
	}
	t.Logf("Client registered with ID: %s", clientID)

	// Test Case 1: ObjectA and ObjectB on the same node
	t.Run("SameNode", func(t *testing.T) {
		// Use GetObjectIDForShard to create objects on the same shard (same node)
		// With 2 nodes and 64 shards, shard 5 will be on one of the nodes
		targetShard := 5
		objectAID := testutil.GetObjectIDForShard(targetShard, "TestObjectA")
		objectBID := testutil.GetObjectIDForShard(targetShard, "TestObjectB")

		// Verify they're on the same node
		node1, err := node1Cluster.GetCurrentNodeForObject(ctx, objectAID)
		if err != nil {
			t.Fatalf("Failed to determine node for ObjectA: %v", err)
		}
		node2, err := node1Cluster.GetCurrentNodeForObject(ctx, objectBID)
		if err != nil {
			t.Fatalf("Failed to determine node for ObjectB: %v", err)
		}

		if node1 != node2 {
			t.Fatalf("Expected objects on same node, got A on %s and B on %s", node1, node2)
		}
		t.Logf("Objects on same shard %d, same node: %s", targetShard, node1)

		// Create both objects
		_, err = gateClient.CreateObject(ctx, &gate_pb.CreateObjectRequest{
			Type: "TestObjectA",
			Id:   objectAID,
		})
		if err != nil {
			t.Fatalf("Failed to create ObjectA: %v", err)
		}

		_, err = gateClient.CreateObject(ctx, &gate_pb.CreateObjectRequest{
			Type: "TestObjectB",
			Id:   objectBID,
		})
		if err != nil {
			t.Fatalf("Failed to create ObjectB: %v", err)
		}

		t.Logf("Created ObjectA: %s and ObjectB: %s", objectAID, objectBID)

		// Client calls ObjectA, passing ObjectB's ID
		testReq := wrapperspb.String(objectBID)
		testReqAny, err := protohelper.MsgToAny(testReq)
		if err != nil {
			t.Fatalf("Failed to marshal test request: %v", err)
		}

		callReq := &gate_pb.CallObjectRequest{
			ClientId: clientID,
			Type:     "TestObjectA",
			Id:       objectAID,
			Method:   "CallObjectB",
			Request:  testReqAny,
		}

		callResp, err := gateClient.CallObject(ctx, callReq)
		if err != nil {
			t.Fatalf("Failed to call ObjectA: %v", err)
		}

		// Verify the response contains the client_id (returned by ObjectB)
		var testResp wrapperspb.StringValue
		err = callResp.Response.UnmarshalTo(&testResp)
		if err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if testResp.GetValue() != clientID {
			t.Errorf("Expected client_id %q in response from ObjectB, got %q", clientID, testResp.GetValue())
		}

		t.Logf("✓ Same node test passed: ObjectB received client_id: %s", testResp.GetValue())
	})

	// Test Case 2: ObjectA and ObjectB on different nodes
	t.Run("DifferentNodes", func(t *testing.T) {
		// We need to create objects that will definitely be on different nodes
		// We'll create many objects until we find a pair on different nodes
		var objectAID, objectBID string
		var foundDifferentNodes bool

		for i := 0; i < 100 && !foundDifferentNodes; i++ {
			objectAID = fmt.Sprintf("TestObjectA-diff-%d", i)
			objectBID = fmt.Sprintf("TestObjectB-diff-%d", i)

			node1, err := node1Cluster.GetCurrentNodeForObject(ctx, objectAID)
			if err != nil {
				t.Fatalf("Failed to determine node for ObjectA: %v", err)
			}
			node2, err := node1Cluster.GetCurrentNodeForObject(ctx, objectBID)
			if err != nil {
				t.Fatalf("Failed to determine node for ObjectB: %v", err)
			}

			if node1 != node2 {
				foundDifferentNodes = true
				t.Logf("Found objects on different nodes: A on %s, B on %s", node1, node2)
			}
		}

		if !foundDifferentNodes {
			t.Skip("Could not find object IDs that hash to different nodes")
		}

		// Create both objects
		_, err = gateClient.CreateObject(ctx, &gate_pb.CreateObjectRequest{
			Type: "TestObjectA",
			Id:   objectAID,
		})
		if err != nil {
			t.Fatalf("Failed to create ObjectA: %v", err)
		}

		_, err = gateClient.CreateObject(ctx, &gate_pb.CreateObjectRequest{
			Type: "TestObjectB",
			Id:   objectBID,
		})
		if err != nil {
			t.Fatalf("Failed to create ObjectB: %v", err)
		}

		t.Logf("Created ObjectA: %s and ObjectB: %s on different nodes", objectAID, objectBID)

		// Client calls ObjectA, passing ObjectB's ID
		testReq := wrapperspb.String(objectBID)
		testReqAny, err := protohelper.MsgToAny(testReq)
		if err != nil {
			t.Fatalf("Failed to marshal test request: %v", err)
		}

		callReq := &gate_pb.CallObjectRequest{
			ClientId: clientID,
			Type:     "TestObjectA",
			Id:       objectAID,
			Method:   "CallObjectB",
			Request:  testReqAny,
		}

		callResp, err := gateClient.CallObject(ctx, callReq)
		if err != nil {
			t.Fatalf("Failed to call ObjectA: %v", err)
		}

		// Verify the response contains the client_id (returned by ObjectB)
		var testResp wrapperspb.StringValue
		err = callResp.Response.UnmarshalTo(&testResp)
		if err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if testResp.GetValue() != clientID {
			t.Errorf("Expected client_id %q in response from ObjectB, got %q", clientID, testResp.GetValue())
		}

		t.Logf("✓ Different nodes test passed: ObjectB received client_id: %s", testResp.GetValue())
	})
}
