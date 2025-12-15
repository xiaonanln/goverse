package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestPushMessageToClient_NoNode tests that PushMessageToClient fails when thisNode is not set
func TestPushMessageToClient_NoNode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:7000", testutil.TestNumShards)
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
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:7000", testutil.TestNumShards)
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

// TestPushMessageToClient_ClientNotFound tests pushing to a non-existent client
func TestPushMessageToClient_ClientNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:7000", testutil.TestNumShards)
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
