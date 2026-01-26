package gateserver

import (
	"context"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestBroadcastToAllClients tests the complete flow of broadcasting messages to all clients
// Flow:
// 1. Start node and gate
// 2. Multiple clients register to gate
// 3. Node calls BroadcastToAllClients
// 4. All clients receive the broadcast message
func TestBroadcastToAllClientsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create and start node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

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

	// Create and start gate server
	gateAddr := testutil.GetFreeAddress()
	gateConfig := &GateServerConfig{
		AdvertiseAddress: gateAddr,
		ListenAddress:    gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
		NumShards:        testutil.TestNumShards,
	}

	gateServer, err := NewGateServer(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}

	err = gateServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gate server: %v", err)
	}
	defer gateServer.Stop()

	// Wait for gate to register with node
	testutil.WaitForClustersReady(t, nodeCluster, gateServer.cluster)

	// Connect multiple clients to the gate
	numClients := 5
	clients := make([]gate_pb.GateService_RegisterClient, numClients)
	
	for i := 0; i < numClients; i++ {
		conn, err := grpc.NewClient(gateAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("Failed to create grpc client %d: %v", i, err)
		}
		defer conn.Close()

		gateClient := gate_pb.NewGateServiceClient(conn)
		stream, err := gateClient.Register(ctx, &gate_pb.Empty{})
		if err != nil {
			t.Fatalf("Failed to register client %d: %v", i, err)
		}

		// Receive RegisterResponse with client_id
		registerResp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Failed to receive register response for client %d: %v", i, err)
		}

		// Unmarshal RegisterResponse from Any
		var regRespMsg gate_pb.RegisterResponse
		err = registerResp.UnmarshalTo(&regRespMsg)
		if err != nil {
			t.Fatalf("Failed to unmarshal register response for client %d: %v", i, err)
		}

		clientID := regRespMsg.GetClientId()
		if clientID == "" {
			t.Fatalf("Expected non-empty client_id for client %d", i)
		}

		t.Logf("Client %d registered with ID: %s", i, clientID)
		clients[i] = stream
	}

	// Create a test message to broadcast
	testData := map[string]interface{}{
		"type":    "server_announcement",
		"message": "This is a broadcast test message",
	}

	testStruct, err := structpb.NewStruct(testData)
	if err != nil {
		t.Fatalf("Failed to create test message: %v", err)
	}

	// Broadcast message to all clients
	t.Logf("Broadcasting message to all clients")
	err = nodeCluster.BroadcastToAllClients(ctx, testStruct)
	if err != nil {
		t.Fatalf("BroadcastToAllClients failed: %v", err)
	}

	// Verify all clients received the broadcast message
	for i := 0; i < numClients; i++ {
		// Create a timeout context for receiving
		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		defer recvCancel()

		// Receive message from stream
		done := make(chan error, 1)
		var anyMsg *anypb.Any
		clientIdx := i // Capture the loop variable
		go func() {
			var err error
			anyMsg, err = clients[clientIdx].Recv()
			done <- err
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Failed to receive broadcast message on client %d: %v", i, err)
			}
		case <-recvCtx.Done():
			t.Fatalf("Timeout waiting for broadcast message on client %d", i)
		}

		// Verify the message content
		var receivedStruct structpb.Struct
		err = anyMsg.UnmarshalTo(&receivedStruct)
		if err != nil {
			t.Fatalf("Failed to unmarshal broadcast message on client %d: %v", i, err)
		}

		if receivedStruct.Fields["type"].GetStringValue() != "server_announcement" {
			t.Errorf("Client %d: expected type 'server_announcement', got '%s'",
				i, receivedStruct.Fields["type"].GetStringValue())
		}

		if receivedStruct.Fields["message"].GetStringValue() != "This is a broadcast test message" {
			t.Errorf("Client %d: expected message 'This is a broadcast test message', got '%s'",
				i, receivedStruct.Fields["message"].GetStringValue())
		}

		t.Logf("Client %d successfully received broadcast message", i)
	}

	t.Logf("All %d clients successfully received the broadcast message", numClients)
}

