package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/node"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestDistributedPushMessageToClient tests pushing messages to clients on different nodes
// This test requires a running etcd instance at localhost:2379
// Note: This test does NOT run in parallel because it uses mock servers on specific ports
func TestDistributedPushMessageToClient(t *testing.T) {
	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create cluster 1
	cluster1 := newClusterForTesting("TestCluster1")

	node1 := node.NewNode("localhost:47011")
	cluster1.SetThisNode(node1)
	node1.RegisterClientType((*client.BaseClient)(nil))

	err := node1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node1: %v", err)
	}
	t.Cleanup(func() { node1.Stop(ctx) })

	err = cluster1.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster1: %v", err)
	}
	t.Cleanup(func() { cluster1.CloseEtcd() })

	err = cluster1.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}
	t.Cleanup(func() { cluster1.UnregisterNode(ctx) })

	err = cluster1.StartWatching(ctx)
	if err != nil {
		t.Fatalf("Failed to start watching nodes for cluster1: %v", err)
	}

	// Create cluster 2
	cluster2 := newClusterForTesting("TestCluster2")

	node2 := node.NewNode("localhost:47012")
	cluster2.SetThisNode(node2)
	node2.RegisterClientType((*client.BaseClient)(nil))

	err = node2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node2: %v", err)
	}
	t.Cleanup(func() { node2.Stop(ctx) })

	err = cluster2.ConnectEtcd("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to connect etcd for cluster2: %v", err)
	}
	t.Cleanup(func() { cluster2.CloseEtcd() })

	err = cluster2.RegisterNode(ctx)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}
	t.Cleanup(func() { cluster2.UnregisterNode(ctx) })

	err = cluster2.StartWatching(ctx)
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
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := NewTestServerHelper("localhost:47012", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	// Wait for servers to be ready
	time.Sleep(500 * time.Millisecond)

	// Start NodeConnections for both clusters (needed for routing)
	err = cluster1.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster1: %v", err)
	}
	t.Cleanup(func() { cluster1.StopNodeConnections() })

	err = cluster2.StartNodeConnections(ctx)
	if err != nil {
		t.Fatalf("Failed to start node connections for cluster2: %v", err)
	}
	t.Cleanup(func() { cluster2.StopNodeConnections() })

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

	// Push 100 messages: 50 cross-node and 50 local
	const numMessages = 100
	const numCrossNode = 50
	const numLocal = 50
	const messageDelay = 1 * time.Millisecond  // Small delay to avoid overwhelming channel buffer
	const receiveTimeout = 10 * time.Second    // Timeout for receiving all messages

	// Track received messages
	type messageStats struct {
		total     int
		crossNode int
		local     int
		errors    []string
	}
	stats := &messageStats{}

	// Consume messages concurrently to avoid channel buffer overflow
	done := make(chan bool)
	go func() {
		timeout := time.After(receiveTimeout)
		for stats.total < numMessages {
			select {
			case msg := <-clientObj.MessageChan():
				notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
				if !ok {
					stats.errors = append(stats.errors, fmt.Sprintf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg))
					continue
				}
				if notification.Message.UserName == "CrossNodeUser" {
					stats.crossNode++
				} else if notification.Message.UserName == "LocalUser" {
					stats.local++
				} else {
					stats.errors = append(stats.errors, fmt.Sprintf("Unexpected UserName: %s", notification.Message.UserName))
				}
				stats.total++
			case <-timeout:
				stats.errors = append(stats.errors, fmt.Sprintf("Timeout waiting for messages. Received %d/%d messages (CrossNode: %d, Local: %d)",
					stats.total, numMessages, stats.crossNode, stats.local))
				done <- false
				return
			}
		}
		done <- true
	}()

	// Push cross-node messages from cluster1 (node1) to client on node2
	for i := 0; i < numCrossNode; i++ {
		testMsg := &chat_pb.Client_NewMessageNotification{
			Message: &chat_pb.ChatMessage{
				UserName:  "CrossNodeUser",
				Message:   fmt.Sprintf("Cross-node message %d", i),
				Timestamp: int64(10000 + i),
			},
		}
		err = cluster1.PushMessageToClient(ctx, clientID, testMsg)
		if err != nil {
			t.Fatalf("Failed to push cross-node message %d from node1 to client on node2: %v", i, err)
		}
		time.Sleep(messageDelay)
	}

	// Push local messages from cluster2 (node2) to client on node2
	for i := 0; i < numLocal; i++ {
		localTestMsg := &chat_pb.Client_NewMessageNotification{
			Message: &chat_pb.ChatMessage{
				UserName:  "LocalUser",
				Message:   fmt.Sprintf("Local message %d", i),
				Timestamp: int64(20000 + i),
			},
		}
		err = cluster2.PushMessageToClient(ctx, clientID, localTestMsg)
		if err != nil {
			t.Fatalf("Failed to push local message %d on node2: %v", i, err)
		}
		time.Sleep(messageDelay)
	}

	// Wait for all messages to be received
	success := <-done
	
	// Report any errors collected by the goroutine
	for _, err := range stats.errors {
		t.Error(err)
	}
	
	if !success {
		t.Fatal("Failed to receive all messages")
	}

	// Verify counts
	if stats.crossNode != numCrossNode {
		t.Errorf("Expected %d cross-node messages, got %d", numCrossNode, stats.crossNode)
	}
	if stats.local != numLocal {
		t.Errorf("Expected %d local messages, got %d", numLocal, stats.local)
	}
	t.Logf("Successfully received all %d messages (CrossNode: %d, Local: %d)",
		numMessages, stats.crossNode, stats.local)
}
