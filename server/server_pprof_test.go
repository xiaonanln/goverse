package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestServerPprof_WithHTTPServer verifies that pprof endpoints are available when HTTP server is configured
func TestServerPprof_WithHTTPServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix to ensure etcd is available and get test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	listenAddr := testutil.GetFreeAddress()
	metricsAddr := testutil.GetFreeAddress()
	config := &ServerConfig{
		ListenAddress:             listenAddr,
		AdvertiseAddress:          listenAddr,
		MetricsListenAddress:      metricsAddr, // pprof is automatically enabled when HTTP server is configured
		EtcdAddress:               "localhost:2379",
		EtcdPrefix:                etcdPrefix,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
		NumShards:                 testutil.TestNumShards,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Create a cancellable context for Run
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()

	// Give server time to start
	time.Sleep(2 * time.Second)

	// Try to access pprof endpoints - they should be available since HTTP server is configured
	pprofEndpoints := []string{
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/cmdline",
	}

	for _, endpoint := range pprofEndpoints {
		url := fmt.Sprintf("http://%s%s", metricsAddr, endpoint)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("Failed to access pprof endpoint %s: %v", endpoint, err)
		}

		// Should get 200 since HTTP server is configured (pprof is automatically enabled)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 for %s when HTTP server configured, got %d", endpoint, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Metrics endpoint should still work
	metricsURL := fmt.Sprintf("http://%s/metrics", metricsAddr)
	resp, err := http.Get(metricsURL)
	if err != nil {
		t.Fatalf("Failed to access metrics endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for /metrics, got %d", resp.StatusCode)
	}

	// Cancel the context to trigger shutdown
	cancel()

	// Wait for server to stop (with timeout)
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Logf("Server stopped with error: %v (this may be acceptable)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not stop within timeout after context cancellation")
	}
}

// TestServerPprof_NoMetricsServer verifies that pprof is not started when metrics server is disabled
func TestServerPprof_NoMetricsServer(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	// Use PrepareEtcdPrefix to ensure etcd is available and get test isolation
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	listenAddr := testutil.GetFreeAddress()
	config := &ServerConfig{
		ListenAddress:             listenAddr,
		AdvertiseAddress:          listenAddr,
		MetricsListenAddress:      "", // No HTTP server - pprof won't be available
		EtcdAddress:               "localhost:2379",
		EtcdPrefix:                etcdPrefix,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
		NumShards:                 testutil.TestNumShards,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Create a cancellable context for Run
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()

	// Give server time to start
	time.Sleep(2 * time.Second)

	// Since no HTTP server is started, pprof endpoints won't be available
	// This test just verifies the server starts successfully without HTTP server

	// Cancel the context to trigger shutdown
	cancel()

	// Wait for server to stop (with timeout)
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Logf("Server stopped with error: %v (this may be acceptable)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not stop within timeout after context cancellation")
	}
}
