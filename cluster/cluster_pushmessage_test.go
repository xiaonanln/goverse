package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/client"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestPushMessageToClient_NoNode tests that PushMessageToClient fails when thisNode is not set
func TestPushMessageToClient_NoNode(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_NoNode")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	err := c.PushMessageToClient(ctx, "localhost:7001/test-client", testMsg)
	if err == nil {
		t.Fatal("PushMessageToClient should fail when thisNode is not set")
	}
}

// TestPushMessageToClient_InvalidClientID tests that PushMessageToClient fails with invalid client ID format
func TestPushMessageToClient_InvalidClientID(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_InvalidClientID")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	tests := []struct {
		name     string
		clientID string
	}{
		{"no slash", "invalidclientid"},
		{"empty", ""},
		{"only slash", "/"},
		{"slash at end", "localhost:7001/"},
		{"slash at start", "/uniqueid"},
		{"empty node", "/abc123"},
		{"empty unique id", "localhost:7001/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.PushMessageToClient(ctx, tt.clientID, testMsg)
			if err == nil {
				t.Fatalf("PushMessageToClient should fail with invalid client ID: %s", tt.clientID)
			}
		})
	}
}

// TestPushMessageToClient_LocalClient tests pushing to a client on the same node
func TestPushMessageToClient_LocalClient(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_LocalClient")

	// Register client type
	testNode.RegisterClientType((*client.BaseClient)(nil))

	// Create a client
	clientID, messageChan, err := testNode.RegisterClient(ctx)
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}

	// Create test message
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName:  "TestUser",
			Message:   "Hello, World!",
			Timestamp: 12345,
		},
	}

	// Push message through cluster API
	err = c.PushMessageToClient(ctx, clientID, testMsg)
	if err != nil {
		t.Fatalf("Failed to push message to local client: %v", err)
	}

	// Verify message was received
	select {
	case msg := <-messageChan:
		notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
		if !ok {
			t.Fatalf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg)
		}
		if notification.Message.UserName != "TestUser" {
			t.Fatalf("Expected UserName 'TestUser', got '%s'", notification.Message.UserName)
		}
		if notification.Message.Message != "Hello, World!" {
			t.Fatalf("Expected Message 'Hello, World!', got '%s'", notification.Message.Message)
		}
	default:
		t.Fatal("No message received on client channel")
	}
}

// TestPushMessageToClient_ClientNotFound tests pushing to a non-existent client
func TestPushMessageToClient_ClientNotFound(t *testing.T) {
	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_ClientNotFound")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Try to push to a client that doesn't exist (but with valid format for local node)
	err := c.PushMessageToClient(ctx, "localhost:7000/non-existent-client", testMsg)
	if err == nil {
		t.Fatal("Expected error when pushing to non-existent client")
	}
}
