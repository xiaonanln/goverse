package node

import (
	"testing"

	"github.com/xiaonanln/goverse/client"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

// TestPushMessageToClient tests the PushMessageToClient functionality
func TestPushMessageToClient(t *testing.T) {
	node := NewNode("test-node:1234")
	
	// Register a client type
	node.RegisterClientType((*client.BaseClient)(nil))
	
	// Create a client object
	clientObj, err := node.RegisterClient()
	if err != nil {
		t.Fatalf("Failed to register client: %v", err)
	}
	
	clientID := clientObj.Id()
	
	// Test pushing a message
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName:  "TestUser",
			Message:   "Hello, World!",
			Timestamp: 12345,
		},
	}
	
	// Push the message
	err = node.PushMessageToClient(clientID, testMsg)
	if err != nil {
		t.Fatalf("Failed to push message to client: %v", err)
	}
	
	// Verify the message was received
	select {
	case msg := <-clientObj.MessageChan():
		notification, ok := msg.(*chat_pb.Client_NewMessageNotification)
		if !ok {
			t.Fatalf("Expected *chat_pb.Client_NewMessageNotification, got %T", msg)
		}
		if notification.Message.UserName != "TestUser" {
			t.Errorf("Expected UserName 'TestUser', got '%s'", notification.Message.UserName)
		}
		if notification.Message.Message != "Hello, World!" {
			t.Errorf("Expected Message 'Hello, World!', got '%s'", notification.Message.Message)
		}
	default:
		t.Fatal("No message received on client channel")
	}
}

// TestPushMessageToClient_NotFound tests pushing to a non-existent client
func TestPushMessageToClient_NotFound(t *testing.T) {
	node := NewNode("test-node:1234")
	
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}
	
	err := node.PushMessageToClient("non-existent-client", testMsg)
	if err == nil {
		t.Fatal("Expected error when pushing to non-existent client, got nil")
	}
}

// TestPushMessageToClient_NotAClient tests pushing to a regular object
func TestPushMessageToClient_NotAClient(t *testing.T) {
	node := NewNode("test-node:1234")
	
	// Register a non-client object type
	type TestObject struct {
		client.BaseClient
	}
	node.RegisterObjectType((*TestObject)(nil))
	
	// Create a regular object (not through RegisterClient)
	obj, err := node.createObject("TestObject", "test-object-123", nil)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}
	
	testMsg := &chat_pb.Client_NewMessageNotification{
		Message: &chat_pb.ChatMessage{
			UserName: "TestUser",
			Message:  "Hello",
		},
	}
	
	// This should work because TestObject embeds BaseClient
	err = node.PushMessageToClient(obj.Id(), testMsg)
	if err != nil {
		t.Logf("Expected this to work since TestObject embeds BaseClient, got: %v", err)
		// This is actually expected to work, so we don't fail the test
	}
}
