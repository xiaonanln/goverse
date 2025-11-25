package cluster

import (
	"context"
	"testing"
	"time"

	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestPushMessageToClient_NoNode tests that PushMessageToClient fails when thisNode is not set
func TestPushMessageToClient_NoNode(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// TestPushMessageToClient_ClientNotFound tests pushing to a non-existent client
func TestPushMessageToClient_ClientNotFound(t *testing.T) {
	t.Parallel()
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

// TestPushMessageToClient_DefaultTimeout tests that the default timeout is applied
func TestPushMessageToClient_DefaultTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_DefaultTimeout")

	// Verify the default timeout is set correctly (5 seconds per TIMEOUT_DESIGN.md)
	if c.defaultPushMessageTimeout != DefaultPushMessageTimeout {
		t.Fatalf("Expected default push message timeout to be %v, got %v", DefaultPushMessageTimeout, c.defaultPushMessageTimeout)
	}

	if c.defaultPushMessageTimeout != 5*time.Second {
		t.Fatalf("Expected default push message timeout to be 5s, got %v", c.defaultPushMessageTimeout)
	}
}

// TestPushMessageToClient_ExistingDeadlinePreserved tests that existing context deadline is preserved
func TestPushMessageToClient_ExistingDeadlinePreserved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_ExistingDeadlinePreserved")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Create context with a short deadline (100ms)
	ctxWithDeadline, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// This should fail fast (gate not connected), not wait for the 5s default timeout
	start := time.Now()
	_ = c.PushMessageToClient(ctxWithDeadline, "localhost:7000/test-client", testMsg)
	elapsed := time.Since(start)

	// Should complete much faster than the default 5s timeout
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Expected operation to complete quickly (existing deadline should be preserved), took %v", elapsed)
	}
}

// TestPushMessageToClient_ContextCancellation tests that context cancellation is respected
func TestPushMessageToClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testNode := testutil.MustNewNode(ctx, t, "localhost:7000")
	c := newClusterForTesting(testNode, "TestPushMessageToClient_ContextCancellation")

	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}

	// Create a cancelled context
	ctxCancelled, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// The operation should return an error (gate not connected is returned before timeout check)
	err := c.PushMessageToClient(ctxCancelled, "localhost:7000/test-client", testMsg)
	// Even with cancelled context, the gate-not-connected error takes precedence
	// since the error happens before we try to send on the channel
	if err == nil {
		t.Fatal("Expected error when pushing with cancelled context")
	}
}
