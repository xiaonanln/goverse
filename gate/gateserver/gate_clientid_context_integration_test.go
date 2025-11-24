package gateserver

import (
	"context"
	"sync"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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
	nodeAddr := "localhost:48020"
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
	gateAddr := "localhost:48021"
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
	testReqAny, err := anypb.New(testReq)
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
	nodeAddr := "localhost:48022"
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
