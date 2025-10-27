package main

import (
	"context"
	"testing"

	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

func TestChatRoom_OnCreated(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	
	// Call OnCreated
	room.OnCreated()
	
	// Verify the room was initialized properly
	if room.users == nil {
		t.Error("users map should be initialized")
	}
	
	if room.clientIDs == nil {
		t.Error("clientIDs map should be initialized")
	}
}

func TestChatRoom_Name(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	
	name := room.Name()
	if name != "General" {
		t.Errorf("Expected room name 'General', got '%s'", name)
	}
}

func TestChatRoom_Join(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	req := &chat_pb.ChatRoom_JoinRequest{
		UserName: "Alice",
		ClientId: "client-123",
	}
	
	resp, err := room.Join(ctx, req)
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	
	// Verify response
	if resp.RoomName != "General" {
		t.Errorf("Expected room name 'General', got '%s'", resp.RoomName)
	}
	
	if len(resp.RecentMessages) == 0 {
		t.Error("Expected some welcome messages")
	}
	
	// Verify user was added
	if !room.users["Alice"] {
		t.Error("User Alice should be in the room")
	}
	
	// Verify client ID was stored
	if room.clientIDs["Alice"] != "client-123" {
		t.Errorf("Expected client ID 'client-123', got '%s'", room.clientIDs["Alice"])
	}
}

func TestChatRoom_Join_WithoutClientID(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	req := &chat_pb.ChatRoom_JoinRequest{
		UserName: "Bob",
		ClientId: "", // Empty client ID
	}
	
	resp, err := room.Join(ctx, req)
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	
	// Verify user was added even without client ID
	if !room.users["Bob"] {
		t.Error("User Bob should be in the room")
	}
	
	// Verify client ID was not stored
	if room.clientIDs["Bob"] != "" {
		t.Error("Client ID should not be stored when empty")
	}
	
	// Verify we still get welcome messages
	if len(resp.RecentMessages) == 0 {
		t.Error("Expected some welcome messages")
	}
}

func TestChatRoom_SendMessage(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	// First join a user WITHOUT client ID to avoid push message failures in unit tests
	ctx := context.Background()
	joinReq := &chat_pb.ChatRoom_JoinRequest{
		UserName: "Alice",
		ClientId: "",
	}
	_, err := room.Join(ctx, joinReq)
	if err != nil {
		t.Fatalf("Join failed: %v", err)
	}
	
	// Send a message
	sendReq := &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Hello, everyone!",
	}
	
	resp, err := room.SendMessage(ctx, sendReq)
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	
	if resp == nil {
		t.Error("Expected non-nil response")
	}
	
	// Verify message was stored
	if len(room.messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(room.messages))
	}
	
	if room.messages[0].UserName != "Alice" {
		t.Errorf("Expected message from Alice, got %s", room.messages[0].UserName)
	}
	
	if room.messages[0].Message != "Hello, everyone!" {
		t.Errorf("Expected message 'Hello, everyone!', got '%s'", room.messages[0].Message)
	}
	
	if room.messages[0].Timestamp == 0 {
		t.Error("Message timestamp should be set")
	}
}

func TestChatRoom_SendMessage_MultipleMessages(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	
	// Join two users WITHOUT client IDs to avoid push message failures in unit tests
	room.Join(ctx, &chat_pb.ChatRoom_JoinRequest{UserName: "Alice", ClientId: ""})
	room.Join(ctx, &chat_pb.ChatRoom_JoinRequest{UserName: "Bob", ClientId: ""})
	
	// Send multiple messages
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "First message",
	})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Bob",
		Message:  "Second message",
	})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Third message",
	})
	
	// Verify all messages were stored
	if len(room.messages) != 3 {
		t.Errorf("Expected 3 messages, got %d", len(room.messages))
	}
	
	// Verify message order
	if room.messages[0].Message != "First message" {
		t.Errorf("First message incorrect: %s", room.messages[0].Message)
	}
	if room.messages[1].Message != "Second message" {
		t.Errorf("Second message incorrect: %s", room.messages[1].Message)
	}
	if room.messages[2].Message != "Third message" {
		t.Errorf("Third message incorrect: %s", room.messages[2].Message)
	}
}

func TestChatRoom_GetRecentMessages(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	
	// Join a user and send some messages (no client ID to avoid push failures)
	room.Join(ctx, &chat_pb.ChatRoom_JoinRequest{UserName: "Alice", ClientId: ""})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Message 1",
	})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Message 2",
	})
	
	// Get all recent messages (after timestamp 0)
	req := &chat_pb.ChatRoom_GetRecentMessagesRequest{
		AfterTimestamp: 0,
	}
	
	resp, err := room.GetRecentMessages(ctx, req)
	if err != nil {
		t.Fatalf("GetRecentMessages failed: %v", err)
	}
	
	if len(resp.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(resp.Messages))
	}
}

func TestChatRoom_GetRecentMessages_AfterTimestamp(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	
	// Join a user and send messages (no client ID to avoid push failures)
	room.Join(ctx, &chat_pb.ChatRoom_JoinRequest{UserName: "Alice", ClientId: ""})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Message 1",
	})
	
	// Get the timestamp of the first message
	firstMsgTimestamp := room.messages[0].Timestamp
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Message 2",
	})
	
	room.SendMessage(ctx, &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: "Alice",
		Message:  "Message 3",
	})
	
	// Get messages after the first message timestamp
	req := &chat_pb.ChatRoom_GetRecentMessagesRequest{
		AfterTimestamp: firstMsgTimestamp,
	}
	
	resp, err := room.GetRecentMessages(ctx, req)
	if err != nil {
		t.Fatalf("GetRecentMessages failed: %v", err)
	}
	
	// Should only get messages 2 and 3
	if len(resp.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(resp.Messages))
	}
	
	if len(resp.Messages) >= 1 && resp.Messages[0].Message != "Message 2" {
		t.Errorf("Expected first message to be 'Message 2', got '%s'", resp.Messages[0].Message)
	}
	
	if len(resp.Messages) >= 2 && resp.Messages[1].Message != "Message 3" {
		t.Errorf("Expected second message to be 'Message 3', got '%s'", resp.Messages[1].Message)
	}
}

func TestChatRoom_GetRecentMessages_Empty(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	room.OnCreated()
	
	ctx := context.Background()
	
	// Get messages when room is empty
	req := &chat_pb.ChatRoom_GetRecentMessagesRequest{
		AfterTimestamp: 0,
	}
	
	resp, err := room.GetRecentMessages(ctx, req)
	if err != nil {
		t.Fatalf("GetRecentMessages failed: %v", err)
	}
	
	if len(resp.Messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(resp.Messages))
	}
}

func TestChatRoom_Type(t *testing.T) {
	room := &ChatRoom{}
	room.OnInit(room, "ChatRoom-General", nil)
	
	if room.Type() != "ChatRoom" {
		t.Errorf("Expected type ChatRoom, got %s", room.Type())
	}
}
