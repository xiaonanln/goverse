package main

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
)

type ChatRoom struct {
	goverseapi.BaseObject

	users     map[string]bool   // userName -> bool
	clientIDs map[string]string // userName -> clientID for push notifications
	messages  []*chat_pb.ChatMessage
	mu        sync.Mutex
}

func (room *ChatRoom) OnCreated() {
	room.Logger.Infof("ChatRoom %s created", room.Id())
	room.users = make(map[string]bool)
	room.clientIDs = make(map[string]string)
}

func (room *ChatRoom) Name() string {
	return room.Id()[9:] // Remove "Chatroom-" prefix
}

func (room *ChatRoom) Join(ctx context.Context, request *chat_pb.ChatRoom_JoinRequest) (*chat_pb.ChatRoom_JoinResponse, error) {
	room.mu.Lock()
	defer room.mu.Unlock()

	userName := request.GetUserName()
	clientID := request.GetClientId()
	room.Logger.Infof("User %s (client %s) joined chatroom %s", userName, clientID, room.Id())

	room.users[userName] = true
	if clientID != "" {
		room.clientIDs[userName] = clientID
	}

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

	chatMsg := &chat_pb.ChatMessage{
		UserName:  request.GetUserName(),
		Message:   message,
		Timestamp: time.Now().UnixMicro(),
	}
	room.messages = append(room.messages, chatMsg)

	// Push message to all connected clients in the room
	notification := &chat_pb.Client_NewMessageNotification{
		Message: chatMsg,
	}
	for userName, clientID := range room.clientIDs {
		// Don't send to the sender (they already know about their own message)
		if userName == request.GetUserName() {
			continue
		}

		err := goverseapi.PushMessageToClient(ctx, clientID, notification)
		if err != nil {
			room.Logger.Warnf("Failed to push message to client %s: %v", clientID, err)
			// Don't fail the send if push fails - client can still poll
		} else {
			room.Logger.Infof("Pushed message to client %s", clientID)
		}
	}

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
