package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestServerHealthz tests that the /healthz endpoint returns OK when the server is running
func TestServerHealthz(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	listenAddr := testutil.GetFreeAddress()
	metricsAddr := testutil.GetFreeAddress()
	config := &ServerConfig{
		ListenAddress:             listenAddr,
		AdvertiseAddress:          listenAddr,
		MetricsListenAddress:      metricsAddr,
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()

	// Give server time to start the HTTP listener
	time.Sleep(2 * time.Second)

	// Test /healthz endpoint
	url := fmt.Sprintf("http://%s/healthz", metricsAddr)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Failed to access /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for /healthz, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if result["status"] != "ok" {
		t.Fatalf("Expected status 'ok', got '%s'", result["status"])
	}

	cancel()
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Logf("Server stopped with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}
}

// TestServerReady tests that the /ready endpoint reflects cluster readiness
func TestServerReady(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	listenAddr := testutil.GetFreeAddress()
	metricsAddr := testutil.GetFreeAddress()
	config := &ServerConfig{
		ListenAddress:             listenAddr,
		AdvertiseAddress:          listenAddr,
		MetricsListenAddress:      metricsAddr,
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()

	// Give server time to start the HTTP listener
	time.Sleep(2 * time.Second)

	readyURL := fmt.Sprintf("http://%s/ready", metricsAddr)

	// Wait for the cluster to become ready (poll the /ready endpoint)
	var lastStatus int
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(readyURL)
		if err != nil {
			t.Fatalf("Failed to access /ready: %v", err)
		}
		lastStatus = resp.StatusCode
		resp.Body.Close()

		if lastStatus == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if lastStatus != http.StatusOK {
		t.Fatalf("Expected /ready to return 200 after cluster is ready, got %d", lastStatus)
	}

	// Verify the response body
	resp, err := http.Get(readyURL)
	if err != nil {
		t.Fatalf("Failed to access /ready: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	if result["status"] != "ready" {
		t.Fatalf("Expected status 'ready', got '%s'", result["status"])
	}

	cancel()
	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Logf("Server stopped with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Server did not stop within timeout")
	}
}
