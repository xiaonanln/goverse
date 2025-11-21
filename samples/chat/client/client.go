package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	chat_pb "github.com/xiaonanln/goverse/samples/chat/proto"
	"github.com/xiaonanln/goverse/util/logger"
)

type ChatClient struct {
	conn             *grpc.ClientConn
	client           gate_pb.GateServiceClient
	roomName         string
	userName         string
	logger           *logger.Logger
	lastMsgTimestamp int64
	clientID         string
}

func NewChatClient(serverAddr, userID string) (*ChatClient, error) {
	conn, err := grpc.Dial(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}

	client := gate_pb.NewGateServiceClient(conn)

	return &ChatClient{
		conn:     conn,
		client:   client,
		userName: userID,
		logger:   logger.NewLogger("ChatClient"),
	}, nil
}

func (c *ChatClient) Close() error {
	return c.conn.Close()
}

func (c *ChatClient) CallObject(objectType, objectID, method string, arg proto.Message) (proto.Message, error) {
	if c.clientID == "" {
		return nil, fmt.Errorf("client is not registered")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	anyReq, err := anypb.New(arg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	req := &gate_pb.CallObjectRequest{
		ClientId: c.clientID,
		Type:     objectType,
		Id:       objectID,
		Method:   method,
		Request:  anyReq,
	}
	resp, err := c.client.CallObject(ctx, req)
	c.logger.Infof("Calling %s.%s.%s => %s, error=%v", objectType, objectID, method, resp.String(), err)
	if err != nil {
		return nil, fmt.Errorf("CallObject failed: %w", err)
	}
	ret, err := resp.GetResponse().UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}
	return ret, nil
}

func (c *ChatClient) ListChatrooms() error {
	// Call ChatRoomMgr directly
	respMsg, err := c.CallObject("ChatRoomMgr", "ChatRoomMgr0", "ListChatRooms", &chat_pb.ChatRoom_ListRequest{})
	if err != nil {
		return fmt.Errorf("Failed to list chat rooms: %w", err)
	}

	resp := respMsg.(*chat_pb.ChatRoom_ListResponse)

	fmt.Println("Available Chatrooms:")
	for _, room := range resp.ChatRooms {
		fmt.Printf(" - %s\n", room)
	}
	return nil
}

func (c *ChatClient) JoinChatroom(roomName string) error {
	// Call ChatRoom object directly
	joinRequest := &chat_pb.ChatRoom_JoinRequest{
		UserName: c.userName,
		ClientId: c.clientID,
	}
	respMsg, err := c.CallObject("ChatRoom", "ChatRoom-"+roomName, "Join", joinRequest)
	if err != nil {
		return fmt.Errorf("failed to join chatroom %s: %w", roomName, err)
	}

	resp := respMsg.(*chat_pb.ChatRoom_JoinResponse)
	c.logger.Infof("Joined chatroom %s", resp.RoomName)
	c.roomName = roomName
	c.lastMsgTimestamp = 0
	for _, msg := range resp.GetRecentMessages() {
		c.logger.Infof("Recent message: [%s] %s", msg.GetUserName(), msg.GetMessage())
		if msg.Timestamp > c.lastMsgTimestamp {
			c.lastMsgTimestamp = msg.Timestamp
		}
	}
	return nil
}

func (c *ChatClient) SendMessage(message string) error {
	if c.roomName == "" {
		return fmt.Errorf("You must join a chatroom first with /join <room>")
	}

	// Call ChatRoom object directly
	req := &chat_pb.ChatRoom_SendChatMessageRequest{
		UserName: c.userName,
		Message:  message,
	}
	_, err := c.CallObject("ChatRoom", "ChatRoom-"+c.roomName, "SendMessage", req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	c.logger.Infof("Message sent.")
	return nil
}

func (c *ChatClient) GetRecentMessages() error {
	if c.roomName == "" {
		return fmt.Errorf("You must join a chatroom first with /join <room>")
	}

	// Call ChatRoom object directly
	req := &chat_pb.ChatRoom_GetRecentMessagesRequest{
		AfterTimestamp: c.lastMsgTimestamp, // Use stored timestamp
	}
	c.logger.Infof("Getting messages after timestamp: %d", c.lastMsgTimestamp)
	respMsg, err := c.CallObject("ChatRoom", "ChatRoom-"+c.roomName, "GetRecentMessages", req)
	if err != nil {
		return fmt.Errorf("failed to get recent messages: %w", err)
	}

	resp := respMsg.(*chat_pb.ChatRoom_GetRecentMessagesResponse)
	fmt.Printf("Recent messages in [%s]:\n", c.roomName)
	for _, msg := range resp.Messages {
		timestamp := time.Unix(msg.Timestamp, 0).Format("15:04:05")
		fmt.Printf("[%s] %s: %s\n", timestamp, msg.UserName, msg.Message)
		// Update lastMsgTimestamp if this message is newer
		if msg.Timestamp > c.lastMsgTimestamp {
			c.lastMsgTimestamp = msg.Timestamp
		}
	}
	return nil
}

// listenForMessages continuously listens for pushed messages from the server
func (c *ChatClient) listenForMessages(stream gate_pb.GateService_RegisterClient) {
	for {
		msgAny, err := stream.Recv()
		if err != nil {
			c.logger.Errorf("Error receiving message from stream: %v", err)
			return
		}

		msg, err := msgAny.UnmarshalNew()
		if err != nil {
			c.logger.Errorf("Failed to unmarshal pushed message: %v", err)
			continue
		}

		// Handle different types of pushed messages
		switch notification := msg.(type) {
		case *chat_pb.Client_NewMessageNotification:
			// Display the pushed message
			chatMsg := notification.Message
			timestamp := time.Unix(chatMsg.Timestamp, 0).Format("15:04:05")
			fmt.Printf("\n[%s] %s: %s\n", timestamp, chatMsg.UserName, chatMsg.Message)

			// Update last message timestamp
			if chatMsg.Timestamp > c.lastMsgTimestamp {
				c.lastMsgTimestamp = chatMsg.Timestamp
			}

			// Re-display prompt
			if c.roomName != "" {
				fmt.Printf("[%s] > ", c.roomName)
			} else {
				fmt.Print("> ")
			}
		default:
			c.logger.Warnf("Received unknown message type: %T", msg)
		}
	}
}

func (c *ChatClient) RunInteractive() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Chat Client - Commands:")
	fmt.Println("  /list         - List all chatrooms")
	fmt.Println("  /join <room>  - Join a chatroom")
	fmt.Println("  /quit         - Quit the client")
	fmt.Println("  <message>     - Send a message to current room")
	fmt.Println()

	for {
		prompt := "> "
		if c.roomName != "" {
			prompt = fmt.Sprintf("[%s] > ", c.roomName)
		}
		fmt.Print(prompt)

		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			c.handleCommand(line)
		} else {
			// Send as chat message
			if err := c.SendMessage(line); err != nil {
				fmt.Printf("Error sending message: %v\n", err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading input: %v", err)
	}
}

func (c *ChatClient) handleCommand(command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "/list":
		if err := c.ListChatrooms(); err != nil {
			fmt.Printf("Error listing chatrooms: %v\n", err)
		}

	case "/join":
		if len(parts) < 2 {
			fmt.Println("Usage: /join <roomname>")
			return
		}
		roomName := parts[1]
		if err := c.JoinChatroom(roomName); err != nil {
			fmt.Printf("Error joining chatroom: %v\n", err)
		}

	case "/messages":
		if err := c.GetRecentMessages(); err != nil {
			fmt.Printf("Error getting recent messages: %v\n", err)
		}

	case "/quit":
		fmt.Println("Goodbye!")
		os.Exit(0)

	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /list         - List all chatrooms")
		fmt.Println("  /join <room>  - Join a chatroom")
		fmt.Println("  /messages     - Show recent messages in current room")
		fmt.Println("  /quit         - Quit the client")
		fmt.Println("  /help         - Show this help")
		fmt.Println("  <message>     - Send a message to current room")

	default:
		fmt.Printf("Unknown command: %s. Type /help for available commands.\n", parts[0])
	}
}

func main() {
	var (
		serverAddr = flag.String("server", "localhost:48000", "Server address")
		userID     = flag.String("user", "user1", "User ID")
	)
	flag.Parse()

	fmt.Printf("Connecting to chat server at %s as user %s...\n", *serverAddr, *userID)

	client, err := NewChatClient(*serverAddr, *userID)
	if err != nil {
		log.Fatalf("Failed to create chat client: %v", err)
	}
	client.logger.Infof("Chat client created")
	defer client.Close()
	ctx := context.Background()

	stream, err := client.client.Register(ctx, &gate_pb.Empty{}) // Ensure registration
	if err != nil {
		log.Fatalf("Failed to register client: %v", err)
	}
	defer stream.CloseSend()

	// Read initial registration response
	regResp := receiveMessages(stream).(*gate_pb.RegisterResponse)
	client.clientID = regResp.ClientId
	client.logger.Infof("Client %s registered successfully", client.clientID)

	// Start a goroutine to listen for pushed messages
	go client.listenForMessages(stream)

	fmt.Println("Connected! Type /help for commands.")
	client.RunInteractive()
}

func receiveMessages(stream gate_pb.GateService_RegisterClient) proto.Message {
	msgAny, err := stream.Recv()
	if err != nil {
		log.Fatalf("Error receiving message: %v", err)
	}

	fmt.Printf("Received message: %v\n", msgAny)
	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		log.Fatalf("Failed to unmarshal message: %v", err)
	}

	return msg
}
