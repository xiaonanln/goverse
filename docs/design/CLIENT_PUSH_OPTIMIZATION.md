# Client Push Message Fan-out Optimization

> **Status**: Draft / Planned  
> This document describes the proposed optimization for client push messaging to reduce network traffic and CPU usage when pushing to multiple clients.

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
Object → PushMessageToClients → 1 × serialize → 1 × network message → Gate → N × fan-out
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

Update `proto/goverse.proto` to support multi-client push messages.

### 3.1 New Message Type

Add `ClientMessageFanOutEnvelope` to wrap messages for multiple clients:

```proto
// ClientMessageFanOutEnvelope wraps a message for multiple clients on the same gate
message ClientMessageFanOutEnvelope {
  repeated string client_ids = 1;  // List of client IDs to receive the message
  google.protobuf.Any message = 2; // The message payload (same for all clients)
}
```

### 3.2 Update GateMessage

Extend `GateMessage` to include the new fan-out envelope type:

```proto
message GateMessage {
  oneof message {
    RegisterGateResponse register_gate_response = 1;
    ClientMessageEnvelope client_message = 2;           // Existing: single client
    ClientMessageFanOutEnvelope client_fanout_message = 3; // New: multiple clients
  }
}
```

**Note**: This maintains backward compatibility. Existing single-client messages continue to use `client_message` field.

---

## 4. API Changes

Add new APIs to `goverseapi` and `cluster` to support pushing to multiple clients.

### 4.1 goverseapi

```go
// PushMessageToClients sends a message to multiple clients.
// This is more efficient than calling PushMessageToClient multiple times
// when clients are connected to the same gate.
//
// The implementation automatically groups clients by gate and uses
// fan-out optimization for clients on the same gate.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - clientIDs: List of client IDs (format: "gateAddress/uniqueId")
//   - message: The protobuf message to push to all clients
//
// Returns:
//   - error: Non-nil if any push operation fails
//
// Example:
//   clientIDs := []string{"localhost:7001/user1", "localhost:7001/user2"}
//   err := goverseapi.PushMessageToClients(ctx, clientIDs, notification)
func PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error
```

### 4.2 cluster.Cluster

```go
// PushMessageToClients sends a message to multiple clients by their IDs.
// Automatically groups clients by gate address and uses fan-out optimization.
func (c *Cluster) PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error
```

---

## 5. Implementation Details

### 5.1 Node Side (`cluster.Cluster.PushMessageToClients`)

**Algorithm:**

1. **Group clients by gate address:**
   - Parse each `clientID` to extract gate address (format: `gateAddress/uniqueId`)
   - Group client IDs by their gate address using a map

2. **For each gate, optimize envelope type:**
   - If **one client** for this gate → use existing `ClientMessageEnvelope`
   - If **multiple clients** for this gate → use new `ClientMessageFanOutEnvelope`

3. **Send to gate stream:**
   - Serialize message to `google.protobuf.Any` once per gate
   - Wrap in appropriate envelope type
   - Send via gate's registered stream channel

**Pseudocode:**

