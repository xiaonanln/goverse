# Client Push Message Fan-out Optimization

> **Status**: Implemented  
> This document describes the client push messaging optimization to reduce network traffic and CPU usage when pushing to multiple clients, including broadcast functionality.

---

## 1. Problem Statement

Currently, when an object needs to push a message to multiple clients (e.g., in a chat room), it calls `PushMessageToClient` for each client individually. If many clients are connected to the same gate, the node sends multiple identical messages to that gate, one for each client.

**Current flow for N clients on the same gate:**
```
Object → N × PushMessageToClient → N × serialize → N × network messages → Gate
```

This leads to:
1. **Increased network traffic** between nodes and gates (N messages instead of 1)
2. **Increased CPU usage on nodes** for serializing and sending N identical messages
3. **Increased CPU usage on gates** for receiving and deserializing N identical messages
4. **Poor scalability** for large-scale scenarios (e.g., 1000-user chat rooms)

**Example scenario:**
- Chat room with 1000 users, all connected to the same gate
- User sends a message
- Current implementation: 1000 × serialize + 1000 × network send
- Result: Massive overhead for high-traffic scenarios

---

## 2. Proposed Solution

Optimize the push message mechanism to send a single message per gate when pushing to multiple clients. The gate will then fan out the message to all target clients connected to it.

**Optimized flow for N clients on the same gate:**
```
Object → PushMessageToClient([...]) → 1 × serialize → 1 × network message → Gate → N × fan-out
```

**Key benefits:**
- Network traffic: **O(gates)** instead of **O(clients)**
- Serialization: **O(gates)** instead of **O(clients)**
- Dramatically better performance when many clients are on the same gate

**Example scenario (optimized):**
- Same 1000 users on same gate
- User sends a message
- Optimized implementation: 1 × serialize + 1 × network send
- Gate fans out locally to 1000 clients
- Result: **~1000x reduction** in network and serialization overhead

---

## 3. Protocol Changes

Refactor `proto/goverse.proto` to support both single and multi-client push messages using a unified approach.

### 3.1 Refactor ClientMessageEnvelope

Update the existing `ClientMessageEnvelope` to support multiple clients:

```proto
// ClientMessageEnvelope wraps a message for one or more clients
message ClientMessageEnvelope {
  repeated string client_ids = 1; // List of client IDs to receive the message (can be single or multiple)
  google.protobuf.Any message = 2; // The message payload (same for all clients)
}
```

**Key changes:**
- Changed `client_id` (singular) to `client_ids` (repeated)
- Supports both single-client (`client_ids` with one element) and multi-client scenarios
- Unified message type eliminates complexity

### 3.2 BroadcastMessageEnvelope

Added a dedicated message type for broadcast operations:

```proto
// BroadcastMessageEnvelope wraps a message to be broadcast to all connected clients
message BroadcastMessageEnvelope {
  google.protobuf.Any message = 1; // The message payload to broadcast to all clients
}
```

**Key features:**
- Dedicated message type for broadcast operations
- Clear semantic distinction from targeted client messages
- Type-safe protocol design

### 3.3 GateMessage

Updated `GateMessage` to include the new broadcast message:

```proto
message GateMessage {
  oneof message {
    RegisterGateResponse register_gate_response = 1;
    ClientMessageEnvelope client_message = 2;  // Supports both single and multiple clients
    BroadcastMessageEnvelope broadcast_message = 3;  // Broadcast to all clients
  }
}
```

**Note**: The separate message type provides better type safety and makes the protocol intention explicit.

---

## 4. API Changes

Refactor the existing API to support pushing to multiple clients efficiently.

### 4.1 goverseapi

Refactor the existing `PushMessageToClient` to accept multiple clients:

