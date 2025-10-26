# Push-Based Chat Messaging

This document explains how the push-based messaging feature works in the Goverse chat sample.

## Overview

Instead of requiring clients to poll for new messages, the chat system now pushes messages in real-time to all connected clients in a chat room. This is implemented using the existing gRPC bidirectional streaming infrastructure.

## How It Works

### 1. Client Registration Stream

When a client connects to the server, it establishes a bidirectional gRPC stream via the `Register` RPC:

```go
stream, err := client.Register(ctx, &client_pb.Empty{})
```

This stream is kept open for the lifetime of the client connection. The server sends messages through this stream, and the client listens continuously.

### 2. Client Message Channel

Each client object (server-side) has a `MessageChan()` that returns a buffered channel:

```go
type ClientObject interface {
    object.Object
    MessageChan() chan proto.Message
}
```

The server's `Register` method listens to this channel and forwards any messages to the client:

```go
for msg := range client.MessageChan() {
    stream.Send(msg)
}
```

### 3. Tracking Clients in Chat Rooms

When a user joins a chat room, the `ChatRoom` object stores the mapping between the username and the client ID:

```go
type ChatRoom struct {
    users     map[string]bool       // userName -> bool
    clientIDs map[string]string     // userName -> clientID
    messages  []*chat_pb.ChatMessage
}
```

### 4. Pushing Messages

When a message is sent to a chat room, the `ChatRoom.SendMessage` method:

1. Stores the message in the room's message history
2. Iterates through all connected clients in the room
3. Pushes the message to each client's message channel (except the sender):

```go
notification := &chat_pb.Client_NewMessageNotification{
    Message: chatMsg,
}
for userName, clientID := range room.clientIDs {
    if userName == request.GetUserName() {
        continue // Don't send to sender
    }
    
    err := goverseapi.PushMessageToClient(clientID, notification)
    if err != nil {
        // Log error but don't fail - client can still poll
    }
}
```

### 5. Client-Side Message Listener

On the client side, a goroutine continuously listens for messages on the stream:

```go
func (c *ChatClient) listenForMessages(stream client_pb.ClientService_RegisterClient) {
    for {
        msgAny, err := stream.Recv()
        if err != nil {
            return
        }
        
        msg, err := msgAny.UnmarshalNew()
        if err != nil {
            continue
        }
        
        switch notification := msg.(type) {
        case *chat_pb.Client_NewMessageNotification:
            // Display the message immediately
            fmt.Printf("\n[%s] %s: %s\n", timestamp, 
                notification.Message.UserName, 
                notification.Message.Message)
        }
    }
}
```

## Benefits

1. **Real-time Updates**: Messages appear instantly without polling
2. **Lower Latency**: No delay between message send and receive
3. **Reduced Server Load**: No repeated polling requests
4. **Better User Experience**: Chat feels more responsive and interactive
5. **Graceful Degradation**: Polling still works if push fails

## Implementation Details

### Buffered Channel

The client message channel is buffered (capacity 10) to prevent blocking when pushing messages:

```go
func (cp *BaseClient) OnCreated() {
    cp.messageChan = make(chan proto.Message, 10)
}
```

If the buffer fills up (e.g., client is not reading), the `PushMessageToClient` method will return an error, but the message send operation will still succeed.

### Error Handling

Push failures are logged but don't cause message send to fail. Clients can still retrieve missed messages via the `/messages` command (polling fallback).

### Protocol Buffer Messages

The new `Client_NewMessageNotification` message type wraps chat messages for push delivery:

```protobuf
message Client_NewMessageNotification {
    ChatMessage message = 1;
}
```

## Testing

Run the integration test to see push messaging in action:

```bash
python3 tests/integration/test_chat.py
```

For manual testing with two clients:

1. Start the server: `go run samples/chat/server/*.go`
2. Open two terminals
3. Terminal 1: `go run samples/chat/client/client.go -user alice`
4. Terminal 2: `go run samples/chat/client/client.go -user bob`
5. Both: `/join General`
6. Type in one terminal and see messages appear in the other instantly!

## Future Enhancements

Possible improvements:

- Push typing indicators
- Push user join/leave notifications
- Push read receipts
- Configurable channel buffer size
- Metrics for push success/failure rates
- Automatic reconnection on stream failure
