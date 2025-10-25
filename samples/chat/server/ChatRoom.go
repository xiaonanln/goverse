package main

import (
	"context"
	"sync"
	"time"

	"github.com/simonlingoogle/pulse/pulseapi"
	chat_pb "github.com/simonlingoogle/pulse/samples/chat/proto"
)

type ChatRoom struct {
	pulseapi.BaseObject

	users    map[string]bool
	messages []*chat_pb.ChatMessage
	mu       sync.Mutex
}

func (room *ChatRoom) OnCreated() {
	room.Logger.Infof("ChatRoom %s created", room.Id())
	room.users = make(map[string]bool)
}

func (room *ChatRoom) Name() string {
	return room.Id()[9:] // Remove "Chatroom-" prefix
}

func (room *ChatRoom) Join(ctx context.Context, request *chat_pb.ChatRoom_JoinRequest) (*chat_pb.ChatRoom_JoinResponse, error) {
	room.mu.Lock()
	defer room.mu.Unlock()

	userName := request.GetUserName()
	room.Logger.Infof("User %s joined chatroom %s", userName, room.Id())

	room.users[userName] = true

	return &chat_pb.ChatRoom_JoinResponse{
		RoomName: room.Name(),
		RecentMessages: []*chat_pb.ChatMessage{
			{
				UserName:  "SYSTEM",
				Message:   "Welcome to the chatroom!",
				Timestamp: time.Now().UnixMicro(),
			},
			{
				UserName:  "SYSTEM",
				Message:   "Feel free to start chatting.",
				Timestamp: time.Now().UnixMicro(),
			},
		},
	}, nil
}

func (room *ChatRoom) SendMessage(ctx context.Context, request *chat_pb.ChatRoom_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
	room.mu.Lock()
	defer room.mu.Unlock()

	message := request.GetMessage()
	room.Logger.Infof("Message in chatroom %s: %s", room.Id(), message)

	room.messages = append(room.messages, &chat_pb.ChatMessage{
		UserName:  request.GetUserName(),
		Message:   message,
		Timestamp: time.Now().UnixMicro(),
	})

	return &chat_pb.Client_SendChatMessageResponse{}, nil
}

func (room *ChatRoom) GetRecentMessages(ctx context.Context, request *chat_pb.ChatRoom_GetRecentMessagesRequest) (*chat_pb.ChatRoom_GetRecentMessagesResponse, error) {
	room.mu.Lock()
	defer room.mu.Unlock()

	afterTimestamp := request.GetAfterTimestamp()
	var recentMessages []*chat_pb.ChatMessage
	for _, msg := range room.messages {
		if msg.Timestamp > afterTimestamp {
			recentMessages = append(recentMessages, msg)
		}
	}

	room.Logger.Infof("GetRecentMessages in chatroom %s after %d: found %d messages", room.Id(), afterTimestamp, len(recentMessages))

	return &chat_pb.ChatRoom_GetRecentMessagesResponse{
		Messages: recentMessages,
	}, nil
}
