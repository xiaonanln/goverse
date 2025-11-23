package gateserver

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gate_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNewGatewayServer(t *testing.T) {
	tests := []struct {
		name       string
		config     *GatewayServerConfig
		wantErr    bool
		errContain string
	}{
		{
			name: "valid config",
			config: &GatewayServerConfig{
				ListenAddress:    ":49001",
				AdvertiseAddress: "localhost:49001",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/test-gateway",
			},
			wantErr: false,
		},
		{
			name: "valid config with default prefix",
			config: &GatewayServerConfig{
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
			config: &GatewayServerConfig{
				ListenAddress:    "",
				AdvertiseAddress: "localhost:49003",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "ListenAddress cannot be empty",
		},
		{
			name: "empty advertise address",
			config: &GatewayServerConfig{
				ListenAddress:    ":49003",
				AdvertiseAddress: "",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GatewayServerConfig{
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
			server, err := NewGatewayServer(tt.config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewGatewayServer() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errContain != "" {
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("NewGatewayServer() error = %v, want error containing %q", err, tt.errContain)
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

func TestGatewayServerStartStop(t *testing.T) {
	listenAddr := testutil.GetFreeAddress()
	config := &GatewayServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-lifecycle",
	}

	server, err := NewGatewayServer(config)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
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
	conn, err := grpc.NewClient(listenAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gateway server: %v", err)
	}
	defer conn.Close()

	// Create client
	client := gate_pb.NewGateServiceClient(conn)
	if client == nil {
		t.Fatalf("Failed to create gateway client")
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

func TestGatewayServerMultipleStops(t *testing.T) {
	listenAddr := testutil.GetFreeAddress()
	config := &GatewayServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-multistop",
	}

	server, err := NewGatewayServer(config)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
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

func TestGatewayServerRPCMethods(t *testing.T) {
	listenAddr := testutil.GetFreeAddress()
	config := &GatewayServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-rpc",
	}

	server, err := NewGatewayServer(config)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}
	defer server.Stop()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Connect to server
	conn, err := grpc.NewClient(listenAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gateway server: %v", err)
	}
	defer conn.Close()

	client := gate_pb.NewGateServiceClient(conn)

	t.Run("CreateObject", func(t *testing.T) {
		req := &gate_pb.CreateObjectRequest{
			Type: "TestObject",
			Id:   "test-object-1",
		}

		// This will return "not implemented" error from the gateway
		resp, err := client.CreateObject(context.Background(), req)
		// We expect an error since gateway methods are not fully implemented yet
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

		// This will return "not implemented" error from the gateway
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

		// This will return "not implemented" error from the gateway
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

		// This will return "not implemented" error from the gateway
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
		config     *GatewayServerConfig
		wantErr    bool
		errContain string
		checkFunc  func(*testing.T, *GatewayServerConfig)
	}{
		{
			name: "valid config",
			config: &GatewayServerConfig{
				ListenAddress:    ":49000",
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
				EtcdPrefix:       "/custom",
			},
			wantErr: false,
		},
		{
			name: "sets default prefix",
			config: &GatewayServerConfig{
				ListenAddress:    ":49000",
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *GatewayServerConfig) {
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
			config: &GatewayServerConfig{
				AdvertiseAddress: "localhost:49000",
				EtcdAddress:      "localhost:2379",
			},
			wantErr:    true,
			errContain: "ListenAddress cannot be empty",
		},
		{
			name: "empty advertise address",
			config: &GatewayServerConfig{
				ListenAddress: ":49000",
				EtcdAddress:   "localhost:2379",
			},
			wantErr:    true,
			errContain: "AdvertiseAddress cannot be empty",
		},
		{
			name: "empty etcd address",
			config: &GatewayServerConfig{
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
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("validateConfig() error = %v, want error containing %q", err, tt.errContain)
				}
			}
			if err == nil && tt.checkFunc != nil {
				tt.checkFunc(t, tt.config)
			}
		})
	}
}

func TestGatewayServerGracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	listenAddr := testutil.GetFreeAddress()
	config := &GatewayServerConfig{
		ListenAddress:    listenAddr,
		AdvertiseAddress: listenAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-graceful",
	}

	server, err := NewGatewayServer(config)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	// Create a client connection
	conn, err := grpc.NewClient(listenAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gateway server: %v", err)
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

func TestGatewayServerMetrics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	config := &GatewayServerConfig{
		ListenAddress:        ":49014",
		AdvertiseAddress:     "localhost:49014",
		MetricsListenAddress: ":19014",
		EtcdAddress:          "localhost:2379",
		EtcdPrefix:           "/test-gateway-metrics",
	}

	server, err := NewGatewayServer(config)
	if err != nil {
		t.Fatalf("Failed to create gateway server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start server
	go server.Start(ctx)

	// Try to fetch metrics with retries - the metrics server starts quickly
	var resp *http.Response
	maxRetries := 20
	for i := 0; i < maxRetries; i++ {
		resp, err = http.Get("http://localhost:19014/metrics")
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Failed to fetch metrics after retries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Read a bit of the response to verify it's prometheus metrics
	buf := make([]byte, 100)
	n, err := resp.Body.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Failed to read metrics response: %v", err)
	}
	if n == 0 {
		t.Fatal("Expected metrics response body, got empty")
	}

	// Check for typical prometheus metrics format
	metricsContent := string(buf[:n])
	if !strings.Contains(metricsContent, "go_") && !strings.Contains(metricsContent, "promhttp_") {
		t.Fatalf("Expected prometheus metrics format, got: %s", metricsContent)
	}

	t.Log("Successfully fetched metrics from HTTP endpoint")

	// Stop server
	if err := server.Stop(); err != nil {
		t.Fatalf("Server.Stop() returned error: %v", err)
	}
}
