package main

import (
	"context"
	"testing"

	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

func TestChatRoomMgr_OnCreated(t *testing.T) {
	mgr := &ChatRoomMgr{}
	mgr.OnInit(mgr, "ChatRoomMgr0", nil)
	
	// Note: We cannot call OnCreated in a unit test because it requires
	// a running node to create ChatRoom objects. This would be tested
	// in an integration test environment.
	// This test just verifies the manager can be initialized.
	
	// Verify the manager was initialized properly
	if mgr.Id() != "ChatRoomMgr0" {
		t.Errorf("Expected ID ChatRoomMgr0, got %s", mgr.Id())
	}
}

func TestChatRoomMgr_ListChatRooms(t *testing.T) {
	mgr := &ChatRoomMgr{}
	mgr.OnInit(mgr, "ChatRoomMgr0", nil)
	
	ctx := context.Background()
	req := &chat_pb.ChatRoom_ListRequest{}
	
	resp, err := mgr.ListChatRooms(ctx, req)
	if err != nil {
		t.Fatalf("ListChatRooms failed: %v", err)
	}
	
	// Verify we get the expected chat rooms
	expectedRooms := []string{"General", "Technology", "Crypto", "Sports", "Movies"}
	if len(resp.ChatRooms) != len(expectedRooms) {
		t.Errorf("Expected %d chat rooms, got %d", len(expectedRooms), len(resp.ChatRooms))
	}
	
	// Verify each expected room is in the response
	roomMap := make(map[string]bool)
	for _, room := range resp.ChatRooms {
		roomMap[room] = true
	}
	
	for _, expectedRoom := range expectedRooms {
		if !roomMap[expectedRoom] {
			t.Errorf("Expected room %s not found in response", expectedRoom)
		}
	}
}

func TestChatRoomMgr_Type(t *testing.T) {
	mgr := &ChatRoomMgr{}
	mgr.OnInit(mgr, "ChatRoomMgr0", nil)
	
	if mgr.Type() != "ChatRoomMgr" {
		t.Errorf("Expected type ChatRoomMgr, got %s", mgr.Type())
	}
}
