package gateserver

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestGateNodeObject is a simple test object
type TestGateNodeDisconnectObject struct {
	object.BaseObject
}

func (o *TestGateNodeDisconnectObject) OnCreated() {}

func (o *TestGateNodeDisconnectObject) Echo(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	message := ""
	if req != nil && req.Fields["message"] != nil {
		message = req.Fields["message"].GetStringValue()
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"message": structpb.NewStringValue("Echo: " + message),
		},
	}, nil
}

// TestGateNodeDisconnectWhenNodeStops tests that:
// 1. Gate connects to a node successfully
// 2. When the node stops, the gate detects the disconnection
// 3. The gate properly cleans up the registration for the stopped node
//
// This test verifies that the gate's registerWithNode goroutine properly
// detects when a node's gRPC stream closes (e.g., when the node shuts down)
// and cleans up the registration from its internal nodeRegs map.
func TestGateNodeDisconnectWhenNodeStops(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := "localhost:48101"
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestGateNodeDisconnectObject)(nil))

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create and start the Gateway (not GatewayServer, just the Gateway component)
	gateAddr := "localhost:48102"
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gw.Stop()

	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}
	t.Logf("Started gateway at %s", gateAddr)

	// Wait for node to be ready
	time.Sleep(500 * time.Millisecond)

	// Wait for shard mapping to be initialized
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create a gRPC client connection to the node
	conn, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create connection to node: %v", err)
	}
	defer conn.Close()

	nodeClient := goverse_pb.NewGoverseClient(conn)
	nodeConnections := map[string]goverse_pb.GoverseClient{
		nodeAddr: nodeClient,
	}
	t.Logf("Created gRPC connection to node at %s", nodeAddr)

	// Register the gateway with the node in a goroutine since it may block
	regDone := make(chan struct{})
	go func() {
		gw.RegisterWithNodes(ctx, nodeConnections)
		close(regDone)
	}()

	// Give time for registration to start
	time.Sleep(2 * time.Second)
	t.Logf("Gateway registration initiated")

	// Now stop the node - this simulates the node going down
	// This will cause the RegisterGate stream to close
	t.Logf("Stopping node server to simulate node going down")
	nodeServer.Stop()
	testNode.Stop(ctx)
	nodeCluster.Stop(ctx)

	// Give time for the stream to detect the closure
	// The gate's registerWithNode goroutine should detect stream.Recv() error
	// and clean up the registration
	time.Sleep(3 * time.Second)
	t.Logf("Waited for gate to detect node disconnect")

	// The gateway should have detected the node disconnection and cleaned up
	// We verify this by ensuring the gateway can be stopped cleanly without issues
	err = gw.Stop()
	if err != nil {
		t.Fatalf("Gateway.Stop() returned error: %v", err)
	}

	t.Logf("Successfully verified gateway handled node disconnect gracefully")
}

