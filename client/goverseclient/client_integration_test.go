package goverseclient

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/gate/gateserver"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestIntegrationObject is a simple test object with methods for testing
type TestIntegrationObject struct {
	object.BaseObject
	callCount int // Track how many times methods are called
}

func (o *TestIntegrationObject) OnCreated() {}

// Echo is a simple method that echoes back the message and tracks call count
// Request format: {message: string}
// Response format: {message: string, callCount: number}
func (o *TestIntegrationObject) Echo(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
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

// Add is a simple method that adds two numbers
// Request format: {a: number, b: number}
// Response format: {result: number}
func (o *TestIntegrationObject) Add(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	var a, b float64
	if req != nil && req.Fields["a"] != nil {
		a = req.Fields["a"].GetNumberValue()
	}
	if req != nil && req.Fields["b"] != nil {
		b = req.Fields["b"].GetNumberValue()
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"result": structpb.NewNumberValue(a + b),
		},
	}, nil
}

// Helper function to create and start a cluster with etcd for testing
func mustNewClusterForClientTest(ctx context.Context, t *testing.T, nodeAddr string, etcdPrefix string) *cluster.Cluster {
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
		NumShards:                     testutil.TestNumShards,
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

// TestClientIntegration tests the goverseclient connecting to an actual cluster
// with gate and node, and performing create/call/delete operations on objects.
// This is an integration test that requires etcd to be running.
func TestClientIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewClusterForClientTest(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestIntegrationObject)(nil))

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

	// Create and start the real GateServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &gateserver.GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer, err := gateserver.NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gate server (non-blocking)
	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}
	t.Logf("Started real gate server at %s", gateAddr)

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	// Create a goverseclient and connect to the gate
	client, err := NewClient([]string{gateAddr})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	// Connect to the gate
	err = client.Connect(ctx)
	if err != nil {
		t.Fatalf("Failed to connect to gate: %v", err)
	}
	t.Logf("Client connected to gate at %s, client ID: %s", gateAddr, client.ClientID())

	// Verify client is connected
	if !client.IsConnected() {
		t.Fatal("Client should be connected after Connect()")
	}
	if client.ClientID() == "" {
		t.Fatal("Client should have a client ID after Connect()")
	}
	if client.CurrentAddress() != gateAddr {
		t.Fatalf("Client address mismatch: got %s, want %s", client.CurrentAddress(), gateAddr)
	}

	t.Run("CreateObject", func(t *testing.T) {
		objID := "test-client-create-1"

		// Create the object via the client
		createdID, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		if createdID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createdID)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully created object %s via goverseclient", objID)
	})

	t.Run("CallObject", func(t *testing.T) {
		objID := "test-client-call-1"

		// Create the object first
		_, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Call Echo method
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from goverseclient"),
			},
		}

		resp, err := client.CallObject(ctx, "TestIntegrationObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject Echo failed: %v", err)
		}

		echoResp, ok := resp.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct response, got %T", resp)
		}

		expectedMsg := "Echo: Hello from goverseclient"
		actualMsg := echoResp.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
		}

		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1, got %d", callCount)
		}

		t.Logf("Successfully called Echo via goverseclient, response: %s (call count: %d)", actualMsg, callCount)
	})

	t.Run("CallObjectMultipleTimes", func(t *testing.T) {
		objID := "test-client-multi-call-1"

		// Create the object first
		_, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Call Echo method multiple times
		for i := 1; i <= 3; i++ {
			echoReq := &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"message": structpb.NewStringValue(fmt.Sprintf("Call %d", i)),
				},
			}

			resp, err := client.CallObject(ctx, "TestIntegrationObject", objID, "Echo", echoReq)
			if err != nil {
				t.Fatalf("CallObject Echo (call %d) failed: %v", i, err)
			}

			echoResp, ok := resp.(*structpb.Struct)
			if !ok {
				t.Fatalf("Expected *structpb.Struct response, got %T", resp)
			}

			callCount := int(echoResp.Fields["callCount"].GetNumberValue())
			if callCount != i {
				t.Fatalf("Expected call count %d, got %d", i, callCount)
			}
		}

		t.Logf("Successfully called Echo 3 times via goverseclient")
	})

	t.Run("CallObjectAdd", func(t *testing.T) {
		objID := "test-client-add-1"

		// Create the object first
		_, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)

		// Call Add method
		addReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"a": structpb.NewNumberValue(10),
				"b": structpb.NewNumberValue(32),
			},
		}

		resp, err := client.CallObject(ctx, "TestIntegrationObject", objID, "Add", addReq)
		if err != nil {
			t.Fatalf("CallObject Add failed: %v", err)
		}

		addResp, ok := resp.(*structpb.Struct)
		if !ok {
			t.Fatalf("Expected *structpb.Struct response, got %T", resp)
		}

		result := addResp.Fields["result"].GetNumberValue()
		expected := float64(42)
		if result != expected {
			t.Fatalf("Expected result %v, got %v", expected, result)
		}

		t.Logf("Successfully called Add(10, 32) = 42 via goverseclient")
	})

	t.Run("DeleteObject", func(t *testing.T) {
		objID := "test-client-delete-1"

		// Create the object first
		_, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Created object %s", objID)

		// Delete the object via the client
		err = client.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("DeleteObject failed: %v", err)
		}

		// Wait for object deletion on the node
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)

		t.Logf("Successfully deleted object %s via goverseclient", objID)
	})

	t.Run("CreateCallDelete", func(t *testing.T) {
		// Test the complete lifecycle: create -> call -> delete
		objID := "test-client-lifecycle-1"

		// Create
		_, err := client.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject failed: %v", err)
		}
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Created object %s", objID)

		// Call
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Lifecycle test"),
			},
		}
		resp, err := client.CallObject(ctx, "TestIntegrationObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject Echo failed: %v", err)
		}
		echoResp := resp.(*structpb.Struct)
		t.Logf("Called Echo: %s", echoResp.Fields["message"].GetStringValue())

		// Delete
		err = client.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("DeleteObject failed: %v", err)
		}
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Deleted object %s", objID)

		t.Logf("Successfully completed full object lifecycle via goverseclient")
	})
}