// TestBroadcastToAllClients_MultipleGates tests broadcasting to clients across multiple gates
func TestBroadcastToAllClients_MultipleGates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create and start node cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, testPrefix)
	defer nodeCluster.Stop(ctx)

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

	// Create and start two gate servers
	numGates := 2
	gateServers := make([]*GateServer, numGates)
	gateAddrs := make([]string, numGates)

	for i := 0; i < numGates; i++ {
		gateAddr := testutil.GetFreeAddress()
		gateAddrs[i] = gateAddr
		
		gateConfig := &GateServerConfig{
			AdvertiseAddress: gateAddr,
			ListenAddress:    gateAddr,
			EtcdAddress:      "localhost:2379",
			EtcdPrefix:       testPrefix,
			NumShards:        testutil.TestNumShards,
		}

		gateServer, err := NewGateServer(gateConfig)
		if err != nil {
			t.Fatalf("Failed to create gate server %d: %v", i, err)
		}

		err = gateServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start gate server %d: %v", i, err)
		}
		defer gateServer.Stop()

		gateServers[i] = gateServer
		t.Logf("Started gate %d at %s", i, gateAddr)
	}

	// Wait for all gates to register with node
	testutil.WaitForClustersReady(t, nodeCluster, gateServers[0].cluster, gateServers[1].cluster)

	// Connect clients to different gates
	clientsPerGate := 3
	totalClients := numGates * clientsPerGate
	clients := make([]gate_pb.GateService_RegisterClient, totalClients)

	for gateIdx := 0; gateIdx < numGates; gateIdx++ {
		for clientIdx := 0; clientIdx < clientsPerGate; clientIdx++ {
			idx := gateIdx*clientsPerGate + clientIdx
			
			conn, err := grpc.NewClient(gateAddrs[gateIdx], grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to create grpc client %d: %v", idx, err)
			}
			defer conn.Close()

			gateClient := gate_pb.NewGateServiceClient(conn)
			stream, err := gateClient.Register(ctx, &gate_pb.Empty{})
			if err != nil {
				t.Fatalf("Failed to register client %d: %v", idx, err)
			}

			// Receive RegisterResponse
			registerResp, err := stream.Recv()
			if err != nil {
				t.Fatalf("Failed to receive register response for client %d: %v", idx, err)
			}

			var regRespMsg gate_pb.RegisterResponse
			err = registerResp.UnmarshalTo(&regRespMsg)
			if err != nil {
				t.Fatalf("Failed to unmarshal register response for client %d: %v", idx, err)
			}

			clientID := regRespMsg.GetClientId()

			t.Logf("Client %d registered to gate %d with ID: %s", idx, gateIdx, clientID)
			clients[idx] = stream
		}
	}

	// Broadcast message to all clients
	testData := map[string]interface{}{
		"type":    "multi_gate_broadcast",
		"message": "Broadcast across multiple gates",
	}

	testStruct, err := structpb.NewStruct(testData)
	if err != nil {
		t.Fatalf("Failed to create test message: %v", err)
	}

	t.Logf("Broadcasting message to all clients across %d gates", numGates)
	err = nodeCluster.BroadcastToAllClients(ctx, testStruct)
	if err != nil {
		t.Fatalf("BroadcastToAllClients failed: %v", err)
	}

	// Verify all clients received the broadcast message
	for i := 0; i < totalClients; i++ {
		// Create a timeout context for receiving
		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		defer recvCancel()

		// Receive message from stream
		done := make(chan error, 1)
		var anyMsg *anypb.Any
		clientIdx := i // Capture the loop variable
		go func() {
			var err error
			anyMsg, err = clients[clientIdx].Recv()
			done <- err
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Failed to receive broadcast message on client %d: %v", i, err)
			}
		case <-recvCtx.Done():
			t.Fatalf("Timeout waiting for broadcast message on client %d", i)
		}

		var receivedStruct structpb.Struct
		err = anyMsg.UnmarshalTo(&receivedStruct)
		if err != nil {
			t.Fatalf("Failed to unmarshal broadcast message on client %d: %v", i, err)
		}

		if receivedStruct.Fields["type"].GetStringValue() != "multi_gate_broadcast" {
			t.Errorf("Client %d: expected type 'multi_gate_broadcast', got '%s'",
				i, receivedStruct.Fields["type"].GetStringValue())
		}

		t.Logf("Client %d successfully received broadcast message", i)
	}

	t.Logf("All %d clients across %d gates successfully received the broadcast message", totalClients, numGates)
}
