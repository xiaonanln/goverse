package node

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/client"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

// TestRegisterClient_ReturnsIDAndMessageChan demonstrates the new API returns client ID and message channel
// This test validates that the refactored RegisterClient API no longer exposes the ClientObject
func TestRegisterClient_ReturnsIDAndMessageChan(t *testing.T) {
	node := NewNode("test-node:1234")
	node.RegisterClientType((*client.BaseClient)(nil))
	
	ctx := context.Background()
	
	// RegisterClient now returns the client ID and message channel
	clientID, messageChan, err := node.RegisterClient(ctx)
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}
	
	// Verify the client ID is a non-empty string
	if clientID == "" {
		t.Fatal("Client ID should not be empty")
	}
	
	// Verify the message channel is not nil
	if messageChan == nil {
		t.Fatal("Message channel should not be nil")
	}
	
	// Verify the client ID has the correct format (node address + / + unique ID)
	expectedPrefix := "test-node:1234/"
	if len(clientID) <= len(expectedPrefix) {
		t.Fatalf("Client ID %s is too short, expected format: %s<unique-id>", clientID, expectedPrefix)
	}
	
	if clientID[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("Client ID %s should start with %s", clientID, expectedPrefix)
	}
}

// TestRegisterClient_MessageChanWorks tests that the returned message channel works correctly
func TestRegisterClient_MessageChanWorks(t *testing.T) {
	node := NewNode("test-node:1234")
	node.RegisterClientType((*client.BaseClient)(nil))
	
	ctx := context.Background()
	
	// Register a client using the new API
	clientID, messageChan, err := node.RegisterClient(ctx)
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}
	
	if messageChan == nil {
		t.Fatal("Message channel should not be nil")
	}
	
	// Push a message and verify it's received through the channel
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName:  "TestUser",
			Message:   "Test Message",
			Timestamp: 12345,
		},
	}
	
	err = node.PushMessageToClient(clientID, testMsg)
	if err != nil {
		t.Fatalf("Failed to push message: %v", err)
	}
	
	// Verify message was received
	select {
	case msg := <-messageChan:
		notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
		if !ok {
			t.Fatalf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg)
		}
		if notification.Message.UserName != "TestUser" {
			t.Errorf("Expected UserName 'TestUser', got '%s'", notification.Message.UserName)
		}
	default:
		t.Fatal("No message received on client channel")
	}
}
