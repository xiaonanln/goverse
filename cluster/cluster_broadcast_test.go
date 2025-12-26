package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

// TestBroadcastToAllClients_NoGates tests that BroadcastToAllClients fails when no gates are connected
func TestBroadcastToAllClients_NoGates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNodeForTesting(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestBroadcastToAllClients_NoGates")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "Server",
			Message:  "Broadcast announcement",
		},
	}

	err := c.BroadcastToAllClients(ctx, testMsg)
	if err == nil {
		t.Fatal("BroadcastToAllClients should fail when no gates are connected")
	}
	if err.Error() != "no gates connected" {
		t.Errorf("Expected error 'no gates connected', got: %v", err)
	}
}