// TestClientIntegrationMultipleGates tests goverseclient connecting to multiple gates
// with failover capability
func TestClientIntegrationMultipleGates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewClusterForClientTest(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestIntegrationObject)(nil))

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })

	// Create and start two gate servers
	gateAddr1 := testutil.GetFreeAddress()
	gateAddr2 := testutil.GetFreeAddress()

	// Gate 1
	gwServerConfig1 := &gateserver.GateServerConfig{
		ListenAddress:    gateAddr1,
		AdvertiseAddress: gateAddr1,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer1, err := gateserver.NewGateServer(gwServerConfig1)
	if err != nil {
		t.Fatalf("Failed to create gate server 1: %v", err)
	}
	t.Cleanup(func() { gwServer1.Stop() })

	if err := gwServer1.Start(ctx); err != nil {
		t.Fatalf("Gate server 1 failed to start: %v", err)
	}
	t.Logf("Started gate server 1 at %s", gateAddr1)

	// Gate 2
	gwServerConfig2 := &gateserver.GateServerConfig{
		ListenAddress:    gateAddr2,
		AdvertiseAddress: gateAddr2,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer2, err := gateserver.NewGateServer(gwServerConfig2)
	if err != nil {
		t.Fatalf("Failed to create gate server 2: %v", err)
	}
	t.Cleanup(func() { gwServer2.Stop() })

	if err := gwServer2.Start(ctx); err != nil {
		t.Fatalf("Gate server 2 failed to start: %v", err)
	}
	t.Logf("Started gate server 2 at %s", gateAddr2)

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	t.Run("ConnectToFirstAvailableGate", func(t *testing.T) {
		// Create a client with multiple gate addresses
		client, err := NewClient([]string{gateAddr1, gateAddr2})
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		// Connect - should connect to first available gate
		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect to gate: %v", err)
		}

		// Verify connected to first gate
		currentAddr := client.CurrentAddress()
		if currentAddr != gateAddr1 {
			t.Fatalf("Expected to connect to first gate %s, got %s", gateAddr1, currentAddr)
		}

		t.Logf("Client connected to first available gate: %s", currentAddr)
	})

	t.Run("ConnectWithFirstGateUnavailable", func(t *testing.T) {
		// Create a client with a non-existent first address
		nonExistentAddr := testutil.GetFreeAddress()
		client, err := NewClient([]string{nonExistentAddr, gateAddr2})
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		// Connect - should fail first, then connect to second gate
		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect to gate: %v", err)
		}

		// Verify connected to second gate
		currentAddr := client.CurrentAddress()
		if currentAddr != gateAddr2 {
			t.Fatalf("Expected to connect to second gate %s, got %s", gateAddr2, currentAddr)
		}

		t.Logf("Client connected to second gate after first failed: %s", currentAddr)
	})

	t.Run("OperationsViaMultipleClients", func(t *testing.T) {
		// Create two clients connected to different gates
		client1, err := NewClient([]string{gateAddr1})
		if err != nil {
			t.Fatalf("Failed to create client 1: %v", err)
		}
		defer client1.Close()

		err = client1.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect client 1: %v", err)
		}

		client2, err := NewClient([]string{gateAddr2})
		if err != nil {
			t.Fatalf("Failed to create client 2: %v", err)
		}
		defer client2.Close()

		err = client2.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect client 2: %v", err)
		}

		t.Logf("Client 1 connected to %s, Client 2 connected to %s",
			client1.CurrentAddress(), client2.CurrentAddress())

		// Create an object via client 1
		objID := "test-multi-client-obj-1"
		_, err = client1.CreateObject(ctx, "TestIntegrationObject", objID)
		if err != nil {
			t.Fatalf("CreateObject via client 1 failed: %v", err)
		}
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Client 1 created object %s", objID)

		// Call the object via client 2 (different gate, same cluster)
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from client 2"),
			},
		}
		resp, err := client2.CallObject(ctx, "TestIntegrationObject", objID, "Echo", echoReq)
		if err != nil {
			t.Fatalf("CallObject via client 2 failed: %v", err)
		}

		echoResp := resp.(*structpb.Struct)
		t.Logf("Client 2 called object %s via different gate: %s",
			objID, echoResp.Fields["message"].GetStringValue())

		// Delete the object via client 2
		err = client2.DeleteObject(ctx, objID)
		if err != nil {
			t.Fatalf("DeleteObject via client 2 failed: %v", err)
		}
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Client 2 deleted object %s", objID)

		t.Logf("Successfully performed cross-gate operations via multiple clients")
	})
}

