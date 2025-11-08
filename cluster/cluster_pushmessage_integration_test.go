package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/client"
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

	// Create clusters using mustNewCluster
	cluster1 := mustNewCluster(ctx, t, "localhost:47011", testPrefix)
	cluster2 := mustNewCluster(ctx, t, "localhost:47012", testPrefix)

	// Register client types on the nodes (can be done after node start)
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.RegisterClientType((*client.BaseClient)(nil))
	node2.RegisterClientType((*client.BaseClient)(nil))

	// Wait for nodes to discover each other
	time.Sleep(1 * time.Second)

	// Start mock gRPC servers for both nodes
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47011", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := testutil.NewTestServerHelper("localhost:47012", mockServer2)
	err = testServer2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	// Wait for node connections to be established (cluster.Start already handles StartNodeConnections)
	time.Sleep(5 * time.Second)

	// Register a client on node2
	clientObj, err := node2.RegisterClient(ctx)
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
	const messageDelay = 1 * time.Millisecond // Small delay to avoid overwhelming channel buffer
	const receiveTimeout = 10 * time.Second   // Timeout for receiving all messages

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