```go
// PushMessageToClient sends a message to one or more clients.
// This is efficient whether pushing to a single client or multiple clients
// on the same gate due to automatic fan-out optimization.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - clientIDs: List of client IDs (format: "gateAddress/uniqueId"). Can be a single client or multiple clients.
//   - message: The protobuf message to push to all clients
//
// Returns:
//   - error: Non-nil if any push operation fails
//
// Example (single client):
//   err := goverseapi.PushMessageToClient(ctx, []string{"localhost:7001/user1"}, notification)
//
// Example (multiple clients):
//   clientIDs := []string{"localhost:7001/user1", "localhost:7001/user2"}
//   err := goverseapi.PushMessageToClient(ctx, clientIDs, notification)
func PushMessageToClient(ctx context.Context, clientIDs []string, message proto.Message) error
```

**Breaking change**: The signature changes from `(ctx, clientID string, message)` to `(ctx, clientIDs []string, message)`.

### 4.2 cluster.Cluster

Refactor the cluster method similarly:

```go
// PushMessageToClient sends a message to one or more clients by their IDs.
// Automatically groups clients by gate address and uses fan-out optimization.
func (c *Cluster) PushMessageToClient(ctx context.Context, clientIDs []string, message proto.Message) error
```

---

## 5. Implementation Details

### 5.1 Node Side (`cluster.Cluster.PushMessageToClient`)

**Algorithm:**

1. **Group clients by gate address:**
   - Parse each `clientID` to extract gate address (format: `gateAddress/uniqueId`)
   - Group client IDs by their gate address using a map

2. **For each gate, create envelope:**
   - Use `ClientMessageEnvelope` with the list of client IDs for that gate
   - Serialize message to `google.protobuf.Any` once per gate

3. **Send to gate stream:**
   - Send the envelope via gate's registered stream channel

**Pseudocode:**

```go
func (c *Cluster) PushMessageToClient(ctx context.Context, clientIDs []string, message proto.Message) error {
    // Group clients by gate address
    clientsByGate := make(map[string][]string)
    for _, clientID := range clientIDs {
        gateAddr, err := extractGateAddress(clientID)
        if err != nil {
            return err
        }
        clientsByGate[gateAddr] = append(clientsByGate[gateAddr], clientID)
    }
    
    // Serialize message once
    anyMsg, err := protohelper.MsgToAny(message)
    if err != nil {
        return err
    }
    
    // Send envelope to each gate
    for gateAddr, clients := range clientsByGate {
        envelope := &goverse_pb.ClientMessageEnvelope{
            ClientIds: clients,  // Can be single or multiple clients
            Message:   anyMsg,
        }
        
        // Send to gate channel
        if err := c.sendToGate(gateAddr, envelope); err != nil {
            return err
        }
    }
    
    return nil
}
```

### 5.2 Gate Side (`gate.Gate.handleGateMessage`)

**Algorithm:**

1. **Handle `ClientMessageEnvelope`:**
   - Iterate over `client_ids` in the envelope (can be single or multiple)
   - For each client ID:
     - Look up client proxy: `g.GetClient(clientID)`
     - If found, push message: `client.PushMessageAny(envelope.Message)`
     - If not found, log warning (client may have disconnected)

**Pseudocode:**

```go
func (g *Gate) handleGateMessage(msg *goverse_pb.GateMessage) {
    switch m := msg.Message.(type) {
    case *goverse_pb.GateMessage_ClientMessage:
        // Handle both single and multiple clients
        for _, clientID := range m.ClientMessage.ClientIds {
            client := g.GetClient(clientID)
            if client != nil {
                client.PushMessageAny(m.ClientMessage.Message)
            } else {
                g.logger.Warnf("Client %s not found, may have disconnected", clientID)
            }
        }
    }
}
```

---

## 6. Example Usage

### 6.1 Chat Room (Before)

Current implementation calling `PushMessageToClient` individually for each client:

