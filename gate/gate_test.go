package gate

import (
	"context"
	"testing"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
)

func TestNewGateway(t *testing.T) {
	tests := []struct {
		name       string
		config     *GatewayConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "valid config",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/test-gateway",
			},
			wantErr: false,
		},
		{
			name: "valid config with default prefix",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
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
			config: &GatewayConfig{
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, err := NewGateway(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewGateway() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("NewGateway() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if gateway != nil {
				// Clean up
				defer gateway.Stop()

				// Verify defaults are set
				if tt.config != nil && tt.config.EtcdPrefix == "" {
					if gateway.config.EtcdPrefix != "/goverse" {
						t.Fatalf("Expected default EtcdPrefix to be /goverse, got %s", gateway.config.EtcdPrefix)
					}
				}
			}
		})
	}
}

func TestValidateGatewayConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     *GatewayConfig
		wantErr    bool
		errContain string
		checkFunc  func(*testing.T, *GatewayConfig)
	}{
		{
			name: "valid config",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/custom",
			},
			wantErr: false,
		},
		{
			name: "sets default prefix",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *GatewayConfig) {
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
			config: &GatewayConfig{
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GatewayConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGatewayConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateGatewayConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("validateGatewayConfig() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if err == nil && tt.checkFunc != nil {
				tt.checkFunc(t, tt.config)
			}
		})
	}
}

func TestGatewayStartStop(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-lifecycle",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	ctx := context.Background()

	// Start gateway
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Stop gateway
	err = gateway.Stop()
	if err != nil {
		t.Fatalf("Gateway.Stop() returned error: %v", err)
	}
}

func TestGatewayMultipleStops(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-multistop",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	ctx := context.Background()

	// Start gateway
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Stop multiple times should not panic or error
	if err := gateway.Stop(); err != nil {
		t.Fatalf("First Stop() returned error: %v", err)
	}

	if err := gateway.Stop(); err != nil {
		t.Fatalf("Second Stop() returned error: %v", err)
	}
}

func TestGatewayRegister(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-register",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Register should return a valid client ID
	clientID, err := gateway.Register(ctx)
	if err != nil {
		t.Fatalf("Register() returned error: %v", err)
	}
	if clientID == "" {
		t.Fatalf("Register() returned empty client ID")
	}

	// Verify client ID format (should start with advertise address)
	if !contains(clientID, "localhost:49000/") {
		t.Fatalf("Register() clientID = %s, want to start with 'localhost:49000/'", clientID)
	}

	// Verify client proxy exists in gateway
	clientProxy, exists := gateway.GetClient(clientID)
	if !exists {
		t.Fatalf("Client proxy not found after registration")
	}
	if clientProxy.GetID() != clientID {
		t.Fatalf("Client proxy ID mismatch: got %s, want %s", clientProxy.GetID(), clientID)
	}

	// Verify message channel is available
	msgChan := clientProxy.MessageChan()
	if msgChan == nil {
		t.Fatalf("Message channel is nil")
	}

	// Test Unregister
	gateway.Unregister(clientID)
	_, exists = gateway.GetClient(clientID)
	if exists {
		t.Fatalf("Client proxy still exists after unregister")
	}
}

func TestGatewayCallObject(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-callobject",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	req := &gate_pb.CallObjectRequest{
		ClientId: "test-client",
		Method:   "TestMethod",
		Type:     "TestObject",
		Id:       "test-object-1",
	}

	// CallObject should return "not implemented" error
	resp, err := gateway.CallObject(ctx, req)
	if err == nil {
		t.Fatalf("CallObject() expected error, got response: %v", resp)
	}
	if !contains(err.Error(), "not implemented") {
		t.Fatalf("CallObject() error = %v, want error containing 'not implemented'", err)
	}
}

func TestGatewayCreateObject(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-createobject",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	req := &gate_pb.CreateObjectRequest{
		Type: "TestObject",
		Id:   "test-object-1",
	}

	// CreateObject should return "not implemented" error
	resp, err := gateway.CreateObject(ctx, req)
	if err == nil {
		t.Fatalf("CreateObject() expected error, got response: %v", resp)
	}
	if !contains(err.Error(), "not implemented") {
		t.Fatalf("CreateObject() error = %v, want error containing 'not implemented'", err)
	}
}

func TestGatewayDeleteObject(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-deleteobject",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	req := &gate_pb.DeleteObjectRequest{
		Id: "test-object-1",
	}

	// DeleteObject should return "not implemented" error
	resp, err := gateway.DeleteObject(ctx, req)
	if err == nil {
		t.Fatalf("DeleteObject() expected error, got response: %v", resp)
	}
	if !contains(err.Error(), "not implemented") {
		t.Fatalf("DeleteObject() error = %v, want error containing 'not implemented'", err)
	}
}

func TestGatewayStartWithoutStop(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-nostop",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	ctx := context.Background()

	// Start gateway
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Can start multiple times without error (idempotent)
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() second call returned error: %v", err)
	}

	// Clean up
	gateway.Stop()
}

func TestGatewayRegisterMultipleClients(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-multiple",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Register multiple clients
	numClients := 5
	clientIDs := make([]string, numClients)

	for i := 0; i < numClients; i++ {
		clientID, err := gateway.Register(ctx)
		if err != nil {
			t.Fatalf("Register() #%d returned error: %v", i, err)
		}
		clientIDs[i] = clientID
	}

	// Verify all clients are registered and have unique IDs
	seenIDs := make(map[string]bool)
	for i, clientID := range clientIDs {
		if seenIDs[clientID] {
			t.Fatalf("Duplicate client ID: %s", clientID)
		}
		seenIDs[clientID] = true

		clientProxy, exists := gateway.GetClient(clientID)
		if !exists {
			t.Fatalf("Client #%d not found after registration", i)
		}
		if clientProxy.GetID() != clientID {
			t.Fatalf("Client #%d ID mismatch: got %s, want %s", i, clientProxy.GetID(), clientID)
		}
	}

	// Unregister all clients
	for i, clientID := range clientIDs {
		gateway.Unregister(clientID)
		_, exists := gateway.GetClient(clientID)
		if exists {
			t.Fatalf("Client #%d still exists after unregister", i)
		}
	}
}

func TestGatewayUnregisterNonExistentClient(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-unregister",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	ctx := context.Background()
	err = gateway.Start(ctx)
	if err != nil {
		t.Fatalf("Gateway.Start() returned error: %v", err)
	}

	// Unregister a non-existent client should not panic or error
	gateway.Unregister("non-existent-client")

	// Should still be able to register new clients
	clientID, err := gateway.Register(ctx)
	if err != nil {
		t.Fatalf("Register() after unregister non-existent returned error: %v", err)
	}
	if clientID == "" {
		t.Fatalf("Register() returned empty client ID")
	}
}

func TestGatewayGetClientNotFound(t *testing.T) {
	config := &GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-getclient",
	}

	gateway, err := NewGateway(config)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gateway.Stop()

	// Get non-existent client
	_, exists := gateway.GetClient("non-existent-client")
	if exists {
		t.Fatalf("GetClient() returned true for non-existent client")
	}
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
