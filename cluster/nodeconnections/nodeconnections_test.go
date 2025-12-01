package nodeconnections

import (
	"context"
	"sort"
	"testing"
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

func TestNodeConnections_GetConnectedNodeAddresses(t *testing.T) {
	nc := New()

	// Initially should return empty slice
	addrs := nc.GetConnectedNodeAddresses()
	if addrs == nil {
		t.Fatal("GetConnectedNodeAddresses should not return nil")
	}
	if len(addrs) != 0 {
		t.Fatalf("Expected 0 addresses initially, got %d", len(addrs))
	}

	// Manually add some connections to test (normally done via SetNodes with running servers)
	// We'll test by directly manipulating the connections map
	nc.connectionsMu.Lock()
	nc.connections["localhost:50001"] = &NodeConnection{address: "localhost:50001"}
	nc.connections["localhost:50002"] = &NodeConnection{address: "localhost:50002"}
	nc.connectionsMu.Unlock()

	// Get connected addresses
	addrs = nc.GetConnectedNodeAddresses()
	if len(addrs) != 2 {
		t.Fatalf("Expected 2 addresses, got %d", len(addrs))
	}

	// Sort for deterministic comparison
	sort.Strings(addrs)
	if addrs[0] != "localhost:50001" || addrs[1] != "localhost:50002" {
		t.Fatalf("Unexpected addresses: %v", addrs)
	}
}
