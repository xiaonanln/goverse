# Push-Based Chat Messaging

This document explains how the push-based messaging feature works in the Goverse chat sample using the gate architecture.

## Overview

Instead of requiring clients to poll for new messages, the chat system pushes messages in real-time to all connected clients in a chat room. This is implemented using the gate architecture with bidirectional gRPC streaming.

## How It Works

### 1. Gate Registration with Nodes

When a gate starts, it connects to each node in the cluster via the `RegisterGate` streaming RPC:

```go
stream, err := nodeClient.RegisterGate(ctx, &goverse_pb.RegisterGateRequest{
    GateAddr: "localhost:49000",
})
```

This stream is kept open for the lifetime of the gate connection. Nodes use this stream to push messages to the gate for delivery to clients.

### 2. Client Registration with Gate

When a client connects to the gate, it establishes a bidirectional gRPC stream via the `Register` RPC:

```go
stream, err := gateClient.Register(ctx, &gate_pb.Empty{})
regResp, _ := stream.Recv()
clientID := regResp.(*gate_pb.RegisterResponse).ClientId  // Format: "gateAddr/uniqueId"
```

This stream is kept open for the lifetime of the client connection. The gate sends messages through this stream, and the client listens continuously.

### 3. Tracking Clients in Chat Rooms

When a user joins a chat room, the `ChatRoom` object stores the mapping between the username and the client ID:

```go
type ChatRoom struct {
    users     map[string]bool       // userName -> bool
    clientIDs map[string]string     // userName -> clientID (format: "gateAddr/uniqueId")
    messages  []*chat_pb.ChatMessage
}
```

### 4. Pushing Messages from Nodes to Gates

When a message is sent to a chat room, the `ChatRoom.SendMessage` method:

1. Stores the message in the room's message history
2. Iterates through all connected clients in the room
3. Pushes the message to each client via `PushMessageToClient` (except the sender):

```go
notification := &chat_pb.Client_NewMessageNotification{
    Message: chatMsg,
}
for userName, clientID := range room.clientIDs {
    if userName == request.GetUserName() {
        continue // Don't send to sender
    }
    
    err := goverseapi.PushMessageToClient(ctx, clientID, notification)
    if err != nil {
        // Log error but don't fail - client can still poll
    }
}
```

The `PushMessageToClient` function:
- Parses the `clientID` to extract the gate address
- Looks up the gate's registered stream channel
- Sends a `ClientMessageEnvelope` wrapped in a `GateMessage` to the gate
- The gate receives the message and routes it to the appropriate client

### 5. Client-Side Message Listener

On the client side, a goroutine continuously listens for messages on the stream:

```go
func (c *ChatClient) listenForMessages(stream gate_pb.GateService_RegisterClient) {
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
            timestamp := time.Unix(notification.Message.Timestamp, 0).Format("15:04:05")
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

### Buffered Channels for Push Messaging

The push messaging system uses two levels of buffered channels:

1. **Gate Message Channel (Node → Gate)**: Each gate has a buffered message channel (capacity 1024) on each node:
   - Created when the gate registers with the node via `RegisterGate`
   - Messages are sent to the channel without blocking (non-blocking send)
   - If the buffer fills up, `PushMessageToClient` returns an error

2. **Client Message Channel (Gate → Client)**: Each client proxy has a buffered message channel (capacity 10):
   - Created when the client registers with the gate
   - Messages are forwarded from the gate's channel to individual client channels
   - If the client's buffer fills up, messages may be dropped

### Client ID Format

Client IDs use the format `gateAddress/uniqueId` (e.g., `localhost:49000/AAZEDvtPr4JHP6WtybiD`):

- The gate address allows nodes to route messages to the correct gate
- The unique ID allows gates to route messages to the correct client
- No centralized client registry is needed

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
python3 tests/samples/chat/test_chat.py
```

For manual testing with two clients:

1. Start the node server: `go run samples/chat/server/*.go`
2. Start the gate: `go run cmd/gate/main.go`
3. Open two terminals
4. Terminal 1: `go run samples/chat/client/*.go -server localhost:49000 -user alice`
5. Terminal 2: `go run samples/chat/client/*.go -server localhost:49000 -user bob`
6. Both: `/join General`
7. Type in one terminal and see messages appear in the other instantly!

## Future Enhancements

Possible improvements:

- Push typing indicators
- Push user join/leave notifications
- Push read receipts
- Configurable channel buffer size
- Metrics for push success/failure rates
- Automatic reconnection on stream failure
- **Fan-out Optimization**: Reduce network traffic by sending a single message per gate for multiple clients (see [Client Push Optimization Design](design/CLIENT_PUSH_OPTIMIZATION.md))
