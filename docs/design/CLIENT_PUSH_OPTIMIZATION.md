# Design Doc: Client Push Message Fan-out Optimization

## Problem Statement
Currently, when an object needs to push a message to multiple clients (e.g., in a chat room), it calls `PushMessageToClient` for each client individually. If many clients are connected to the same gate, the node sends multiple identical messages to that gate, one for each client. This leads to:
1.  Increased network traffic between nodes and gates.
2.  Increased CPU usage on nodes for serializing and sending multiple messages.
3.  Increased CPU usage on gates for receiving and processing multiple messages.

## Proposed Solution
Optimize the push message mechanism to send a single message per gate for multiple clients. The gate will then fan out the message to all target clients connected to it.

### 1. Protocol Changes
Update `proto/goverse.proto` to support multi-client push messages.

```proto
// ClientMessageFanOutEnvelope wraps a message for multiple clients on the same gate
message ClientMessageFanOutEnvelope {
  repeated string client_ids = 1;
  google.protobuf.Any message = 2;
}

message GateMessage {
  oneof message {
    RegisterGateResponse register_gate_response = 1;
    ClientMessageEnvelope client_message = 2;
    ClientMessageFanOutEnvelope client_fanout_message = 3; // New field
  }
}
```

### 2. API Changes
Add new APIs to `goverseapi` and `cluster` to support pushing to multiple clients.

#### `goverseapi`
```go
// PushMessageToClients sends a message to multiple clients
func PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error
```

#### `cluster.Cluster`
```go
// PushMessageToClients sends a message to multiple clients by their IDs
func (c *Cluster) PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error
```

### 3. Implementation Details

#### Node Side (`cluster.Cluster.PushMessageToClients`)
1.  Group `clientIDs` by their gate address (extracted from the ID prefix).
2.  For each gate:
    *   If there is only one client for this gate, use the existing `ClientMessageEnvelope`.
    *   If there are multiple clients for this gate, use the new `ClientMessageFanOutEnvelope`.
    *   Send the envelope to the gate's stream.

#### Gate Side (`gate.Gate.handleGateMessage`)
1.  Update the gate's message listener to handle `ClientMessageFanOutEnvelope`.
2.  For each `client_id` in the envelope:
    *   Look up the local client proxy using `g.GetClient(clientID)`.
    *   If found, push the message to the client's channel using `client.PushMessageAny(envelope.Message)`.

### 4. Backward Compatibility
The existing `PushMessageToClient` API and `ClientMessageEnvelope` will be preserved to maintain backward compatibility. `PushMessageToClients` will be the preferred way for multi-client pushes.

### 5. Example Usage (Chat Room)
```go
func (room *ChatRoom) SendMessage(ctx context.Context, msg *chat_pb.ChatMessage) {
    // ...
    var clientIDs []string
    for _, clientID := range room.clientIDs {
        if userName == request.GetUserName() {
            continue // Don't send to sender
        }
        clientIDs = append(clientIDs, clientID)
    }
    goverseapi.PushMessageToClients(ctx, clientIDs, notification)
}
```

## Benefits
- **Reduced Network Traffic**: Only one message per gate instead of one per client.
- **Lower CPU Usage**: Fewer serialization operations on nodes and fewer deserialization/routing operations on gates.
- **Improved Scalability**: Better performance when handling large numbers of clients in the same room or group.