// TestClientIntegrationWithCallbacks tests goverseclient callbacks
func TestClientIntegrationWithCallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewClusterForClientTest(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()

	// Register test object type on the node
	testNode.RegisterObjectType((*TestIntegrationObject)(nil))

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })

	// Create and start the gate server
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &gateserver.GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer, err := gateserver.NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	t.Run("OnConnectCallback", func(t *testing.T) {
		var connectedClientID string
		var callbackCalled bool

		client, err := NewClient(
			[]string{gateAddr},
			WithOnConnect(func(clientID string) {
				callbackCalled = true
				connectedClientID = clientID
			}),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}

		if !callbackCalled {
			t.Fatal("OnConnect callback was not called")
		}

		if connectedClientID == "" {
			t.Fatal("OnConnect callback was called with empty clientID")
		}

		if connectedClientID != client.ClientID() {
			t.Fatalf("Callback clientID %s doesn't match client.ClientID() %s",
				connectedClientID, client.ClientID())
		}

		t.Logf("OnConnect callback correctly called with clientID: %s", connectedClientID)
	})
}

// TestClientIntegrationPushMessages tests that pushed messages from the cluster are received
// by the client via both the OnMessage callback and the MessageChan() channel.
// This tests the client's ability to receive server-initiated push messages.
func TestClientIntegrationPushMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewClusterForClientTest(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node (needed for some operations)
	testNode.RegisterObjectType((*TestIntegrationObject)(nil))

	// Start mock gRPC server for the node
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

	// Create and start the real GateServer
	gateAddr := testutil.GetFreeAddress()
	gwServerConfig := &gateserver.GateServerConfig{
		ListenAddress:    gateAddr,
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
		NumShards:        testutil.TestNumShards,
	}
	gwServer, err := gateserver.NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}
	t.Logf("Started real gate server at %s", gateAddr)

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	// Wait for gate to register with the node
	testutil.WaitFor(t, 10*time.Second, "gate to register with node", func() bool {
		return nodeCluster.IsGateConnected(gateAddr)
	})
	t.Logf("Gate %s registered with node %s", gateAddr, nodeAddr)

	t.Run("PushMessageViaOnMessageCallback", func(t *testing.T) {
		var receivedMessages []proto.Message
		var mu sync.Mutex

		// Create a client with OnMessage callback
		client, err := NewClient(
			[]string{gateAddr},
			WithOnMessage(func(msg proto.Message) {
				mu.Lock()
				receivedMessages = append(receivedMessages, msg)
				mu.Unlock()
			}),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		clientID := client.ClientID()
		t.Logf("Client connected with ID: %s", clientID)

		// Push a message directly via the cluster to the connected client
		pushMsg := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"type":      structpb.NewStringValue("notification"),
				"message":   structpb.NewStringValue("Hello via callback!"),
				"timestamp": structpb.NewNumberValue(float64(time.Now().Unix())),
			},
		}

		err = nodeCluster.PushMessageToClients(ctx, []string{clientID}, pushMsg)
		if err != nil {
			t.Fatalf("PushMessageToClient failed: %v", err)
		}
		t.Logf("Pushed message to client %s", clientID)

		// Wait for the push message to be received via callback
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			count := len(receivedMessages)
			mu.Unlock()

			if count > 0 {
				break
			}

			if time.Now().After(deadline) {
				t.Fatal("Timeout waiting for push message via OnMessage callback")
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Verify the received message
		mu.Lock()
		if len(receivedMessages) != 1 {
			t.Fatalf("Expected 1 message, got %d", len(receivedMessages))
		}
		receivedStruct, ok := receivedMessages[0].(*structpb.Struct)
		mu.Unlock()

		if !ok {
			t.Fatalf("Expected *structpb.Struct, got %T", receivedMessages[0])
		}

		msgType := receivedStruct.Fields["type"].GetStringValue()
		msgContent := receivedStruct.Fields["message"].GetStringValue()

		if msgType != "notification" {
			t.Fatalf("Expected type 'notification', got %q", msgType)
		}
		if msgContent != "Hello via callback!" {
			t.Fatalf("Expected message 'Hello via callback!', got %q", msgContent)
		}

		t.Logf("Successfully received push message via OnMessage callback: type=%s, message=%s", msgType, msgContent)
	})

	t.Run("PushMessageViaMessageChan", func(t *testing.T) {
		// Create a client without OnMessage callback to test MessageChan
		client, err := NewClient([]string{gateAddr})
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		clientID := client.ClientID()
		t.Logf("Client connected with ID: %s", clientID)

		// Push a message directly via the cluster to the connected client
		pushMsg := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"type":      structpb.NewStringValue("notification"),
				"message":   structpb.NewStringValue("Hello via channel!"),
				"timestamp": structpb.NewNumberValue(float64(time.Now().Unix())),
			},
		}

		err = nodeCluster.PushMessageToClients(ctx, []string{clientID}, pushMsg)
		if err != nil {
			t.Fatalf("PushMessageToClient failed: %v", err)
		}
		t.Logf("Pushed message to client %s", clientID)

		// Receive the push message via MessageChan()
		msgChan := client.MessageChan()
		select {
		case msg := <-msgChan:
			receivedStruct, ok := msg.(*structpb.Struct)
			if !ok {
				t.Fatalf("Expected *structpb.Struct, got %T", msg)
			}

			msgType := receivedStruct.Fields["type"].GetStringValue()
			msgContent := receivedStruct.Fields["message"].GetStringValue()

			if msgType != "notification" {
				t.Fatalf("Expected type 'notification', got %q", msgType)
			}
			if msgContent != "Hello via channel!" {
				t.Fatalf("Expected message 'Hello via channel!', got %q", msgContent)
			}

			t.Logf("Successfully received push message via MessageChan: type=%s, message=%s", msgType, msgContent)

		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for push message via MessageChan")
		}
	})

	t.Run("MultiplePushMessages", func(t *testing.T) {
		var receivedMessages []proto.Message
		var mu sync.Mutex

		// Create a client with OnMessage callback
		client, err := NewClient(
			[]string{gateAddr},
			WithOnMessage(func(msg proto.Message) {
				mu.Lock()
				receivedMessages = append(receivedMessages, msg)
				mu.Unlock()
			}),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		clientID := client.ClientID()
		t.Logf("Client connected with ID: %s", clientID)

		// Push multiple messages to the connected client
		pushCount := 3
		for i := 0; i < pushCount; i++ {
			pushMsg := &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"type":    structpb.NewStringValue("notification"),
					"message": structpb.NewStringValue(fmt.Sprintf("Notification #%d", i+1)),
					"index":   structpb.NewNumberValue(float64(i)),
				},
			}

			err = nodeCluster.PushMessageToClients(ctx, []string{clientID}, pushMsg)
			if err != nil {
				t.Fatalf("PushMessageToClient failed for message %d: %v", i, err)
			}
		}
		t.Logf("Pushed %d messages to client %s", pushCount, clientID)

		// Wait for all push messages to be received
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			count := len(receivedMessages)
			mu.Unlock()

			if count >= pushCount {
				break
			}

			if time.Now().After(deadline) {
				mu.Lock()
				receivedCount := len(receivedMessages)
				mu.Unlock()
				t.Fatalf("Timeout waiting for push messages, received %d of %d", receivedCount, pushCount)
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Verify all received messages
		mu.Lock()
		defer mu.Unlock()

		if len(receivedMessages) != pushCount {
			t.Fatalf("Expected %d messages, got %d", pushCount, len(receivedMessages))
		}

		for i, msg := range receivedMessages {
			receivedStruct, ok := msg.(*structpb.Struct)
			if !ok {
				t.Fatalf("Message %d: Expected *structpb.Struct, got %T", i, msg)
			}

			msgType := receivedStruct.Fields["type"].GetStringValue()
			msgContent := receivedStruct.Fields["message"].GetStringValue()
			index := int(receivedStruct.Fields["index"].GetNumberValue())

			if msgType != "notification" {
				t.Fatalf("Message %d: Expected type 'notification', got %q", i, msgType)
			}

			// Messages may arrive out of order, so just verify content format
			if msgContent == "" {
				t.Fatalf("Message %d: Expected non-empty message", i)
			}

			t.Logf("Received message %d: type=%s, message=%s, index=%d", i, msgType, msgContent, index)
		}

		t.Logf("Successfully received all %d push messages", pushCount)
	})

	t.Run("PushMessageBothCallbackAndChannel", func(t *testing.T) {
		// Test that messages are received via both callback and channel
		var callbackMessages []proto.Message
		var mu sync.Mutex

		client, err := NewClient(
			[]string{gateAddr},
			WithOnMessage(func(msg proto.Message) {
				mu.Lock()
				callbackMessages = append(callbackMessages, msg)
				mu.Unlock()
			}),
		)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		err = client.Connect(ctx)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		clientID := client.ClientID()
		t.Logf("Client connected with ID: %s", clientID)

		// Push a message
		pushMsg := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"type":    structpb.NewStringValue("dual"),
				"message": structpb.NewStringValue("Hello to both!"),
			},
		}

		err = nodeCluster.PushMessageToClients(ctx, []string{clientID}, pushMsg)
		if err != nil {
			t.Fatalf("PushMessageToClient failed: %v", err)
		}
		t.Logf("Pushed message to client %s", clientID)

		// Wait for message via callback
		deadline := time.Now().Add(5 * time.Second)
		for {
			mu.Lock()
			count := len(callbackMessages)
			mu.Unlock()

			if count > 0 {
				break
			}

			if time.Now().After(deadline) {
				t.Fatal("Timeout waiting for push message via callback")
			}
			time.Sleep(50 * time.Millisecond)
		}

		// Also receive via channel (message should be in both)
		msgChan := client.MessageChan()
		select {
		case msg := <-msgChan:
			receivedStruct, ok := msg.(*structpb.Struct)
			if !ok {
				t.Fatalf("Expected *structpb.Struct, got %T", msg)
			}

			msgType := receivedStruct.Fields["type"].GetStringValue()
			if msgType != "dual" {
				t.Fatalf("Expected type 'dual', got %q", msgType)
			}
			t.Logf("Received message via MessageChan: type=%s", msgType)

		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for push message via MessageChan")
		}

		// Verify callback also received it
		mu.Lock()
		if len(callbackMessages) != 1 {
			t.Fatalf("Expected 1 callback message, got %d", len(callbackMessages))
		}
		callbackStruct, ok := callbackMessages[0].(*structpb.Struct)
		mu.Unlock()

		if !ok {
			t.Fatalf("Expected *structpb.Struct in callback, got %T", callbackMessages[0])
		}

		callbackType := callbackStruct.Fields["type"].GetStringValue()
		if callbackType != "dual" {
			t.Fatalf("Expected callback type 'dual', got %q", callbackType)
		}

		t.Logf("Successfully received push message via both OnMessage callback and MessageChan")
	})
}
