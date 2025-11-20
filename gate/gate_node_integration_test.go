package gate

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// clusterRouter defines the interface for routing object operations through a cluster
type clusterRouter interface {
	CreateObject(ctx context.Context, typeName string, objectID string) (string, error)
	CallObject(ctx context.Context, typeName string, objectID string, method string, request proto.Message) (proto.Message, error)
}

// mockGateServer is a simple mock implementation of gate_pb.GateServiceServer
// that routes calls through a cluster to nodes for testing purposes
type mockGateServer struct {
	gate_pb.UnimplementedGateServiceServer
	gate    *Gateway
	cluster clusterRouter
}

func (m *mockGateServer) CreateObject(ctx context.Context, req *gate_pb.CreateObjectRequest) (*gate_pb.CreateObjectResponse, error) {
	// Route the call through the cluster
	objID, err := m.cluster.CreateObject(ctx, req.Type, req.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to create object via cluster: %w", err)
	}
	return &gate_pb.CreateObjectResponse{Id: objID}, nil
}

func (m *mockGateServer) CallObject(ctx context.Context, req *gate_pb.CallObjectRequest) (*gate_pb.CallObjectResponse, error) {
	// Unmarshal the request from Any if present
	var requestMsg proto.Message
	if req.Request != nil {
		var err error
		requestMsg, err = req.Request.UnmarshalNew()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	// Route the call through the cluster
	responseMsg, err := m.cluster.CallObject(ctx, req.Type, req.Id, req.Method, requestMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to call object via cluster: %w", err)
	}

	// Marshal the response back to Any
	var responseAny *anypb.Any
	if responseMsg != nil {
		responseAny = &anypb.Any{}
		if err := responseAny.MarshalFrom(responseMsg); err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}
	}

	return &gate_pb.CallObjectResponse{Response: responseAny}, nil
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

// mockClusterRouter is a test implementation of clusterRouter that uses testutil helpers
type mockClusterRouter struct {
	t              *testing.T
	nodeServerAddr string
	nodeServer     *testutil.MockGoverseServer
}

func (m *mockClusterRouter) CreateObject(ctx context.Context, typeName string, objectID string) (string, error) {
	// For this simple mock, we directly call the node server via gRPC
	// In a real cluster, this would route through node connections based on shard mapping
	conn, err := grpc.NewClient(m.nodeServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("failed to connect to node: %w", err)
	}
	defer conn.Close()

	client := goverse_pb.NewGoverseClient(conn)
	resp, err := client.CreateObject(ctx, &goverse_pb.CreateObjectRequest{
		Type: typeName,
		Id:   objectID,
	})
	if err != nil {
		return "", err
	}
	return resp.Id, nil
}

func (m *mockClusterRouter) CallObject(ctx context.Context, typeName string, objectID string, method string, request proto.Message) (proto.Message, error) {
	// For this simple mock, we directly call the node server via gRPC
	conn, err := grpc.NewClient(m.nodeServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to node: %w", err)
	}
	defer conn.Close()

	// Marshal request to Any
	var reqAny *anypb.Any
	if request != nil {
		reqAny = &anypb.Any{}
		if err := reqAny.MarshalFrom(request); err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
	}

	client := goverse_pb.NewGoverseClient(conn)
	resp, err := client.CallObject(ctx, &goverse_pb.CallObjectRequest{
		Type:    typeName,
		Id:      objectID,
		Method:  method,
		Request: reqAny,
	})
	if err != nil {
		return nil, err
	}

	// Unmarshal response
	if resp.Response != nil {
		return resp.Response.UnmarshalNew()
	}
	return nil, nil
}

// TestGateNodeIntegrationSimple tests basic gate-to-node integration:
// - Creates a mock gate gRPC server that routes calls through a cluster router to a node
// - Creates objects via the gate client and verifies they're created on the node
// - Calls object methods via the gate client and verifies responses
// - Uses 1 gate and 1 node for simplicity
//
// This test uses a simplified cluster router that connects to the node via gRPC
func TestGateNodeIntegrationSimple(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()

	// Create a node
	nodeAddr := "localhost:48001"
	testNode := testutil.MustNewNode(ctx, t, nodeAddr)
	t.Logf("Created node at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeObject)(nil))

	// Start mock gRPC server for the node to handle requests
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create a gateway
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

	// Start the gateway
	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gateway: %v", err)
	}
	t.Logf("Created and started gateway at %s", gateAddr)

	// Wait for servers to be ready
	time.Sleep(200 * time.Millisecond)

	// Create a mock cluster router that routes to the node
	clusterRouter := &mockClusterRouter{
		t:              t,
		nodeServerAddr: nodeAddr,
		nodeServer:     mockNodeServer,
	}

	// Create and start mock gate gRPC server that routes through cluster
	mockGate := &mockGateServer{
		gate:    gw,
		cluster: clusterRouter,
	}

	// Create gRPC server for the gate
	grpcServer := grpc.NewServer()
	gate_pb.RegisterGateServiceServer(grpcServer, mockGate)

	// Start listening
	listener, err := net.Listen("tcp", gateAddr)
	if err != nil {
		t.Fatalf("Failed to listen on %s: %v", gateAddr, err)
	}

	// Start serving in background
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("Gate server error: %v", err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.GracefulStop()
		listener.Close()
	})
	t.Logf("Started mock gate server at %s", gateAddr)

	// Wait for servers to be ready
	time.Sleep(200 * time.Millisecond)

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
}

// Note: For full gate-to-node integration tests with cluster routing,
// see cluster/cluster_gate_node_createobject_integration_test.go
// which tests the complete flow with gate cluster routing to nodes.