```go
func (c *Cluster) PushMessageToClients(ctx context.Context, clientIDs []string, message proto.Message) error {
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
    
    // Send optimized envelope to each gate
    for gateAddr, clients := range clientsByGate {
        var envelope proto.Message
        
        if len(clients) == 1 {
            // Single client - use existing envelope
            envelope = &goverse_pb.ClientMessageEnvelope{
                ClientId: clients[0],
                Message:  anyMsg,
            }
        } else {
            // Multiple clients - use fan-out envelope
            envelope = &goverse_pb.ClientMessageFanOutEnvelope{
                ClientIds: clients,
                Message:   anyMsg,
            }
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

1. **Update gate message handler** to handle both envelope types
2. **For `ClientMessageFanOutEnvelope`:**
   - Iterate over `client_ids` in the envelope
   - For each client ID:
     - Look up client proxy: `g.GetClient(clientID)`
     - If found, push message: `client.PushMessageAny(envelope.Message)`
     - If not found, log warning (client may have disconnected)

**Pseudocode:**

```go
func (g *Gate) handleGateMessage(msg *goverse_pb.GateMessage) {
    switch m := msg.Message.(type) {
    case *goverse_pb.GateMessage_ClientMessage:
        // Existing: single client
        client := g.GetClient(m.ClientMessage.ClientId)
        if client != nil {
            client.PushMessageAny(m.ClientMessage.Message)
        }
        
    case *goverse_pb.GateMessage_ClientFanoutMessage:
        // New: multiple clients (fan-out)
        for _, clientID := range m.ClientFanoutMessage.ClientIds {
            client := g.GetClient(clientID)
            if client != nil {
                client.PushMessageAny(m.ClientFanoutMessage.Message)
            } else {
                g.logger.Warnf("Client %s not found, may have disconnected", clientID)
            }
        }
    }
}
```

---

## 6. Backward Compatibility

The existing `PushMessageToClient` API and `ClientMessageEnvelope` will be **preserved** to maintain backward compatibility:

- **Old code continues to work:** No breaking changes to existing applications
- **Gradual migration:** Applications can migrate to `PushMessageToClients` incrementally
- **Performance improvement:** Even with single client, new API uses same efficient code path

**Migration path:**
1. Implement new protocol and APIs
2. Update internal examples (chat sample)
3. Document benefits in migration guide
4. Applications migrate at their own pace
5. Consider deprecating `PushMessageToClient` in future major version (not immediately)

---

## 7. Example Usage

### 7.1 Chat Room (Before)

Current implementation using `PushMessageToClient`:

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
    
    // Current: Send individually to each client (inefficient)
    for _, clientID := range room.clientIDs {
        err := goverseapi.PushMessageToClient(ctx, clientID, notification)
        if err != nil {
            room.Logger.Warnf("Failed to push to client %s: %v", clientID, err)
        }
    }
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### 7.2 Chat Room (After)

Optimized implementation using `PushMessageToClients`:

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
    
    // Optimized: Collect all client IDs and send in batch
    clientIDs := make([]string, 0, len(room.clientIDs))
    for _, clientID := range room.clientIDs {
        clientIDs = append(clientIDs, clientID)
    }
    
    // Single call with automatic fan-out optimization
    err := goverseapi.PushMessageToClients(ctx, clientIDs, notification)
    if err != nil {
        room.Logger.Warnf("Failed to push to clients: %v", err)
    }
    
    return &chat_pb.Client_SendChatMessageResponse{}, nil
}
```

### 7.3 Selective Broadcasting

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
    
    // Optimized batch send
    return goverseapi.PushMessageToClients(ctx, clientIDs, msg)
}
```

---

## 8. Performance Considerations

### 8.1 Expected Improvements

**Scenario: 1000 clients on same gate**

| Metric | Current (PushMessageToClient) | Optimized (PushMessageToClients) | Improvement |
|--------|-------------------------------|----------------------------------|-------------|
| Network messages | 1000 | 1 | **1000x** |
| Serialization ops | 1000 | 1 | **1000x** |
| Deserialization ops | 1000 | 1 | **1000x** |
| Gate-local fan-out | N/A | 1000 (cheap) | N/A |

**Scenario: 1000 clients across 10 gates (100 each)**

| Metric | Current | Optimized | Improvement |
|--------|---------|-----------|-------------|
| Network messages | 1000 | 10 | **100x** |
| Serialization ops | 1000 | 10 | **100x** |

### 8.2 When to Use

**Use `PushMessageToClients` when:**
- Broadcasting to multiple clients (2+)
- Clients likely on same gate (e.g., same region, same server)
- High-frequency broadcasts (chat, game state updates)

**Use `PushMessageToClient` when:**
- Sending to exactly one client
- Different messages per client (cannot be batched)

### 8.3 Memory Impact

- **Temporary memory:** Group map scales with O(gates × clients_per_gate)
- **Typically negligible:** Even 10K clients = small map overhead
- **Bounded by:** Maximum clients per object (natural application limit)

---

## 9. Edge Cases and Error Handling

### 9.1 Client Disconnection

**Scenario:** Client disconnects between when `PushMessageToClients` is called and when gate receives message.

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

### 9.4 Empty Client List

**Scenario:** `PushMessageToClients` called with empty `clientIDs` slice.

**Handling:**
- Return success immediately (no-op)
- No network traffic generated
- No error (empty broadcast is valid)

### 9.5 Mixed Gate Distribution

**Scenario:** Clients span multiple gates.

**Handling:**
- Automatic optimization per gate
- Each gate receives optimized envelope
- Transparent to caller

---

## 10. Testing Strategy

### 10.1 Unit Tests

**`cluster/cluster_pushmessage_test.go`:**
- Test grouping logic (clients by gate)
- Test single client → `ClientMessageEnvelope`
- Test multiple clients → `ClientMessageFanOutEnvelope`
- Test invalid client ID formats
- Test empty client list
- Test error handling (gate not found)

**`gate/gate_message_test.go`:**
- Test fan-out envelope handling
- Test client lookup (found vs. not found)
- Test message delivery to multiple clients
- Test partial failures (some clients disconnected)

### 10.2 Integration Tests

**`tests/integration/push_fanout_test.go`:**
- Test end-to-end: Object → Node → Gate → Clients
- Test multiple clients on same gate receive message
- Test clients on different gates receive message
- Test performance improvement (measure network calls)
- Test backward compatibility (mix old and new APIs)

### 10.3 Performance Tests

**Benchmarks:**
```go
// Benchmark current implementation
func BenchmarkPushMessageToClient_1000Clients(b *testing.B)

