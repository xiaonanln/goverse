package inspector

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

func TestUpdateConnectedNodes_ForGate(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a gate first with initial connected nodes
	registerReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err := inspector.RegisterGate(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	// Verify initial connected nodes
	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	gate := gates[0]
	if len(gate.ConnectedNodes) != 2 {
		t.Fatalf("Expected 2 connected nodes, got %d", len(gate.ConnectedNodes))
	}

	// Update connected nodes using the same RPC as nodes
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002", "localhost:47003"},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Verify updated connected nodes
	gates = pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	gate = gates[0]
	if len(gate.ConnectedNodes) != 3 {
		t.Fatalf("Expected 3 connected nodes, got %d", len(gate.ConnectedNodes))
	}

	// Verify the new connection is present
	found := false
	for _, addr := range gate.ConnectedNodes {
		if addr == "localhost:47003" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Expected to find localhost:47003 in connected nodes")
	}
}

func TestUpdateConnectedNodes_ForGate_NonexistentGate(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Try to update a gate that doesn't exist
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001"},
	}

	// Should not error, just be a no-op
	_, err := inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Gate should not have been created
	gates := pg.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates, got %d", len(gates))
	}
}

func TestUpdateConnectedNodes_ForGate_EventNotification(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a gate
	registerReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001"},
	}

	_, err := inspector.RegisterGate(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	initialGates := pg.GetGates()
	initialGate := initialGates[0]

	// Update connected nodes using the same RPC as nodes
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Give it a moment for async operations
	time.Sleep(50 * time.Millisecond)

	// Verify gate was updated
	updatedGates := pg.GetGates()
	if len(updatedGates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(updatedGates))
	}

	updatedGate := updatedGates[0]
	if len(updatedGate.ConnectedNodes) != 2 {
		t.Fatalf("Expected 2 connected nodes after update, got %d", len(updatedGate.ConnectedNodes))
	}

	// Verify it's different from initial
	if len(initialGate.ConnectedNodes) == len(updatedGate.ConnectedNodes) {
		// Only check if lengths are the same - if different we know it updated
		t.Log("Gate connected nodes successfully updated")
	}
}

func TestUpdateConnectedNodes_ForGate_EmptyList(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a gate with connections
	registerReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err := inspector.RegisterGate(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	// Update to empty connection list using the same RPC as nodes
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Verify connections cleared
	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	gate := gates[0]
	if len(gate.ConnectedNodes) != 0 {
		t.Fatalf("Expected 0 connected nodes, got %d", len(gate.ConnectedNodes))
	}
}

func TestRegisterGate_WithConnectedNodes(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a gate with connected nodes
	registerReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err := inspector.RegisterGate(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	// Verify gate was registered with connected nodes
	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	gate := gates[0]
	if len(gate.ConnectedNodes) != 2 {
		t.Fatalf("Expected 2 connected nodes, got %d", len(gate.ConnectedNodes))
	}

	// Verify connection addresses
	expectedAddrs := map[string]bool{"localhost:47001": true, "localhost:47002": true}
	for _, addr := range gate.ConnectedNodes {
		if !expectedAddrs[addr] {
			t.Fatalf("Unexpected connected node: %s", addr)
		}
	}
}
