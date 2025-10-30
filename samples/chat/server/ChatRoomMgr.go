package main

import (
	"context"

	"github.com/xiaonanln/goverse/goverseapi"

	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

var (
	chatRooms = []string{"General", "Technology", "Crypto", "Sports", "Movies"}
)

type ChatRoomMgr struct {
	goverseapi.BaseObject
	roomsInitialized bool
}

func (mgr *ChatRoomMgr) OnCreated() {
	mgr.Logger.Infof("ChatRoomMgr %s created", mgr.Id())
}

// ensureRoomsCreated lazily creates chat rooms on first access
// This ensures the cluster infrastructure (shard mapping) is ready before creating rooms
func (mgr *ChatRoomMgr) ensureRoomsCreated() {
	if mgr.roomsInitialized {
		return
	}

	mgr.Logger.Infof("Initializing chat rooms...")
	for _, roomName := range chatRooms {
		roomID, err := goverseapi.CreateObject(context.Background(), "ChatRoom", "ChatRoom-"+roomName, nil)
		if err != nil {
			mgr.Logger.Errorf("Failed to create chat room %s: %v", roomName, err)
		} else {
			mgr.Logger.Infof("Chat room %s created: %s", roomName, roomID)
		}
	}
	mgr.roomsInitialized = true
}

func (mgr *ChatRoomMgr) ListChatRooms(ctx context.Context, request *chat_pb.ChatRoom_ListRequest) (*chat_pb.ChatRoom_ListResponse, error) {
	mgr.ensureRoomsCreated()
	return &chat_pb.ChatRoom_ListResponse{
		ChatRooms: chatRooms,
	}, nil
}
