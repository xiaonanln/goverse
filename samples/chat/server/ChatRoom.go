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

	// Push message to all connected clients in the room (including sender)
	notification := &chat_pb.Client_NewMessageNotification{
		Message: chatMsg,
	}
	
	// Collect all client IDs
	clientIDs := make([]string, 0, len(room.clientIDs))
	for _, clientID := range room.clientIDs {
		clientIDs = append(clientIDs, clientID)
	}
	
	// Log clientIDs: how many and the list
	room.Logger.Infof("Active clientIDs in room %s: count=%d ids=%v", room.Id(), len(clientIDs), clientIDs)
	
	// Single API call with automatic fan-out optimization
	if len(clientIDs) > 0 {
		err := goverseapi.PushMessageToClient(ctx, clientIDs, notification)
		if err != nil {
			room.Logger.Warnf("Failed to push to clients: %v", err)
		} else {
			room.Logger.Infof("Pushed message to %d client(s)", len(clientIDs))
		}
	}

	return &chat_pb.Client_SendChatMessageResponse{}, nil
}
