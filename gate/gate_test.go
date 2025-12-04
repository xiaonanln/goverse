package gate

import (
	"context"
	"testing"
	"time"

	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestNewGate(t *testing.T) {
	// Get free addresses for tests that need them
	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()

	tests := []struct {
		name       string
		config     *GateConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "valid config",
			config: &GateConfig{
				AdvertiseAddress: addr1,
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/test-gate",
			},
			wantErr: false,
		},
		{
			name: "valid config with default prefix",
			config: &GateConfig{
				AdvertiseAddress: addr2,
				EtcdAddress:      "localhost:2379",
			},
			wantErr: false,
		},
		{
			name:       "nil config",
			config:     nil,
			wantErr:    true,
			errContain: "config cannot be nil",
		},
		{
			name: "empty advertise address",
			config: &GateConfig{
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GateConfig{
				AdvertiseAddress: addr1, // Reuse addr1 since this test will fail validation
				EtcdAddress:      "",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate, err := NewGate(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewGate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("NewGate() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if gate != nil {
				// Clean up
				defer gate.Stop()

				// Verify defaults are set
				if tt.config != nil && tt.config.EtcdPrefix == "" {
					if gate.config.EtcdPrefix != "/goverse" {
						t.Fatalf("Expected default EtcdPrefix to be /goverse, got %s", gate.config.EtcdPrefix)
					}
				}
			}
		})
	}
}

func TestValidateGateConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     *GateConfig
		wantErr    bool
		errContain string
		checkFunc  func(*testing.T, *GateConfig)
	}{
		{
			name: "valid config",
			config: &GateConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/custom",
			},
			wantErr: false,
		},
		{
			name: "sets default prefix",
			config: &GateConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *GateConfig) {
				if cfg.EtcdPrefix != "/goverse" {
					t.Fatalf("Expected EtcdPrefix to be /goverse, got %s", cfg.EtcdPrefix)
				}
			},
		},
		{
			name:       "nil config",
			config:     nil,
			wantErr:    true,
			errContain: "config cannot be nil",
		},
		{
			name: "empty advertise address",
			config: &GateConfig{
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GateConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateGateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("validateGateConfig() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if err == nil && tt.checkFunc != nil {
				tt.checkFunc(t, tt.config)
			}
		})
	}
}

func TestGateStartStop(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-lifecycle",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	ctx := context.Background()

	// Start gate
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Stop gate
	err = gate.Stop()
	if err != nil {
		t.Fatalf("Gate.Stop() returned error: %v", err)
	}
}

func TestGateMultipleStops(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-multistop",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	ctx := context.Background()

	// Start gate
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Stop multiple times should not panic or error
	if err := gate.Stop(); err != nil {
		t.Fatalf("First Stop() returned error: %v", err)
	}

	if err := gate.Stop(); err != nil {
		t.Fatalf("Second Stop() returned error: %v", err)
	}
}

func TestGateRegister(t *testing.T) {
	advertiseAddr := testutil.GetFreeAddress()
	config := &GateConfig{
		AdvertiseAddress: advertiseAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-register",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Register should return a valid client proxy
	clientProxy := gate.Register(ctx)
	if clientProxy == nil {
		t.Fatalf("Register() returned nil clientProxy")
	}

	clientID := clientProxy.GetID()
	if clientID == "" {
		t.Fatalf("Register() returned empty client ID")
	}

	// Verify client ID format (should start with advertise address)
	expectedPrefix := advertiseAddr + "/"
	if !contains(clientID, expectedPrefix) {
		t.Fatalf("Register() clientID = %s, want to start with '%s'", clientID, expectedPrefix)
	}

	// Verify client proxy exists in gate
	clientProxy2, exists := gate.GetClient(clientID)
	if !exists {
		t.Fatalf("Client proxy not found after registration")
	}
	if clientProxy2.GetID() != clientID {
		t.Fatalf("Client proxy ID mismatch: got %s, want %s", clientProxy2.GetID(), clientID)
	}

	// Verify message channel is available
	msgChan := clientProxy.MessageChan()
	if msgChan == nil {
		t.Fatalf("Message channel is nil")
	}

	// Test Unregister
	gate.Unregister(clientID)
	_, exists = gate.GetClient(clientID)
	if exists {
		t.Fatalf("Client proxy still exists after unregister")
	}
}

func TestGateStartWithoutStop(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-nostop",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	ctx := context.Background()

	// Start gate
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Can start multiple times without error (idempotent)
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() second call returned error: %v", err)
	}

	// Clean up
	gate.Stop()
}

