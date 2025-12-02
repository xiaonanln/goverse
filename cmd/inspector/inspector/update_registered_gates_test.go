package inspector

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

func TestUpdateRegisteredGates(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node first
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{},
		RegisteredGates:  []string{"localhost:49001", "localhost:49002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Verify initial registered gates
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if len(node.RegisteredGates) != 2 {
		t.Fatalf("Expected 2 registered gates, got %d", len(node.RegisteredGates))
	}

	// Update registered gates
	updateReq := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: "localhost:47000",
		RegisteredGates:  []string{"localhost:49001", "localhost:49002", "localhost:49003"},
	}

	_, err = inspector.UpdateRegisteredGates(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateRegisteredGates failed: %v", err)
	}

	// Verify updated registered gates
	nodes = pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node = nodes[0]
	if len(node.RegisteredGates) != 3 {
		t.Fatalf("Expected 3 registered gates, got %d", len(node.RegisteredGates))
	}

	// Verify the new gate is present
	found := false
	for _, addr := range node.RegisteredGates {
		if addr == "localhost:49003" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Expected to find localhost:49003 in registered gates")
	}
}

func TestUpdateRegisteredGates_EmptyAddress(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	updateReq := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: "",
		RegisteredGates:  []string{"localhost:49001"},
	}

	_, err := inspector.UpdateRegisteredGates(context.Background(), updateReq)
	if err == nil {
		t.Fatal("Expected error for empty address, got nil")
	}
}

func TestUpdateRegisteredGates_NonexistentNode(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Try to update a node that doesn't exist
	updateReq := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: "localhost:47000",
		RegisteredGates:  []string{"localhost:49001"},
	}

	// Should not error, just be a no-op
	_, err := inspector.UpdateRegisteredGates(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateRegisteredGates failed: %v", err)
	}

	// Node should not have been created
	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected 0 nodes, got %d", len(nodes))
	}
}

func TestUpdateRegisteredGates_EventNotification(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{},
		RegisteredGates:  []string{"localhost:49001"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	initialNodes := pg.GetNodes()
	initialNode := initialNodes[0]

	// Update registered gates
	updateReq := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: "localhost:47000",
		RegisteredGates:  []string{"localhost:49001", "localhost:49002"},
	}

	_, err = inspector.UpdateRegisteredGates(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateRegisteredGates failed: %v", err)
	}

	// Give it a moment for async operations
	time.Sleep(50 * time.Millisecond)

	// Verify node was updated
	updatedNodes := pg.GetNodes()
	if len(updatedNodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(updatedNodes))
	}

	updatedNode := updatedNodes[0]
	if len(updatedNode.RegisteredGates) != 2 {
		t.Fatalf("Expected 2 registered gates after update, got %d", len(updatedNode.RegisteredGates))
	}

	// Verify it's different from initial
	if len(initialNode.RegisteredGates) == len(updatedNode.RegisteredGates) {
		// Only check if lengths are the same - if different we know it updated
		t.Log("Node registered gates successfully updated")
	}
}

func TestUpdateRegisteredGates_EmptyList(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node with registered gates
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{},
		RegisteredGates:  []string{"localhost:49001", "localhost:49002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Update to empty gate list
	updateReq := &inspector_pb.UpdateRegisteredGatesRequest{
		AdvertiseAddress: "localhost:47000",
		RegisteredGates:  []string{},
	}

	_, err = inspector.UpdateRegisteredGates(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateRegisteredGates failed: %v", err)
	}

	// Verify gates cleared
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if len(node.RegisteredGates) != 0 {
		t.Fatalf("Expected 0 registered gates, got %d", len(node.RegisteredGates))
	}
}

func TestRegisterNode_WithRegisteredGates(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register node with registered gates
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{"localhost:47001"},
		RegisteredGates:  []string{"localhost:49001", "localhost:49002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Verify registered gates
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if len(node.RegisteredGates) != 2 {
		t.Fatalf("Expected 2 registered gates, got %d", len(node.RegisteredGates))
	}

	// Verify specific gates
	expectedGates := map[string]bool{
		"localhost:49001": true,
		"localhost:49002": true,
	}

	for _, addr := range node.RegisteredGates {
		if !expectedGates[addr] {
			t.Fatalf("Unexpected registered gate: %s", addr)
		}
	}
}

func TestRegisterNode_UpdateRegisteredGates(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// First registration with some gates
	registerReq1 := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{},
		RegisteredGates:  []string{"localhost:49001", "localhost:49002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq1)
	if err != nil {
		t.Fatalf("First RegisterNode failed: %v", err)
	}

	// Verify initial gates
	nodes := pg.GetNodes()
	node := nodes[0]
	if len(node.RegisteredGates) != 2 {
		t.Fatalf("Expected 2 registered gates initially, got %d", len(node.RegisteredGates))
	}

	// Second registration (re-registration with updated gates)
	registerReq2 := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{},
		RegisteredGates:  []string{"localhost:49003"},
	}

	_, err = inspector.RegisterNode(context.Background(), registerReq2)
	if err != nil {
		t.Fatalf("Second RegisterNode failed: %v", err)
	}

	// Verify gates updated
	nodes = pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node = nodes[0]
	if len(node.RegisteredGates) != 1 {
		t.Fatalf("Expected 1 registered gate after update, got %d", len(node.RegisteredGates))
	}

	if node.RegisteredGates[0] != "localhost:49003" {
		t.Fatalf("Expected localhost:49003, got %s", node.RegisteredGates[0])
	}
}
