package testutil_test

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

// Example_dynamicPortsSingleServer demonstrates using dynamic port allocation
// for a single test server
func Example_dynamicPortsSingleServer() {
	// This example can be run as a test: go test -v -run Example_dynamicPortsSingleServer

	// Get a free address
	addr, err := testutil.GetFreeAddress()
	if err != nil {
		panic(err)
	}

	// addr will be something like "localhost:45678"
	_ = addr
}

// Example_dynamicPortsMultipleServers demonstrates using dynamic port allocation
// for multiple test servers that need to run concurrently
func Example_dynamicPortsMultipleServers() {
	// Get free addresses for multiple servers
	addr1, _ := testutil.GetFreeAddress()
	addr2, _ := testutil.GetFreeAddress()
	addr3, _ := testutil.GetFreeAddress()

	// All addresses will be unique and available
	_, _, _ = addr1, addr2, addr3
}

// Example_testServerHelper demonstrates using TestServerHelper with dynamic ports
func Example_testServerHelper() {
	// Create a mock server
	mockServer := testutil.NewMockGoverseServer()

	// Create TestServerHelper with dynamic port allocation (localhost:0)
	testServer := testutil.NewTestServerHelper("localhost:0", mockServer)

	ctx := context.Background()
	_ = testServer.Start(ctx)
	defer testServer.Stop()

	// Get the actual bound address
	actualAddr := testServer.GetAddress()
	_ = actualAddr // Will be something like "127.0.0.1:45678"
}

// TestDynamicPortsParallelExecution demonstrates that tests with dynamic ports
// can run in parallel without conflicts
func TestDynamicPortsParallelExecution(t *testing.T) {
	// Run sub-tests in parallel
	t.Run("Node1", func(t *testing.T) {
		t.Parallel()

		addr, err := testutil.GetFreeAddress()
		if err != nil {
			t.Fatalf("Failed to get address: %v", err)
		}

		// Create mock server with dynamic port
		mockServer := testutil.NewMockGoverseServer()
		testServer := testutil.NewTestServerHelper("localhost:0", mockServer)

		ctx := context.Background()
		err = testServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start server: %v", err)
		}
		defer testServer.Stop()

		t.Logf("Node1 using advertise address: %s, listening on: %s", addr, testServer.GetAddress())
	})

	t.Run("Node2", func(t *testing.T) {
		t.Parallel()

		addr, err := testutil.GetFreeAddress()
		if err != nil {
			t.Fatalf("Failed to get address: %v", err)
		}

		// Create mock server with dynamic port
		mockServer := testutil.NewMockGoverseServer()
		testServer := testutil.NewTestServerHelper("localhost:0", mockServer)

		ctx := context.Background()
		err = testServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start server: %v", err)
		}
		defer testServer.Stop()

		t.Logf("Node2 using advertise address: %s, listening on: %s", addr, testServer.GetAddress())
	})

	t.Run("Node3", func(t *testing.T) {
		t.Parallel()

		addr, err := testutil.GetFreeAddress()
		if err != nil {
			t.Fatalf("Failed to get address: %v", err)
		}

		// Create mock server with dynamic port
		mockServer := testutil.NewMockGoverseServer()
		testServer := testutil.NewTestServerHelper("localhost:0", mockServer)

		ctx := context.Background()
		err = testServer.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start server: %v", err)
		}
		defer testServer.Stop()

		t.Logf("Node3 using advertise address: %s, listening on: %s", addr, testServer.GetAddress())
	})
}
