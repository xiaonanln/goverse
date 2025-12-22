package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

// TestPushMessageToClient_NoNode tests that PushMessageToClient fails when thisNode is not set
func TestPushMessageToClient_NoNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_NoNode")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	err := c.PushMessageToClients(ctx, []string{"localhost:7001/test-client"}, testMsg)
	if err == nil {
		t.Fatal("PushMessageToClient should fail when thisNode is not set")
	}
}

// TestPushMessageToClient_InvalidClientID tests that PushMessageToClient fails with invalid client ID format
func TestPushMessageToClient_InvalidClientID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
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
			err := c.PushMessageToClients(ctx, []string{tt.clientID}, testMsg)
			if err == nil {
				t.Fatalf("PushMessageToClient should fail with invalid client ID: %s", tt.clientID)
			}
		})
	}
}

// TestPushMessageToClient_ClientNotFound tests pushing to a non-existent client
func TestPushMessageToClient_ClientNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_ClientNotFound")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Try to push to a client that doesn't exist (but with valid format for local node)
	err := c.PushMessageToClients(ctx, []string{"localhost:7000/non-existent-client"}, testMsg)
	if err == nil {
		t.Fatal("Expected error when pushing to non-existent client")
	}
}

// TestPushMessageToClient_EmptyClientList tests that empty client list is a no-op
func TestPushMessageToClient_EmptyClientList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_EmptyClientList")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Empty client list should be a no-op and return nil
	err := c.PushMessageToClients(ctx, []string{}, testMsg)
	if err != nil {
		t.Fatalf("PushMessageToClient should succeed with empty client list, got error: %v", err)
	}
}

// TestPushMessageToClient_MultipleClients tests grouping clients by gate
func TestPushMessageToClient_MultipleClients(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_MultipleClients")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Test with multiple clients on different gates
	clientIDs := []string{
		"localhost:7001/client1",
		"localhost:7001/client2",
		"localhost:7002/client3",
	}

	// Should fail since gates are not connected, but should not panic
	err := c.PushMessageToClients(ctx, clientIDs, testMsg)
	// We expect an error since the gates are not actually connected
	if err == nil {
		t.Fatal("Expected error when pushing to unconnected gates")
	}
}

