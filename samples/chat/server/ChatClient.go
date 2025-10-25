package main

import (
	"context"
	"fmt"

	"github.com/simonlingoogle/pulse/pulseapi"
	chat_pb "github.com/simonlingoogle/pulse/samples/chat/proto"
)

type ChatClient struct {
	pulseapi.BaseClient
	currentChatRoom string
}

func (cc *ChatClient) OnCreated() {
	cc.BaseClient.OnCreated()
}

func (cc *ChatClient) ListChatRooms(ctx context.Context, request *chat_pb.Client_ListChatRoomsRequest) (*chat_pb.Client_ListChatRoomsResponse, error) {
	resp, err := pulseapi.CallObject(ctx, "ChatRoomMgr0", "ListChatRooms", &chat_pb.ChatRoom_ListRequest{})
	if err != nil {
		return nil, err
	}
	return &chat_pb.Client_ListChatRoomsResponse{
		ChatRooms: resp.(*chat_pb.ChatRoom_ListResponse).ChatRooms,
	}, nil
}

func (cc *ChatClient) Join(ctx context.Context, request *chat_pb.Client_JoinChatRoomRequest) (*chat_pb.Client_JoinChatRoomResponse, error) {
	cc.Logger.Infof("Joining chat room %s as user %s", request.RoomName, request.UserName)
	resp, err := pulseapi.CallObject(ctx, "ChatRoom-"+request.RoomName, "Join", &chat_pb.ChatRoom_JoinRequest{
		UserName: request.UserName,
	})
	if err != nil {
		return nil, err
	}
	cc.currentChatRoom = request.RoomName
	joinResp := resp.(*chat_pb.ChatRoom_JoinResponse)
	return &chat_pb.Client_JoinChatRoomResponse{
		RoomName:       joinResp.RoomName,
		RecentMessages: joinResp.RecentMessages,
	}, nil
}

func (cc *ChatClient) SendMessage(ctx context.Context, request *chat_pb.Client_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
	cc.Logger.Infof("Sending message: %s", request.Message)
	if cc.currentChatRoom == "" {
		return nil, fmt.Errorf("You must join a chatroom first with /join <room>")
	}

	_, err := pulseapi.CallObject(ctx, "ChatRoom-"+cc.currentChatRoom, "SendMessage", &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: request.GetUserName(),
		Message:  request.GetMessage(),
	})
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (cc *ChatClient) GetRecentMessages(ctx context.Context, request *chat_pb.Client_GetRecentMessagesRequest) (*chat_pb.Client_GetRecentMessagesResponse, error) {
	if cc.currentChatRoom == "" {
		return nil, fmt.Errorf("You must join a chatroom first with /join <room>")
	}

	resp, err := pulseapi.CallObject(ctx, "ChatRoom-"+cc.currentChatRoom, "GetRecentMessages", &chat_pb.ChatRoom_GetRecentMessagesRequest{})
	if err != nil {
		return nil, err
	}
	recentResp := resp.(*chat_pb.ChatRoom_GetRecentMessagesResponse)
	return &chat_pb.Client_GetRecentMessagesResponse{
		Messages: recentResp.Messages,
	}, nil
}