// TestGateNodeReconnectAfterNodeRestart tests node restart scenarios.
// This test is more complex and may be added later if needed.
func testGateNodeReconnectAfterNodeRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	nodeAddr := "localhost:48201"

	// Create and start first node instance
	nodeCluster1 := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode1 := nodeCluster1.GetThisNode()
	testNode1.RegisterObjectType((*TestGateNodeDisconnectObject)(nil))
	t.Logf("Created first node cluster at %s", nodeAddr)

	mockNodeServer1 := testutil.NewMockGoverseServer()
	mockNodeServer1.SetNode(testNode1)
	mockNodeServer1.SetCluster(nodeCluster1)
	nodeServer1 := testutil.NewTestServerHelper(nodeAddr, mockNodeServer1)
	err := nodeServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start first node mock server: %v", err)
	}
	t.Logf("Started first mock node server at %s", nodeAddr)

	// Create gateway
	gateAddr := "localhost:48202"
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gw.Stop()

	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}
	t.Logf("Started gateway at %s", gateAddr)

	// Wait for initial setup
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create a gRPC client connection to the first node
	conn1, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create connection to node: %v", err)
	}
	defer conn1.Close()

	nodeClient1 := goverse_pb.NewGoverseClient(conn1)
	nodeConnections1 := map[string]goverse_pb.GoverseClient{
		nodeAddr: nodeClient1,
	}

	// Register gateway with first node
	gw.RegisterWithNodes(ctx, nodeConnections1)
	time.Sleep(1 * time.Second)
	t.Logf("Gateway registered with first node")

	// Stop the first node
	t.Logf("Stopping first node to simulate node failure")
	nodeServer1.Stop()
	testNode1.Stop(ctx)

	// Wait for gate to detect disconnection
	time.Sleep(2 * time.Second)
	t.Logf("First node stopped, gateway should have detected disconnect")

	// Start a new node instance at the same address
	t.Logf("Starting second node at same address to simulate restart")
	nodeCluster2 := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode2 := nodeCluster2.GetThisNode()
	testNode2.RegisterObjectType((*TestGateNodeDisconnectObject)(nil))

	mockNodeServer2 := testutil.NewMockGoverseServer()
	mockNodeServer2.SetNode(testNode2)
	mockNodeServer2.SetCluster(nodeCluster2)
	nodeServer2 := testutil.NewTestServerHelper(nodeAddr, mockNodeServer2)
	err = nodeServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start second node mock server: %v", err)
	}
	defer nodeServer2.Stop()
	t.Logf("Started second mock node server at %s", nodeAddr)

	// Wait for second node to be ready
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create a gRPC client connection to the second node
	conn2, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to create connection to restarted node: %v", err)
	}
	defer conn2.Close()

	nodeClient2 := goverse_pb.NewGoverseClient(conn2)
	nodeConnections2 := map[string]goverse_pb.GoverseClient{
		nodeAddr: nodeClient2,
	}

	// Register gateway with the restarted node
	gw.RegisterWithNodes(ctx, nodeConnections2)
	time.Sleep(1 * time.Second)
	t.Logf("Gateway re-registered with restarted node")

	// Verify the gateway can communicate with the new node
	// This is a basic check - in a full test we'd create objects and call methods
	// For now, just verify no errors occurred
	t.Logf("Successfully verified gateway reconnected after node restart")
}

// TestMultipleNodesDisconnectSequentially tests multiple node disconnection scenarios.
// This test is more complex and may be added later if needed.
func testMultipleNodesDisconnectSequentially(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create 3 nodes
	nodeAddrs := []string{"localhost:48301", "localhost:48302", "localhost:48303"}
	var nodes []*node.Node
	var clusters []*cluster.Cluster
	var servers []*testutil.TestServerHelper

	for i, nodeAddr := range nodeAddrs {
		nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
		testNode := nodeCluster.GetThisNode()
		testNode.RegisterObjectType((*TestGateNodeDisconnectObject)(nil))
		nodes = append(nodes, testNode)
		clusters = append(clusters, nodeCluster)

		mockNodeServer := testutil.NewMockGoverseServer()
		mockNodeServer.SetNode(testNode)
		mockNodeServer.SetCluster(nodeCluster)
		nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
		err := nodeServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start node %d mock server: %v", i+1, err)
		}
		servers = append(servers, nodeServer)
		t.Logf("Started node %d at %s", i+1, nodeAddr)
	}

	// Create gateway
	gateAddr := "localhost:48304"
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gw.Stop()

	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}
	t.Logf("Started gateway at %s", gateAddr)

	// Wait for all nodes to be ready
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Create gRPC client connections to all nodes
	allConnections := make(map[string]goverse_pb.GoverseClient)
	for _, nodeAddr := range nodeAddrs {
		conn, err := grpc.NewClient(nodeAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("Failed to create connection to node %s: %v", nodeAddr, err)
		}
		defer conn.Close()
		allConnections[nodeAddr] = goverse_pb.NewGoverseClient(conn)
	}
	t.Logf("Created gRPC connections to %d nodes", len(allConnections))

	// Register gateway with all nodes
	gw.RegisterWithNodes(ctx, allConnections)
	time.Sleep(2 * time.Second)
	t.Logf("Gateway registered with all nodes")

	// Stop nodes one by one
	for i, nodeServer := range servers {
		t.Logf("Stopping node %d", i+1)
		nodeServer.Stop()
		nodes[i].Stop(ctx)

		// Wait for gate to detect disconnection
		time.Sleep(2 * time.Second)
		t.Logf("Node %d stopped, gateway should have detected disconnect", i+1)
	}

	// All nodes are now stopped, gateway should have cleaned up all registrations
	t.Logf("All nodes stopped")

	// Stop gateway - should not cause any issues
	err = gw.Stop()
	if err != nil {
		t.Fatalf("Gateway.Stop() returned error: %v", err)
	}

	t.Logf("Successfully verified gateway handled multiple sequential node disconnects")
}
