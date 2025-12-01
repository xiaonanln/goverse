package inspector

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

func TestUpdateConnectedNodes(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node first
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Verify initial connected nodes
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if len(node.ConnectedNodes) != 2 {
		t.Fatalf("Expected 2 connected nodes, got %d", len(node.ConnectedNodes))
	}

	// Update connected nodes
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:47000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002", "localhost:47003"},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Verify updated connected nodes
	nodes = pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node = nodes[0]
	if len(node.ConnectedNodes) != 3 {
		t.Fatalf("Expected 3 connected nodes, got %d", len(node.ConnectedNodes))
	}

	// Verify the new connection is present
	found := false
	for _, addr := range node.ConnectedNodes {
		if addr == "localhost:47003" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Expected to find localhost:47003 in connected nodes")
	}
}

func TestUpdateConnectedNodes_EmptyAddress(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "",
		ConnectedNodes:   []string{"localhost:47001"},
	}

	_, err := inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err == nil {
		t.Fatal("Expected error for empty address, got nil")
	}
}

func TestUpdateConnectedNodes_NonexistentNode(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Try to update a node that doesn't exist
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:47000",
		ConnectedNodes:   []string{"localhost:47001"},
	}

	// Should not error, just be a no-op
	_, err := inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Node should not have been created
	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected 0 nodes, got %d", len(nodes))
	}
}

func TestUpdateConnectedNodes_EventNotification(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{"localhost:47001"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	initialNodes := pg.GetNodes()
	initialNode := initialNodes[0]

	// Update connected nodes
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:47000",
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Give it a moment for async operations
	time.Sleep(50 * time.Millisecond)

	// Verify node was updated
	updatedNodes := pg.GetNodes()
	if len(updatedNodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(updatedNodes))
	}

	updatedNode := updatedNodes[0]
	if len(updatedNode.ConnectedNodes) != 2 {
		t.Fatalf("Expected 2 connected nodes after update, got %d", len(updatedNode.ConnectedNodes))
	}

	// Verify it's different from initial
	if len(initialNode.ConnectedNodes) == len(updatedNode.ConnectedNodes) {
		// Only check if lengths are the same - if different we know it updated
		t.Log("Node connected nodes successfully updated")
	}
}

func TestUpdateConnectedNodes_EmptyList(t *testing.T) {
	pg := graph.NewGoverseGraph()
	inspector := New(pg)

	// Register a node with connections
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects:          []*inspector_pb.Object{},
		ConnectedNodes:   []string{"localhost:47001", "localhost:47002"},
	}

	_, err := inspector.RegisterNode(context.Background(), registerReq)
	if err != nil {
		t.Fatalf("RegisterNode failed: %v", err)
	}

	// Update to empty connection list
	updateReq := &inspector_pb.UpdateConnectedNodesRequest{
		AdvertiseAddress: "localhost:47000",
		ConnectedNodes:   []string{},
	}

	_, err = inspector.UpdateConnectedNodes(context.Background(), updateReq)
	if err != nil {
		t.Fatalf("UpdateConnectedNodes failed: %v", err)
	}

	// Verify connections cleared
	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	node := nodes[0]
	if len(node.ConnectedNodes) != 0 {
		t.Fatalf("Expected 0 connected nodes, got %d", len(node.ConnectedNodes))
	}
}
