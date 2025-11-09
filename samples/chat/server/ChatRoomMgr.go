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
}

func (mgr *ChatRoomMgr) OnCreated() {
	mgr.Logger.Infof("ChatRoomMgr %s created", mgr.Id())
	
	// Create chat rooms immediately when ChatRoomMgr is created
	// This happens after the cluster is ready (shard mapping is available)
	mgr.Logger.Infof("Creating chat rooms...")
	for _, roomName := range chatRooms {
		roomID, err := goverseapi.CreateObject(context.Background(), "ChatRoom", "ChatRoom-"+roomName)
		if err != nil {
			mgr.Logger.Errorf("Failed to create chat room %s: %v", roomName, err)
		} else {
			mgr.Logger.Infof("Chat room %s created: %s", roomName, roomID)
		}
	}
}

func (mgr *ChatRoomMgr) ListChatRooms(ctx context.Context, request *chat_pb.ChatRoom_ListRequest) (*chat_pb.ChatRoom_ListResponse, error) {
	return &chat_pb.ChatRoom_ListResponse{
		ChatRooms: chatRooms,
	}, nil
}