// Benchmark optimized implementation
func BenchmarkPushMessageToClients_1000Clients(b *testing.B)

// Expected: 100x-1000x improvement for same-gate scenarios
```

### 10.4 Manual Testing

**Chat Sample:**
1. Run chat server with multiple nodes and gates
2. Connect 100+ clients to same gate
3. Send messages in chat room
4. Verify all clients receive messages
5. Monitor network traffic (should see reduction)
6. Check logs for fan-out envelope usage

---

## 11. Implementation Phases

### Phase 1: Protocol and Infrastructure
- [ ] Add `ClientMessageFanOutEnvelope` to `proto/goverse.proto`
- [ ] Compile protos
- [ ] Update `GateMessage` definition

### Phase 2: Gate-side Implementation
- [ ] Update `gate.Gate.handleGateMessage` to handle fan-out envelopes
- [ ] Add unit tests for gate-side fan-out logic
- [ ] Add logging for fan-out operations

### Phase 3: Cluster-side Implementation
- [ ] Implement `cluster.Cluster.PushMessageToClients`
- [ ] Add client grouping logic by gate address
- [ ] Add unit tests for cluster-side logic

### Phase 4: API Exposure
- [ ] Add `goverseapi.PushMessageToClients`
- [ ] Update API documentation
- [ ] Add usage examples

### Phase 5: Integration and Testing
- [ ] Add integration tests
- [ ] Add performance benchmarks
- [ ] Update chat sample to use new API

### Phase 6: Documentation
- [ ] Update user guide
- [ ] Add migration guide
- [ ] Document performance characteristics

---

## 12. Future Enhancements

### 12.1 Automatic API Selection

Future enhancement: Make `PushMessageToClient` automatically batch when called multiple times in quick succession.

```go
// Potential future optimization
func (c *Cluster) PushMessageToClient(ctx context.Context, clientID string, message proto.Message) error {
    // Check if in batching window
    if c.shouldBatch(ctx) {
        c.addToBatch(clientID, message)
        return nil
    }
    // ... existing implementation
}
```

### 12.2 Configurable Batching Window

Allow configuration of batching window for automatic optimization:

```yaml
cluster:
  push_batching_window_ms: 10  # Batch pushes within 10ms window
```

### 12.3 Metrics and Monitoring

Add metrics to track optimization effectiveness:
- `push_messages_sent`: Total push messages
- `push_fanout_efficiency`: Average clients per network message
- `push_gates_per_broadcast`: Average gates per multi-client push

---

## 13. Summary

**Benefits:**
- ✅ **Reduced Network Traffic**: O(gates) instead of O(clients)
- ✅ **Lower CPU Usage**: Fewer serialization/deserialization operations
- ✅ **Improved Scalability**: Handle large-scale broadcasts efficiently
- ✅ **Backward Compatible**: Existing code continues to work
- ✅ **Simple API**: Easy migration path for applications

**Impact:**
- **High-impact** for chat, game, notification systems
- **Low-risk** due to backward compatibility
- **Easy adoption** through incremental migration

**Next Steps:**
1. Prototype implementation
2. Performance benchmarking
3. Review and approval
4. Phased rollout
