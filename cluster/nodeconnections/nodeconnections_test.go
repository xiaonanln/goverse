package nodeconnections

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	nc := New()

	if nc == nil {
		t.Fatal("New() should not return nil")
	}

	if nc.connections == nil {
		t.Fatal("NodeConnections connections map should be initialized")
	}

	if nc.logger == nil {
		t.Fatal("NodeConnections logger should be initialized")
	}
}

func TestNodeConnections_StartStop(t *testing.T) {
	nc := New()

	ctx := context.Background()

	// Start NodeConnections
	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}

	// Verify context is set
	if nc.ctx == nil {
		t.Fatal("NodeConnections context should be set after Start()")
	}

	// Stop NodeConnections
	nc.Stop()

	// Verify connections are closed
	if nc.NumConnections() != 0 {
		t.Fatalf("Expected 0 connections after Stop(), got %d", nc.NumConnections())
	}
}

func TestNodeConnections_StartTwice(t *testing.T) {
	nc := New()
	ctx := context.Background()

	// Start first time
	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}

	// Start second time - should not error
	err = nc.Start(ctx)
	if err != nil {
		t.Fatalf("Starting NodeConnections twice should not error: %v", err)
	}

	nc.Stop()
}

func TestNodeConnections_NumConnections(t *testing.T) {
	nc := New()

	// Initially should have 0 connections
	if nc.NumConnections() != 0 {
		t.Fatalf("Expected 0 connections initially, got %d", nc.NumConnections())
	}
}

func TestNodeConnections_GetConnection_NotExists(t *testing.T) {
	nc := New()

	_, err := nc.GetConnection("localhost:50000")
	if err == nil {
		t.Fatal("GetConnection should return error for non-existent connection")
	}
}

func TestNodeConnections_GetAllConnections(t *testing.T) {
	nc := New()

	connections := nc.GetAllConnections()
	if connections == nil {
		t.Fatal("GetAllConnections should not return nil")
	}

	if len(connections) != 0 {
		t.Fatalf("Expected 0 connections initially, got %d", len(connections))
	}
}

func TestNodeConnections_ConnectDisconnect(t *testing.T) {
	nc := New()

	// Note: We can't test actual connection establishment without a running server
	// We test the connection management logic

	// Test disconnect from non-existent node
	err := nc.disconnectFromNode("localhost:60000")
	if err != nil {
		t.Fatalf("Disconnecting from non-existent node should not error: %v", err)
	}
}

func TestNodeConnections_SetNodes(t *testing.T) {
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	// Initially, we should have 0 connections
	if nc.NumConnections() != 0 {
		t.Fatalf("Expected 0 connections initially, got %d", nc.NumConnections())
	}

	// Test that SetNodes processes node list correctly
	// Note: Actual connection attempts will fail without running servers, which is expected in unit tests
	// The caller (cluster) should exclude this node's address from the list
	nodes := []string{"localhost:50001", "localhost:50002"}

	nc.SetNodes(nodes)

	// The method should handle the node list without error
	// We can't verify actual connections without running servers
	// But we can verify it doesn't panic or error
}

func TestNodeConnections_RetryOnFailedConnection(t *testing.T) {
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	// Note: grpc.NewClient doesn't fail immediately for non-existent servers
	// It only fails when actual RPCs are made. The connection is created optimistically.
	// This test verifies that the connection manager handles the case where
	// grpc.NewClient succeeds (which is the normal case).
	nodes := []string{"localhost:59999"}
	nc.SetNodes(nodes)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Connection should be created (grpc.NewClient succeeds even without server)
	connCount := nc.NumConnections()
	if connCount == 0 {
		t.Fatal("Expected connection to be created (grpc.NewClient succeeds optimistically)")
	}

	// No retry should be running since grpc.NewClient succeeded
	retryCount := nc.NumActiveRetries()

	if retryCount != 0 {
		t.Fatalf("Expected 0 retry goroutines when grpc.NewClient succeeds, got %d", retryCount)
	}
}