func TestGateRegisterMultipleClients(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-multiple",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Register multiple clients
	numClients := 5
	clientIDs := make([]string, numClients)

	for i := 0; i < numClients; i++ {
		clientProxy := gate.Register(ctx)
		clientIDs[i] = clientProxy.GetID()
	}

	// Verify all clients are registered and have unique IDs
	seenIDs := make(map[string]bool)
	for i, clientID := range clientIDs {
		if seenIDs[clientID] {
			t.Fatalf("Duplicate client ID: %s", clientID)
		}
		seenIDs[clientID] = true

		clientProxy, exists := gate.GetClient(clientID)
		if !exists {
			t.Fatalf("Client #%d not found after registration", i)
		}
		if clientProxy.GetID() != clientID {
			t.Fatalf("Client #%d ID mismatch: got %s, want %s", i, clientProxy.GetID(), clientID)
		}
	}

	// Unregister all clients
	for i, clientID := range clientIDs {
		gate.Unregister(clientID)
		_, exists := gate.GetClient(clientID)
		if exists {
			t.Fatalf("Client #%d still exists after unregister", i)
		}
	}
}

func TestGateUnregisterNonExistentClient(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-unregister",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Unregister a non-existent client should not panic or error
	gate.Unregister("non-existent-client")

	// Should still be able to register new clients
	clientProxy := gate.Register(ctx)
	if clientProxy.GetID() == "" {
		t.Fatalf("Register() returned empty client ID")
	}
}

func TestGateGetClientNotFound(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-getclient",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	// Get non-existent client
	_, exists := gate.GetClient("non-existent-client")
	if exists {
		t.Fatalf("GetClient() returned true for non-existent client")
	}
}

func TestGateStopCleansUpAllClientProxies(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-cleanup",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Register multiple clients
	numClients := 10
	clientProxies := make([]*ClientProxy, numClients)
	clientIDs := make([]string, numClients)

	for i := 0; i < numClients; i++ {
		clientProxies[i] = gate.Register(ctx)
		clientIDs[i] = clientProxies[i].GetID()
	}

	// Verify all clients are registered
	for i, clientID := range clientIDs {
		_, exists := gate.GetClient(clientID)
		if !exists {
			t.Fatalf("Client #%d not found after registration", i)
		}

		// Verify message channel is open
		msgChan := clientProxies[i].MessageChan()
		if msgChan == nil {
			t.Fatalf("Client #%d message channel is nil before Stop", i)
		}
	}

	// Stop the gate
	err = gate.Stop()
	if err != nil {
		t.Fatalf("Gate.Stop() returned error: %v", err)
	}

	// Verify all clients are unregistered
	for i, clientID := range clientIDs {
		_, exists := gate.GetClient(clientID)
		if exists {
			t.Fatalf("Client #%d still exists after Stop", i)
		}

		// Verify client proxy is closed
		if !clientProxies[i].IsClosed() {
			t.Fatalf("Client #%d not closed after Stop", i)
		}
	}

	// Verify internal clients map is empty
	gate.clientsMu.RLock()
	clientCount := len(gate.clients)
	gate.clientsMu.RUnlock()

	if clientCount != 0 {
		t.Fatalf("Gate still has %d clients after Stop, expected 0", clientCount)
	}
}

