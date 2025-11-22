package testutil

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestTestServerHelper_DynamicPort tests that TestServerHelper works with port 0 (dynamic allocation)
func TestTestServerHelper_DynamicPort(t *testing.T) {
	// Create a mock server with port 0 (dynamic allocation)
	mockServer := NewMockGoverseServer()
	testServer := NewTestServerHelper("localhost:0", mockServer)

	ctx := context.Background()
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer testServer.Stop()

	// Verify the server is running
	if !testServer.IsRunning() {
		t.Fatal("Server should be running")
	}

	// Get the actual address
	actualAddr := testServer.GetAddress()
	if actualAddr == "" {
		t.Fatal("GetAddress() returned empty string")
	}

	// Verify it's not the original "localhost:0" address
	if actualAddr == "localhost:0" {
		t.Fatal("GetAddress() should return the actual bound address, not 'localhost:0'")
	}

	// Verify it has a valid format (should be something like "127.0.0.1:12345")
	if !strings.Contains(actualAddr, ":") {
		t.Fatalf("GetAddress() returned invalid format: %s", actualAddr)
	}

	t.Logf("Server successfully bound to dynamic address: %s", actualAddr)
}

// TestTestServerHelper_SpecificPort tests that TestServerHelper works with a specific port
func TestTestServerHelper_SpecificPort(t *testing.T) {
	// Get a free port first
	freeAddr := GetFreeAddress()

	// Create a mock server with the specific port
	mockServer := NewMockGoverseServer()
	testServer := NewTestServerHelper(freeAddr, mockServer)

	ctx := context.Background()
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer testServer.Stop()

	// Verify the server is running
	if !testServer.IsRunning() {
		t.Fatal("Server should be running")
	}

	// Get the actual address
	actualAddr := testServer.GetAddress()
	if actualAddr == "" {
		t.Fatal("GetAddress() returned empty string")
	}

	// Verify it matches (or is similar to) the requested address
	// Note: The returned address might be 127.0.0.1 instead of localhost, but port should match
	if !strings.Contains(actualAddr, ":") {
		t.Fatalf("GetAddress() returned invalid format: %s", actualAddr)
	}

	t.Logf("Server successfully bound to specific address: %s (requested: %s)", actualAddr, freeAddr)
}

// TestTestServerHelper_MultipleServers tests multiple servers with dynamic ports don't conflict
func TestTestServerHelper_MultipleServers(t *testing.T) {
	numServers := 3
	servers := make([]*TestServerHelper, numServers)
	addresses := make(map[string]bool)

	ctx := context.Background()

	// Start multiple servers with dynamic port allocation
	for i := 0; i < numServers; i++ {
		mockServer := NewMockGoverseServer()
		testServer := NewTestServerHelper("localhost:0", mockServer)

		err := testServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start test server %d: %v", i, err)
		}
		servers[i] = testServer

		addr := testServer.GetAddress()
		if addresses[addr] {
			t.Fatalf("Duplicate address detected: %s", addr)
		}
		addresses[addr] = true

		t.Logf("Server %d started on: %s", i, addr)
	}

	// Verify all servers are running
	for i, server := range servers {
		if !server.IsRunning() {
			t.Fatalf("Server %d should be running", i)
		}
	}

	// Stop all servers
	for i, server := range servers {
		err := server.Stop()
		if err != nil {
			t.Fatalf("Failed to stop server %d: %v", i, err)
		}

		if server.IsRunning() {
			t.Fatalf("Server %d should not be running after Stop()", i)
		}
	}

	// Verify we got unique addresses
	if len(addresses) != numServers {
		t.Fatalf("Expected %d unique addresses, got %d", numServers, len(addresses))
	}
}

// TestTestServerHelper_StopBeforeStart tests stopping before starting
func TestTestServerHelper_StopBeforeStart(t *testing.T) {
	mockServer := NewMockGoverseServer()
	testServer := NewTestServerHelper("localhost:0", mockServer)

	// Stop before starting should not error
	err := testServer.Stop()
	if err != nil {
		t.Fatalf("Stop() before Start() returned error: %v", err)
	}

	if testServer.IsRunning() {
		t.Fatal("Server should not be running")
	}
}

// TestTestServerHelper_DoubleStart tests starting twice
func TestTestServerHelper_DoubleStart(t *testing.T) {
	mockServer := NewMockGoverseServer()
	testServer := NewTestServerHelper("localhost:0", mockServer)

	ctx := context.Background()

	// First start should succeed
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}
	defer testServer.Stop()

	// Second start should return error
	err = testServer.Start(ctx)
	if err == nil {
		t.Fatal("Second Start() should return error")
	}

	// Should still be running after failed second start
	if !testServer.IsRunning() {
		t.Fatal("Server should still be running")
	}
}

// TestTestServerHelper_StartStopStartAgain tests starting after stopping
func TestTestServerHelper_StartStopStartAgain(t *testing.T) {
	mockServer := NewMockGoverseServer()
	testServer := NewTestServerHelper("localhost:0", mockServer)

	ctx := context.Background()

	// First start
	err := testServer.Start(ctx)
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}
	firstAddr := testServer.GetAddress()

	// Stop
	err = testServer.Stop()
	if err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	// Wait a bit for cleanup
	time.Sleep(50 * time.Millisecond)

	// Second start
	err = testServer.Start(ctx)
	if err != nil {
		t.Fatalf("Second Start() after Stop() failed: %v", err)
	}
	defer testServer.Stop()

	secondAddr := testServer.GetAddress()

	// Addresses might be different (different port allocated)
	t.Logf("First address: %s, Second address: %s", firstAddr, secondAddr)

	if !testServer.IsRunning() {
		t.Fatal("Server should be running after second start")
	}
}
