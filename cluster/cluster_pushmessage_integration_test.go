package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/node"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestDistributedPushMessageToClient tests pushing messages to clients on different nodes
// This test requires a running etcd instance at localhost:2379
func TestDistributedPushMessageToClient(t *testing.T) {
	t.Parallel()
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster 1
	cluster1 := newClusterForTesting("TestCluster1")
	etcdMgr1, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 1: %v", err)
	}
	cluster1.SetEtcdManager(etcdMgr1)

	node1 := node.NewNode("localhost:47011")
	cluster1.SetThisNode(node1)
	node1.RegisterClientType((*client.BaseClient)(nil))

	err = node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	defer node1.Stop(ctx)

	err = cluster1.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	defer cluster1.CloseEtcd()

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	defer cluster1.UnregisterNode(ctx)

	err = cluster1.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create cluster 2
	cluster2 := newClusterForTesting("TestCluster2")
	etcdMgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager 2: %v", err)
	}
	cluster2.SetEtcdManager(etcdMgr2)

	node2 := node.NewNode("localhost:47012")
	cluster2.SetThisNode(node2)
	node2.RegisterClientType((*client.BaseClient)(nil))

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	defer node2.Stop(ctx)

	err = cluster2.ConnectEtcd()
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	defer cluster2.CloseEtcd()

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	defer cluster2.UnregisterNode(ctx)

	err = cluster2.WatchNodes(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster2: %v", err)
	}

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC servers for both nodes
	mockServer1 := NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := NewTestServerHelper("localhost:47011", mockServer1)
	err = testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	defer testServer1.Stop()

	mockServer2 := NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := NewTestServerHelper("localhost:47012", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	defer testServer2.Stop()

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Start NodeConnections for both clusters (needed for routing)
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster1: %v", err)
	}
	defer cluster1.StopNodeConnections()

	err = cluster2.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster2: %v", err)
	}
	defer cluster2.StopNodeConnections()

	// Wait for connections to be established
	time.Sleep(1 * time.Second)

	// Register a client on node2
	clientObj, err := node2.RegisterClient()
	if err != nil {
		t.Fatalf("Failed to register client on node2: %v", err)
	}
	clientID := clientObj.Id()

	// Verify the client ID has the correct format
	if len(clientID) < len("localhost:47012/") {
		t.Fatalf("Client ID format is invalid: %s", clientID)
	}
	if clientID[:len("localhost:47012/")] != "localhost:47012/" {
		t.Fatalf("Client ID should start with node2 address, got: %s", clientID)
	}

	// Create test message
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName:  "TestUser",
			Message:   "Cross-node message!",
			Timestamp: 99999,
		},
	}

	// Push message from cluster1 (node1) to client on node2
	err = cluster1.PushMessageToClient(ctx, clientID, testMsg)
	if err != nil {
		t.Fatalf("Failed to push message from node1 to client on node2: %v", err)
	}

	// Verify message was received on node2
	select {
	case msg := <-clientObj.MessageChan():
		notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
		if !ok {
			t.Fatalf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg)
		}
		if notification.Message.UserName != "TestUser" {
			t.Errorf("Expected UserName 'TestUser', got '%s'", notification.Message.UserName)
		}
		if notification.Message.Message != "Cross-node message!" {
			t.Errorf("Expected Message 'Cross-node message!', got '%s'", notification.Message.Message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for message on client channel")
	}

	// Test pushing from node2 to itself (local push through cluster API)
	localTestMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName:  "LocalUser",
			Message:   "Local message!",
			Timestamp: 88888,
		},
	}

	err = cluster2.PushMessageToClient(ctx, clientID, localTestMsg)
	if err != nil {
		t.Fatalf("Failed to push message locally on node2: %v", err)
	}

	// Verify local message was received
	select {
	case msg := <-clientObj.MessageChan():
		notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
		if !ok {
			t.Fatalf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg)
		}
		if notification.Message.UserName != "LocalUser" {
			t.Errorf("Expected UserName 'LocalUser', got '%s'", notification.Message.UserName)
		}
		if notification.Message.Message != "Local message!" {
			t.Errorf("Expected Message 'Local message!', got '%s'", notification.Message.Message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for local message on client channel")
	}
}