func TestGateHandleGateMessage(t *testing.T) {
	config := &GateConfig{
		AdvertiseAddress: testutil.GetFreeAddress(),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-handlemsg",
	}

	gate, err := NewGate(config)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gate.Stop()

	ctx := context.Background()
	err = gate.Start(ctx)
	if err != nil {
		t.Fatalf("Gate.Start() returned error: %v", err)
	}

	// Register two clients to verify correct routing
	clientProxy1 := gate.Register(ctx)
	clientID1 := clientProxy1.GetID()

	clientProxy2 := gate.Register(ctx)
	clientID2 := clientProxy2.GetID()

	t.Run("ClientMessage_RoutesToCorrectClient", func(t *testing.T) {
		// Create messages for both clients
		testMessage1 := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"text":   structpb.NewStringValue("Message for client 1"),
				"client": structpb.NewStringValue("1"),
			},
		}

		testMessage2 := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"text":   structpb.NewStringValue("Message for client 2"),
				"client": structpb.NewStringValue("2"),
			},
		}

		// Send message to client 1
		anyMsg1, err := anypb.New(testMessage1)
		if err != nil {
			t.Fatalf("Failed to create Any message: %v", err)
		}

		envelope1 := &goverse_pb.ClientMessageEnvelope{
			ClientId: clientID1,
			Message:  anyMsg1,
		}

		gateMsg1 := &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_ClientMessage{
				ClientMessage: envelope1,
			},
		}

		gate.handleGateMessage("test-node", gateMsg1)

		// Send message to client 2
		anyMsg2, err := anypb.New(testMessage2)
		if err != nil {
			t.Fatalf("Failed to create Any message: %v", err)
		}

		envelope2 := &goverse_pb.ClientMessageEnvelope{
			ClientId: clientID2,
			Message:  anyMsg2,
		}

		gateMsg2 := &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_ClientMessage{
				ClientMessage: envelope2,
			},
		}

		gate.handleGateMessage("test-node", gateMsg2)

		// Verify client 1 receives only its message
		select {
		case anyMsg := <-clientProxy1.MessageChan():
			if anyMsg == nil {
				t.Fatal("Client 1: Received nil message")
			}
			receivedStruct := &structpb.Struct{}
			if err := anyMsg.UnmarshalTo(receivedStruct); err != nil {
				t.Fatalf("Client 1: Failed to unmarshal Any to Struct: %v", err)
			}
			if receivedStruct.Fields["client"].GetStringValue() != "1" {
				t.Fatalf("Client 1 received message for client %s", receivedStruct.Fields["client"].GetStringValue())
			}
			if receivedStruct.Fields["text"].GetStringValue() != "Message for client 1" {
				t.Fatalf("Client 1: Expected 'Message for client 1', got %q", receivedStruct.Fields["text"].GetStringValue())
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for message on client 1 channel")
		}

		// Verify client 2 receives only its message
		select {
		case anyMsg := <-clientProxy2.MessageChan():
			if anyMsg == nil {
				t.Fatal("Client 2: Received nil message")
			}
			receivedStruct := &structpb.Struct{}
			if err := anyMsg.UnmarshalTo(receivedStruct); err != nil {
				t.Fatalf("Client 2: Failed to unmarshal Any to Struct: %v", err)
			}
			if receivedStruct.Fields["client"].GetStringValue() != "2" {
				t.Fatalf("Client 2 received message for client %s", receivedStruct.Fields["client"].GetStringValue())
			}
			if receivedStruct.Fields["text"].GetStringValue() != "Message for client 2" {
				t.Fatalf("Client 2: Expected 'Message for client 2', got %q", receivedStruct.Fields["text"].GetStringValue())
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for message on client 2 channel")
		}

		// Verify no extra messages on either channel
		select {
		case msg := <-clientProxy1.MessageChan():
			t.Fatalf("Client 1 received unexpected extra message: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Good - no extra messages
		}

		select {
		case msg := <-clientProxy2.MessageChan():
			t.Fatalf("Client 2 received unexpected extra message: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Good - no extra messages
		}
	})

	t.Run("ClientMessage_ClientNotFound", func(t *testing.T) {
		// Create a message for non-existent client
		testMessage := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"text": structpb.NewStringValue("Should be dropped"),
			},
		}

		anyMsg, err := anypb.New(testMessage)
		if err != nil {
			t.Fatalf("Failed to create Any message: %v", err)
		}

		envelope := &goverse_pb.ClientMessageEnvelope{
			ClientId: "non-existent-client",
			Message:  anyMsg,
		}

		gateMsg := &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_ClientMessage{
				ClientMessage: envelope,
			},
		}

		// Handle the message - should log warning and drop message
		gate.handleGateMessage("test-node", gateMsg)

		// Verify our registered clients did NOT receive the message
		select {
		case msg := <-clientProxy1.MessageChan():
			t.Fatalf("Client 1 received message intended for different client: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}

		select {
		case msg := <-clientProxy2.MessageChan():
			t.Fatalf("Client 2 received message intended for different client: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}
	})

	t.Run("ClientMessage_NilMessage", func(t *testing.T) {
		// Create envelope with nil message
		envelope := &goverse_pb.ClientMessageEnvelope{
			ClientId: clientID1,
			Message:  nil,
		}

		gateMsg := &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_ClientMessage{
				ClientMessage: envelope,
			},
		}

		// Handle the message - should log warning and not crash
		gate.handleGateMessage("test-node", gateMsg)

		// Verify client did NOT receive a message
		select {
		case msg := <-clientProxy1.MessageChan():
			t.Fatalf("Client 1 received message when none expected: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}
	})

	t.Run("RegisterGateResponse", func(t *testing.T) {
		// Create RegisterGateResponse message
		gateMsg := &goverse_pb.GateMessage{
			Message: &goverse_pb.GateMessage_RegisterGateResponse{
				RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
			},
		}

		// Handle the message - should just log
		gate.handleGateMessage("test-node", gateMsg)

		// Verify clients did NOT receive anything
		select {
		case msg := <-clientProxy1.MessageChan():
			t.Fatalf("Client 1 received RegisterGateResponse: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}

		select {
		case msg := <-clientProxy2.MessageChan():
			t.Fatalf("Client 2 received RegisterGateResponse: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}
	})

	t.Run("UnknownMessageType", func(t *testing.T) {
		// Create GateMessage with nil message (unknown type)
		gateMsg := &goverse_pb.GateMessage{
			Message: nil,
		}

		// Handle the message - should log warning and not crash
		gate.handleGateMessage("test-node", gateMsg)

		// Verify clients did NOT receive a message
		select {
		case msg := <-clientProxy1.MessageChan():
			t.Fatalf("Client 1 received unknown message: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}

		select {
		case msg := <-clientProxy2.MessageChan():
			t.Fatalf("Client 2 received unknown message: %v", msg)
		case <-time.After(50 * time.Millisecond):
			// Expected - no message received
		}
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestGateGetClientCount(t *testing.T) {
config := &GateConfig{
AdvertiseAddress: testutil.GetFreeAddress(),
EtcdAddress:      "localhost:2379",
EtcdPrefix:       "/test-gate-clientcount",
}

gate, err := NewGate(config)
if err != nil {
t.Fatalf("Failed to create gate: %v", err)
}
defer gate.Stop()

ctx := context.Background()
err = gate.Start(ctx)
if err != nil {
t.Fatalf("Gate.Start() returned error: %v", err)
}

// Initially should have 0 clients
if count := gate.GetClientCount(); count != 0 {
t.Fatalf("Expected 0 clients initially, got %d", count)
}

// Register 3 clients
clientProxies := make([]*ClientProxy, 3)
for i := 0; i < 3; i++ {
clientProxies[i] = gate.Register(ctx)
}

// Should have 3 clients now
if count := gate.GetClientCount(); count != 3 {
t.Fatalf("Expected 3 clients after registering, got %d", count)
}

// Unregister one client
gate.Unregister(clientProxies[0].GetID())

// Should have 2 clients now
if count := gate.GetClientCount(); count != 2 {
t.Fatalf("Expected 2 clients after unregistering one, got %d", count)
}

// Unregister the remaining clients
gate.Unregister(clientProxies[1].GetID())
gate.Unregister(clientProxies[2].GetID())

// Should have 0 clients now
if count := gate.GetClientCount(); count != 0 {
t.Fatalf("Expected 0 clients after unregistering all, got %d", count)
}
}
