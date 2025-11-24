package gateserver

import (
	"context"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Helper function to create and start a cluster with etcd for testing
func mustNewCluster(ctx context.Context, t *testing.T, nodeAddr string, etcdPrefix string) *cluster.Cluster {
	t.Helper()

	// Create a node
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create cluster config with test values (shorter durations for faster tests)
	cfg := cluster.Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
	}

	// Create cluster with etcd
	c, err := cluster.NewClusterWithNode(cfg, n)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster creation fails
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// Start the cluster (register node, start watching, etc.)
	err = c.Start(ctx, n)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster start fails
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		c.Stop(ctx)
		n.Stop(ctx)
	})

	return c
}

// TestPushMessageToClientViaGate tests the complete flow of pushing messages from a node to a client via a gate
// Flow:
// 1. Client registers to gate and gets client_id (with gate address prefix)
// 2. Gate registers to node via RegisterGate and gets stream
// 3. Node calls PushMessageToClient with client_id
// 4. Node routes message to gate based on client_id prefix
// 5. Gate receives message via RegisterGate stream
// 6. Gate extracts client_id from envelope and routes to client
// 7. Client receives message via Register stream
func TestPushMessageToClientViaGate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)
	t.Logf("Created node cluster at %s", nodeAddr)

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started node server at %s", nodeAddr)

	// 2. Create and start the real GateServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gate server in a goroutine
	gwStartCtx, gwStartCancel := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel)

	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gwServer.Start(gwStartCtx)
	}()

	// Wait for gate to be ready
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gate server failed to start: %v", err)
		}
	default:
		// Gate is running
	}
	t.Logf("Started real gate server at %s", gateAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	testutil.WaitForClusterReady(t, nodeCluster)

	// 3. Client connects to gate and registers
	conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	gateClient := gate_pb.NewGateServiceClient(conn)

	// Start client register stream
	registerStream, err := gateClient.Register(ctx, &gate_pb.Empty{})
	if err != nil {
		t.Fatalf("Failed to call Register: %v", err)
	}

	// Receive RegisterResponse with client_id
	firstMsg, err := registerStream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive RegisterResponse: %v", err)
	}

	var regResp gate_pb.RegisterResponse
	if err := firstMsg.UnmarshalTo(&regResp); err != nil {
		t.Fatalf("Failed to unmarshal RegisterResponse: %v", err)
	}

	clientID := regResp.ClientId
	t.Logf("Client registered with ID: %s", clientID)

	// Verify client ID has gate address as prefix
	expectedPrefix := gateAddr + "/"
	if len(clientID) <= len(expectedPrefix) || clientID[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("Expected client ID to start with %q, got %q", expectedPrefix, clientID)
	}

	// 4. Node pushes a message to the client via cluster routing
	testMessage := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"text":      structpb.NewStringValue("Hello from node!"),
			"timestamp": structpb.NewNumberValue(float64(time.Now().Unix())),
		},
	}

	// Push message via cluster (which will route to the gate)
	err = nodeCluster.PushMessageToClient(ctx, clientID, testMessage)
	if err != nil {
		t.Fatalf("Failed to push message from node: %v", err)
	}
	t.Logf("Node pushed message to client %s via cluster", clientID)

	// 5. Client receives the message via the stream
	receivedMsg, err := registerStream.Recv()
	if err != nil {
		t.Fatalf("Failed to receive message on client stream: %v", err)
	}

	var receivedStruct structpb.Struct
	if err := receivedMsg.UnmarshalTo(&receivedStruct); err != nil {
		t.Fatalf("Failed to unmarshal received message: %v", err)
	}

	// Verify the message content
	if receivedStruct.Fields["text"].GetStringValue() != "Hello from node!" {
		t.Fatalf("Expected text 'Hello from node!', got %q", receivedStruct.Fields["text"].GetStringValue())
	}

	t.Logf("Client successfully received message: %v", receivedStruct.Fields["text"].GetStringValue())
}

// TestPushMessageToMultipleClients tests pushing messages to multiple clients via the same gate
func TestPushMessageToMultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)
	t.Logf("Created node cluster at %s", nodeAddr)

	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started node server at %s", nodeAddr)

	// 2. Create and start the real GateServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gate server in a goroutine
	gwStartCtx, gwStartCancel := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel)

	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gwServer.Start(gwStartCtx)
	}()

	// Wait for gate to be ready
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gate server failed to start: %v", err)
		}
	default:
		// Gate is running
	}
	t.Logf("Started real gate server at %s", gateAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	testutil.WaitForClusterReady(t, nodeCluster)

	// 3. Register multiple clients
	numClients := 3
	type clientInfo struct {
		id     string
		stream grpc.ServerStreamingClient[anypb.Any]
	}
	clients := make([]clientInfo, numClients)

	for i := 0; i < numClients; i++ {
		gateConn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("Failed to connect to gate for client %d: %v", i, err)
		}
		defer gateConn.Close()

		gateClient := gate_pb.NewGateServiceClient(gateConn)
		registerStream, err := gateClient.Register(ctx, &gate_pb.Empty{})
		if err != nil {
			t.Fatalf("Failed to call Register for client %d: %v", i, err)
		}

		firstMsg, err := registerStream.Recv()
		if err != nil {
			t.Fatalf("Failed to receive RegisterResponse for client %d: %v", i, err)
		}

		var regResp gate_pb.RegisterResponse
		if err := firstMsg.UnmarshalTo(&regResp); err != nil {
			t.Fatalf("Failed to unmarshal RegisterResponse for client %d: %v", i, err)
		}

		clients[i] = clientInfo{
			id:     regResp.ClientId,
			stream: registerStream,
		}
		t.Logf("Client %d registered with ID: %s", i, clients[i].id)
	}

	// 4. Push different messages to each client via cluster routing
	for i, client := range clients {
		testMessage := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"clientNum": structpb.NewNumberValue(float64(i)),
				"message":   structpb.NewStringValue("Hello to client " + string(rune(i+'A'))),
			},
		}

		err = nodeCluster.PushMessageToClient(ctx, client.id, testMessage)
		if err != nil {
			t.Fatalf("Failed to push message to client %d: %v", i, err)
		}
		t.Logf("Pushed message to client %d (%s) via cluster", i, client.id)
	}

	// 5. Each client receives their specific message
	for i, client := range clients {
		receivedMsg, err := client.stream.Recv()
		if err != nil {
			t.Fatalf("Client %d failed to receive message: %v", i, err)
		}

		var receivedStruct structpb.Struct
		if err := receivedMsg.UnmarshalTo(&receivedStruct); err != nil {
			t.Fatalf("Client %d failed to unmarshal message: %v", i, err)
		}

		expectedMsg := "Hello to client " + string(rune(i+'A'))
		actualMsg := receivedStruct.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Client %d expected message %q, got %q", i, expectedMsg, actualMsg)
		}

		clientNum := int(receivedStruct.Fields["clientNum"].GetNumberValue())
		if clientNum != i {
			t.Fatalf("Client %d expected clientNum %d, got %d", i, i, clientNum)
		}

		t.Logf("Client %d successfully received correct message: %s", i, actualMsg)
	}
}
