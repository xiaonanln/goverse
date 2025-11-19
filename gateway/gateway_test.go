package gateway

import (
	"context"
	"testing"
	"time"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
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
				EtcdAddress: "localhost:2379",
				EtcdPrefix:  "/test-gateway",
			},
			wantErr: false,
		},
		{
			name: "valid config with default prefix",
			config: &GatewayConfig{
				EtcdAddress: "localhost:2379",
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
			name: "empty etcd address",
			config: &GatewayConfig{
				EtcdAddress: "",
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

				// Verify etcdManager is initialized
				if gateway.etcdManager == nil {
					t.Fatal("Expected etcdManager to be initialized")
				}

				// Verify shardLock is initialized
				if gateway.shardLock == nil {
					t.Fatal("Expected shardLock to be initialized")
				}

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
				EtcdAddress: "localhost:2379",
				EtcdPrefix:  "/custom",
			},
			wantErr: false,
		},
		{
			name: "sets default prefix",
			config: &GatewayConfig{
				EtcdAddress: "localhost:2379",
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
			name: "empty etcd address",
			config: &GatewayConfig{
				EtcdAddress: "",
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-lifecycle",
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-multistop",
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-register",
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

	// Register should return "not implemented" error
	clientID, err := gateway.Register(ctx)
	if err == nil {
		t.Fatalf("Register() expected error, got clientID: %s", clientID)
	}
	if !contains(err.Error(), "not implemented") {
		t.Fatalf("Register() error = %v, want error containing 'not implemented'", err)
	}
}

func TestGatewayCallObject(t *testing.T) {
	config := &GatewayConfig{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-callobject",
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

	req := &gateway_pb.CallObjectRequest{
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-createobject",
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

	req := &gateway_pb.CreateObjectRequest{
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-deleteobject",
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

	req := &gateway_pb.DeleteObjectRequest{
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
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-nostop",
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

func TestGatewayConfigDefaults(t *testing.T) {
	tests := []struct {
		name                       string
		inputPrefix                string
		inputManagementInterval    time.Duration
		expectedPrefix             string
		expectedManagementInterval time.Duration
	}{
		{
			name:                       "custom prefix",
			inputPrefix:                "/custom-prefix",
			inputManagementInterval:    0,
			expectedPrefix:             "/custom-prefix",
			expectedManagementInterval: DefaultManagementInterval,
		},
		{
			name:                       "empty prefix uses default",
			inputPrefix:                "",
			inputManagementInterval:    0,
			expectedPrefix:             "/goverse",
			expectedManagementInterval: DefaultManagementInterval,
		},
		{
			name:                       "custom management interval",
			inputPrefix:                "/goverse",
			inputManagementInterval:    10 * time.Second,
			expectedPrefix:             "/goverse",
			expectedManagementInterval: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &GatewayConfig{
				EtcdAddress:        "localhost:2379",
				EtcdPrefix:         tt.inputPrefix,
				ManagementInterval: tt.inputManagementInterval,
			}

			gateway, err := NewGateway(config)
			if err != nil {
				t.Fatalf("NewGateway() error = %v", err)
			}
			defer gateway.Stop()

			if gateway.config.EtcdPrefix != tt.expectedPrefix {
				t.Fatalf("Expected EtcdPrefix=%s, got %s", tt.expectedPrefix, gateway.config.EtcdPrefix)
			}
			if gateway.config.ManagementInterval != tt.expectedManagementInterval {
				t.Fatalf("Expected ManagementInterval=%v, got %v", tt.expectedManagementInterval, gateway.config.ManagementInterval)
			}
		})
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
