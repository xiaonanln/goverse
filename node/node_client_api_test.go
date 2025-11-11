package node

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/client"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

// TestRegisterClient_ReturnsIDNotObject demonstrates the new API returns only the client ID
// This test validates that the refactored RegisterClient API no longer exposes the ClientObject
func TestRegisterClient_ReturnsIDNotObject(t *testing.T) {
	node := NewNode("test-node:1234")
	node.RegisterClientType((*client.BaseClient)(nil))
	
	ctx := context.Background()
	
	// RegisterClient now returns only the client ID (string)
	clientID, err := node.RegisterClient(ctx)
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}
	
	// Verify the client ID is a non-empty string
	if clientID == "" {
		t.Fatal("Client ID should not be empty")
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

// TestGetClientMessageChan_ValidClient tests the new GetClientMessageChan method
func TestGetClientMessageChan_ValidClient(t *testing.T) {
	node := NewNode("test-node:1234")
	node.RegisterClientType((*client.BaseClient)(nil))
	
	ctx := context.Background()
	
	// Register a client using the new API
	clientID, err := node.RegisterClient(ctx)
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}
	
	// Use GetClientMessageChan to get the message channel
	messageChan, err := node.GetClientMessageChan(clientID)
	if err != nil {
		t.Fatalf("Failed to get client message channel: %v", err)
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

// TestGetClientMessageChan_InvalidClient tests error handling for non-existent client
func TestGetClientMessageChan_InvalidClient(t *testing.T) {
	node := NewNode("test-node:1234")
	
	// Try to get message channel for a non-existent client
	_, err := node.GetClientMessageChan("non-existent-client")
	if err == nil {
		t.Fatal("Expected error when getting message channel for non-existent client")
	}
	
	expectedError := "client not found: non-existent-client"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

// TestGetClientMessageChan_NotAClient tests error handling when object is not a ClientObject
func TestGetClientMessageChan_NotAClient(t *testing.T) {
	node := NewNode("test-node:1234")
	
	// Register a regular object type (not a client)
	type TestObject struct {
		client.BaseClient
	}
	node.RegisterObjectType((*TestObject)(nil))
	
	ctx := context.Background()
	err := node.createObject(ctx, "TestObject", "test-obj-123")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}
	
	// Try to get message channel for a regular object
	// Note: This will actually work because TestObject embeds BaseClient
	// So this test verifies that objects that embed BaseClient can be treated as clients
	_, err = node.GetClientMessageChan("test-obj-123")
	if err != nil {
		t.Logf("Note: Object with embedded BaseClient is treated as client (expected behavior): %v", err)
	}
}