func TestNodeConnections_RetryStopsOnDisconnect(t *testing.T) {
	// This test validates the cleanup logic when a node is removed
	// Even though grpc.NewClient typically succeeds, this tests the
	// cleanup path for any retry goroutines that might be running
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	nodes := []string{"localhost:59998"}
	nc.SetNodes(nodes)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Connection should exist
	initialCount := nc.NumConnections()
	if initialCount == 0 {
		t.Fatal("Expected connection to be created")
	}

	// Now remove the node
	nc.SetNodes([]string{})

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Connection should be removed
	finalCount := nc.NumConnections()
	if finalCount != 0 {
		t.Fatalf("Expected 0 connections after node removal, got %d", finalCount)
	}

	// No retry goroutines should be running
	retryCount := nc.NumActiveRetries()

	if retryCount != 0 {
		t.Fatalf("Expected 0 retry goroutines after node removal, got %d", retryCount)
	}
}

func TestNodeConnections_RetryStopsOnStop(t *testing.T) {
	// This test validates that Stop() properly cleans up all retry goroutines
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}

	nodes := []string{"localhost:59997"}
	nc.SetNodes(nodes)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Stop the manager
	nc.Stop()

	// Verify all retry goroutines stopped and cleanup happened
	retryCountAfterStop := nc.NumActiveRetries()

	if retryCountAfterStop != 0 {
		t.Fatalf("Expected 0 retry goroutines after Stop(), got %d", retryCountAfterStop)
	}

	// Verify connections are closed
	if nc.NumConnections() != 0 {
		t.Fatalf("Expected 0 connections after Stop(), got %d", nc.NumConnections())
	}
}

func TestNodeConnections_NoRetryForAlreadyConnected(t *testing.T) {
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	// First attempt - will succeed (grpc.NewClient doesn't fail immediately)
	nodes := []string{"localhost:59996"}
	nc.SetNodes(nodes)

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// If connection was established, no retry should be running
	// (Note: grpc.NewClient succeeds even if server isn't running)
	retryCount := nc.NumActiveRetries()

	// Connection should exist (even if server isn't actually running)
	connCount := nc.NumConnections()
	if connCount == 0 {
		t.Fatal("Expected connection to be created (grpc.NewClient succeeds even without server)")
	}

	// No retry should be active if connection was created
	if retryCount != 0 {
		t.Fatalf("Expected 0 retry goroutines when connection succeeds, got %d", retryCount)
	}
}

func TestNodeConnections_ExponentialBackoffConstants(t *testing.T) {
	// Verify the backoff constants are set to reasonable values
	if initialRetryDelay != 1*time.Second {
		t.Errorf("Expected initialRetryDelay to be 1s, got %v", initialRetryDelay)
	}

	if maxRetryDelay != 30*time.Second {
		t.Errorf("Expected maxRetryDelay to be 30s, got %v", maxRetryDelay)
	}
}

func TestNodeConnections_SetNodesDoesNotStartRetryForExistingConnection(t *testing.T) {
	nc := New()

	ctx := context.Background()

	err := nc.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start NodeConnections: %v", err)
	}
	defer nc.Stop()

	nodes := []string{"localhost:59995"}
	nc.SetNodes(nodes)

	// Give it a moment to establish connection
	time.Sleep(100 * time.Millisecond)

	// Verify connection exists
	if nc.NumConnections() != 1 {
		t.Fatalf("Expected 1 connection, got %d", nc.NumConnections())
	}

	// Call SetNodes again with the same node
	nc.SetNodes(nodes)

	// Give it a moment
	time.Sleep(100 * time.Millisecond)

	// Should still have exactly one connection, not a duplicate
	if nc.NumConnections() != 1 {
		t.Fatalf("Expected 1 connection after calling SetNodes twice, got %d", nc.NumConnections())
	}

	// No retry goroutines should be running
	retryCount := nc.NumActiveRetries()

	if retryCount != 0 {
		t.Fatalf("Expected 0 retry goroutines for existing connection, got %d", retryCount)
	}
}
