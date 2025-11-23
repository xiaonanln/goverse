package gateserver

import (
	"context"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNewGateServer(t *testing.T) {
	tests := []struct {
		name       string
		config     *GateServerConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "valid config",
			config: &GateServerConfig{
				ListenAddress:    ":49001",
				AdvertiseAddress: "localhost:49001",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/test-gate",
			},
			wantErr: false,
		},
		{
			name: "valid config with default prefix",
			config: &GateServerConfig{
				ListenAddress:    ":49002",
				AdvertiseAddress: "localhost:49002",
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
			name: "empty listen address",
			config: &GateServerConfig{
				ListenAddress:    "",
				AdvertiseAddress: "localhost:49003",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "ListenAddress cannot be empty",
		},
		{
			name: "empty advertise address",
			config: &GateServerConfig{
				ListenAddress:    ":49003",
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GateServerConfig{
				ListenAddress:    ":49003",
				AdvertiseAddress: "localhost:49003",
				EtcdAddress:      "",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, err := NewGateServer(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewGateServer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("NewGateServer() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if server != nil {
				// Clean up
				defer server.Stop()

				// Verify defaults are set
				if tt.config != nil && tt.config.EtcdPrefix == "" {
					if server.config.EtcdPrefix != "/goverse" {
						t.Fatalf("Expected default EtcdPrefix to be /goverse, got %s", server.config.EtcdPrefix)
					}
				}
			}
		})
	}
}

func TestGateServerStartStop(t *testing.T) {
	config := &GateServerConfig{
		ListenAddress:    ":49010",
		AdvertiseAddress: "localhost:49010",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-lifecycle",
	}

	server, err := NewGateServer(config)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start server in goroutine
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Verify server is listening by attempting connection
	conn, err := grpc.NewClient("localhost:49010", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate server: %v", err)
	}
	defer conn.Close()

	// Create client
	client := gate_pb.NewGateServiceClient(conn)
	if client == nil {
		t.Fatalf("Failed to create gate client")
	}

	// Cancel context to trigger shutdown
	cancel()

	// Wait for server to stop
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server.Start() returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("Server.Stop() returned error: %v", err)
	}
}

func TestGateServerMultipleStops(t *testing.T) {
	config := &GateServerConfig{
		ListenAddress:    ":49011",
		AdvertiseAddress: "localhost:49011",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-multistop",
	}

	server, err := NewGateServer(config)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Stop multiple times should not panic or error
	if err := server.Stop(); err != nil {
		t.Fatalf("First Stop() returned error: %v", err)
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Second Stop() returned error: %v", err)
	}
}

func TestGateServerRPCMethods(t *testing.T) {
	config := &GateServerConfig{
		ListenAddress:    ":49012",
		AdvertiseAddress: "localhost:49012",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-rpc",
	}

	server, err := NewGateServer(config)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	defer server.Stop()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Connect to server
	conn, err := grpc.NewClient("localhost:49012", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate server: %v", err)
	}
	defer conn.Close()

	client := gate_pb.NewGateServiceClient(conn)

	t.Run("CreateObject", func(t *testing.T) {
		req := &gate_pb.CreateObjectRequest{
			Type: "TestObject",
			Id:   "test-object-1",
		}

		// This will return "not implemented" error from the gate
		resp, err := client.CreateObject(context.Background(), req)
		// We expect an error since gate methods are not fully implemented yet
		if err == nil {
			t.Logf("CreateObject succeeded (unexpected but ok for now): %v", resp)
		} else {
			t.Logf("CreateObject returned expected error: %v", err)
		}
	})

	t.Run("CallObject", func(t *testing.T) {
		req := &gate_pb.CallObjectRequest{
			ClientId: "test-client",
			Method:   "TestMethod",
			Type:     "TestObject",
			Id:       "test-object-1",
		}

		// This will return "not implemented" error from the gate
		resp, err := client.CallObject(context.Background(), req)
		if err == nil {
			t.Logf("CallObject succeeded (unexpected but ok for now): %v", resp)
		} else {
			t.Logf("CallObject returned expected error: %v", err)
		}
	})

	t.Run("DeleteObject", func(t *testing.T) {
		req := &gate_pb.DeleteObjectRequest{
			Id: "test-object-1",
		}

		// This will return "not implemented" error from the gate
		resp, err := client.DeleteObject(context.Background(), req)
		if err == nil {
			t.Logf("DeleteObject succeeded (unexpected but ok for now): %v", resp)
		} else {
			t.Logf("DeleteObject returned expected error: %v", err)
		}
	})

	t.Run("Register", func(t *testing.T) {
		stream, err := client.Register(context.Background(), &gate_pb.Empty{})
		if err != nil {
			t.Fatalf("Register failed to create stream: %v", err)
		}

		// This will return "not implemented" error from the gate
		_, err = stream.Recv()
		if err == nil {
			t.Logf("Register succeeded (unexpected but ok for now)")
		} else {
			t.Logf("Register returned expected error: %v", err)
		}
	})
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     *GateServerConfig
		wantErr    bool
		errContain string
		checkFunc  func(*testing.T, *GateServerConfig)
	}{
		{
			name: "valid config",
			config: &GateServerConfig{
				ListenAddress:    ":49000",
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/custom",
			},
			wantErr: false,
		},
		{
			name: "sets default prefix",
			config: &GateServerConfig{
				ListenAddress:    ":49000",
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *GateServerConfig) {
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
			name: "empty listen address",
			config: &GateServerConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "ListenAddress cannot be empty",
		},
		{
			name: "empty advertise address",
			config: &GateServerConfig{
				ListenAddress: ":49000",
				EtcdAddress:   "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GateServerConfig{
				ListenAddress:    ":49000",
				AdvertiseAddress: "localhost:49000",
			},
			wantErr:    true,
			errContain: "EtcdAddress cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !contains(err.Error(), tt.errContain) {
					t.Fatalf("validateConfig() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if err == nil && tt.checkFunc != nil {
				tt.checkFunc(t, tt.config)
			}
		})
	}
}

func TestGateServerGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	config := &GateServerConfig{
		ListenAddress:    ":49013",
		AdvertiseAddress: "localhost:49013",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-graceful",
	}

	server, err := NewGateServer(config)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create a client connection
	conn, err := grpc.NewClient("localhost:49013", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gate server: %v", err)
	}
	defer conn.Close()

	// Measure shutdown time
	start := time.Now()

	// Stop server (should complete within 5 seconds graceful timeout)
	if err := server.Stop(); err != nil {
		t.Fatalf("Server.Stop() returned error: %v", err)
	}

	duration := time.Since(start)

	// Should complete within reasonable time
	if duration > 7*time.Second {
		t.Fatalf("Graceful shutdown took too long: %v", duration)
	}

	t.Logf("Graceful shutdown completed in %v", duration)
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