```go
func (room *ChatRoom) SendMessage(ctx context.Context, request *chat_pb.ChatRoom_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    chatMsg := &chat_pb.ChatMessage{
        UserName:  request.GetUserName(),
        Message:   request.GetMessage(),
        Timestamp: time.Now().UnixMicro(),
    }
    room.messages = append(room.messages, chatMsg)
    
    notification := &chat_pb.Client_NewMessageNotification{
        Message: chatMsg,
    }
    
    // Current: Call individually for each client (inefficient)
    for _, clientID := range room.clientIDs {
        err := goverseapi.PushMessageToClient(ctx, clientID, notification)
        if err != nil {
            room.Logger.Warnf("Failed to push to client %s: %v", clientID, err)
        }
    }
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### 6.2 Chat Room (After)

Refactored implementation passing all client IDs at once:

```go
func (room *ChatRoom) SendMessage(ctx context.Context, request *chat_pb.ChatRoom_SendChatMessageRequest) (*chat_pb.Client_SendChatMessageResponse, error) {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    chatMsg := &chat_pb.ChatMessage{
        UserName:  request.GetUserName(),
        Message:   request.GetMessage(),
        Timestamp: time.Now().UnixMicro(),
    }
    room.messages = append(room.messages, chatMsg)
    
    notification := &chat_pb.Client_NewMessageNotification{
        Message: chatMsg,
    }
    
    // Refactored: Collect all client IDs and send in one call
    clientIDs := make([]string, 0, len(room.clientIDs))
    for _, clientID := range room.clientIDs {
        clientIDs = append(clientIDs, clientID)
    }
    
    // Single API call with automatic fan-out optimization
    err := goverseapi.PushMessageToClient(ctx, clientIDs, notification)
    if err != nil {
        room.Logger.Warnf("Failed to push to clients: %v", err)
    }
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### 6.3 Selective Broadcasting

Example: Send to all clients except the sender:

```go
func (room *ChatRoom) BroadcastExceptSender(ctx context.Context, senderUserName string, msg proto.Message) error {
    room.mu.Lock()
    defer room.mu.Unlock()
    
    // Collect client IDs excluding sender
    var clientIDs []string
    for userName, clientID := range room.clientIDs {
        if userName != senderUserName {
            clientIDs = append(clientIDs, clientID)
        }
    }
    
    // Refactored API call
    return goverseapi.PushMessageToClient(ctx, clientIDs, msg)
}
```

---

## 7. Performance Considerations

### 7.1 Expected Improvements

**Scenario: 1000 clients on same gate**

| Metric | Current (individual calls) | Refactored (batch call) | Improvement |
|--------|---------------------------|-------------------------|-------------|
| Network messages | 1000 | 1 | **1000x** |
| Serialization ops | 1000 | 1 | **1000x** |
| Deserialization ops | 1000 | 1 | **1000x** |
| Gate-local fan-out | N/A | 1000 (cheap) | N/A |

**Scenario: 1000 clients across 10 gates (100 each)**

| Metric | Current | Refactored | Improvement |
|--------|---------|-----------|-------------|
| Network messages | 1000 | 10 | **100x** |
| Serialization ops | 1000 | 10 | **100x** |

### 7.2 API Usage Guidelines

**The refactored API is optimal for:**
- Broadcasting to multiple clients (2+)
- Clients likely on same gate (e.g., same region, same server)
- High-frequency broadcasts (chat, game state updates)

**Single client use case:**
- Pass a single-element slice: `PushMessageToClient(ctx, []string{clientID}, msg)`
- No performance penalty compared to previous API

### 7.3 Memory Impact

- **Temporary memory:** Group map scales with O(gates × clients_per_gate)
- **Typically negligible:** Even 10K clients = small map overhead
- **Bounded by:** Maximum clients per object (natural application limit)

---

## 8. Edge Cases and Error Handling

### 8.1 Client Disconnection

**Scenario:** Client disconnects between when `PushMessageToClient` is called and when gate receives message.

**Handling:**
- Gate logs warning for missing client
- Other clients in batch still receive message
- No error returned to sender (best-effort delivery)

### 9.2 Invalid Client IDs

**Scenario:** Client ID format is invalid (missing gate address).

**Handling:**
- Return error immediately during grouping phase
- No partial sends (all-or-nothing per gate)
- Caller can retry with corrected IDs

### 9.3 Gate Disconnection

**Scenario:** Gate disconnects while node is sending message.

**Handling:**
- Channel send fails/blocks depending on gate state
- Error returned to caller
- Caller can retry or handle gracefully

### 8.4 Empty Client List

**Scenario:** `PushMessageToClient` called with empty `clientIDs` slice.

**Handling:**
- Return success immediately (no-op)
- No network traffic generated
- No error (empty broadcast is valid)

### 8.5 Mixed Gate Distribution

**Scenario:** Clients span multiple gates.

**Handling:**
- Automatic optimization per gate
- Each gate receives envelope with its subset of clients
- Transparent to caller

---

## 9. Testing Strategy

### 9.1 Unit Tests

**`cluster/cluster_pushmessage_test.go`:**
- Test grouping logic (clients by gate)
- Test single client → `ClientMessageEnvelope` with one element
- Test multiple clients → `ClientMessageEnvelope` with multiple elements
- Test invalid client ID formats
- Test empty client list
- Test error handling (gate not found)

**`gate/gate_message_test.go`:**
- Test envelope handling with single client
- Test envelope handling with multiple clients
- Test client lookup (found vs. not found)
- Test message delivery to multiple clients
- Test partial failures (some clients disconnected)

### 9.2 Integration Tests

**`tests/integration/push_fanout_test.go`:**
- Test end-to-end: Object → Node → Gate → Clients
- Test multiple clients on same gate receive message
- Test clients on different gates receive message
- Test performance improvement (measure network calls)

### 9.3 Performance Tests

**Benchmarks:**
```go
// Benchmark refactored implementation with multiple clients
func BenchmarkPushMessageToClient_1000Clients(b *testing.B)

// Expected: 100x-1000x improvement for same-gate scenarios
```

### 9.4 Manual Testing

**Chat Sample:**
1. Run chat server with multiple nodes and gates
2. Connect 100+ clients to same gate
3. Send messages in chat room
4. Verify all clients receive messages
5. Monitor network traffic (should see reduction)
6. Check logs for envelope usage

---

## 10. Implementation Phases

### Phase 1: Protocol Refactor
- [ ] Refactor `ClientMessageEnvelope` in `proto/goverse.proto` (change `client_id` to `client_ids`)
- [ ] Compile protos
- [ ] Update generated code

### Phase 2: Gate-side Implementation
- [ ] Update `gate.Gate.handleGateMessage` to iterate over `client_ids`
- [ ] Add unit tests for multi-client handling
- [ ] Add logging for fan-out operations

### Phase 3: Cluster-side Implementation
- [ ] Refactor `cluster.Cluster.PushMessageToClient` to accept `[]string`
- [ ] Add client grouping logic by gate address
- [ ] Add unit tests for cluster-side logic

### Phase 4: API Refactor
- [ ] Refactor `goverseapi.PushMessageToClient` signature to accept `[]string`
- [ ] Update API documentation
- [ ] Add usage examples

### Phase 5: Integration and Testing
- [ ] Add integration tests
- [ ] Add performance benchmarks
- [ ] Update chat sample to use new API

### Phase 6: Sample Applications
- [ ] Update chat sample to use refactored API
- [ ] Document migration patterns

---

## 11. Future Enhancements

### 11.1 Automatic Batching Window

Future enhancement: Automatically batch multiple individual `PushMessageToClient` calls within a time window.

```go
// Potential future optimization
func (c *Cluster) PushMessageToClient(ctx context.Context, clientIDs []string, message proto.Message) error {
    // Check if in batching window for additional optimization
    if c.shouldBatch(ctx) {
        c.addToBatch(clientIDs, message)
        return nil
    }
    // ... existing implementation
}
```

### 11.2 Configurable Batching

Allow configuration of batching window:

```yaml
cluster:
  push_batching_window_ms: 10  # Batch pushes within 10ms window
```

### 11.3 Metrics and Monitoring

Add metrics to track optimization effectiveness:
- `push_messages_sent`: Total push messages
- `push_fanout_efficiency`: Average clients per network message
- `push_gates_per_broadcast`: Average gates per multi-client push

---

## 12. Broadcast to All Clients

### 12.1 Feature Overview

In addition to targeted client push, Goverse now supports broadcasting messages to all connected clients across all gates. This is useful for:
- Server-wide announcements
- Maintenance notifications
- Global events or updates
- System-wide alerts

### 12.2 API

**goverseapi.BroadcastToAllClients**

```go
// BroadcastToAllClients sends a message to all clients connected to all gates.
// The message is sent to all connected gates, and each gate fans out the message
// to all its connected clients.
func BroadcastToAllClients(ctx context.Context, message proto.Message) error
```

**Example usage:**

```go
// Server announcement
announcement := &MyNotification{
    Type: "server_announcement",
    Text: "Server maintenance in 5 minutes",
}
err := goverseapi.BroadcastToAllClients(ctx, announcement)
if err != nil {
    log.Printf("Failed to broadcast: %v", err)
}
```

### 12.3 Implementation

The broadcast feature uses a dedicated message type for clarity and type safety:

1. **Node sends to all gates**: Uses `BroadcastMessageEnvelope` message type
2. **Gate fans out to all clients**: Each gate broadcasts to all its connected clients locally

**Protocol:**
- Added `BroadcastMessageEnvelope` message type in `proto/goverse.proto`
- `GateMessage` oneof includes `broadcast_message` field
- Each gate handles the broadcast message by iterating through all connected clients

**Flow:**
```
Object → BroadcastToAllClients → 1 × serialize → N gates × network message → Each gate → M clients
```

Where N = number of connected gates, M = clients per gate

### 12.4 Performance Characteristics

**Network efficiency:**
- **O(gates)** network messages from node to gates
- **O(1)** local fan-out at each gate (in-memory iteration)

**Example scenario:**
- 10 gates, 100 clients per gate (1000 total clients)
- Broadcast: 10 network messages (1 per gate)
- Each gate: local iteration over 100 clients
- Result: **100x reduction** in network traffic vs. sending 1000 individual messages

### 12.5 Error Handling

**No gates connected:**
- Returns error: `"no gates connected"`
- No messages sent

**Gate channel full:**
- Logs warning for affected gate
- Continues with other gates
- Returns last error encountered

**Client disconnection:**
- Gate skips disconnected clients
- No error returned (best-effort delivery)

### 12.6 Testing

**Unit tests:**
- `cluster/cluster_broadcast_test.go`: Tests cluster-level broadcast
- `gate/gate_test.go`: Tests gate-level broadcast and fan-out

**Integration tests:**
- End-to-end broadcast validation across multiple gates and clients

---

## 13. Summary

**Benefits:**
- ✅ **Reduced Network Traffic**: O(gates) instead of O(clients)
- ✅ **Lower CPU Usage**: Fewer serialization/deserialization operations
- ✅ **Improved Scalability**: Handle large-scale broadcasts efficiently
- ✅ **Simple Refactor**: Unified API with slice parameter
- ✅ **Broadcast Support**: Easy server-wide announcements

**Impact:**
- **High-impact** for chat, game, notification systems
- **Breaking change** requires code updates in applications
- **Clear migration** path with better performance
- **Broadcast feature** enables efficient global messaging

**Implementation Status:**
- ✅ Protocol changes (ClientMessageEnvelope with repeated client_ids)
- ✅ Gate-side implementation (fan-out to clients)
- ✅ Cluster-side implementation (grouping by gate)
- ✅ API refactor (PushMessageToClients with []string)
- ✅ Broadcast API (BroadcastToAllClients)
- ✅ Unit tests and integration tests
- ✅ Sample applications updated (chat)
