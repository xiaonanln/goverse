package inspector

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

// TestRemoveObject tests the RemoveObject RPC handler
func TestRemoveObject(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	// Register a node first
	node := models.GoverseNode{
		ID:            "localhost:47000",
		AdvertiseAddr: "localhost:47000",
	}
	pg.AddOrUpdateNode(node)

	// Add an object
	obj := models.GoverseObject{
		ID:            "test-obj-1",
		GoverseNodeID: "localhost:47000",
	}
	pg.AddOrUpdateObject(obj)

	// Verify object exists
	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object before removal, got %d", len(objects))
	}

	// Remove the object
	ctx := context.Background()
	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    "test-obj-1",
		NodeAddress: "localhost:47000",
	}

	_, err := insp.RemoveObject(ctx, req)
	if err != nil {
		t.Fatalf("RemoveObject failed: %v", err)
	}

	// Verify object was removed
	objects = pg.GetObjects()
	if len(objects) != 0 {
		t.Fatalf("Expected 0 objects after removal, got %d", len(objects))
	}
}

// TestRemoveObject_EmptyObjectID tests removing with empty object ID
func TestRemoveObject_EmptyObjectID(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	// Register a node
	node := models.GoverseNode{
		ID:            "localhost:47000",
		AdvertiseAddr: "localhost:47000",
	}
	pg.AddOrUpdateNode(node)

	ctx := context.Background()
	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    "",
		NodeAddress: "localhost:47000",
	}

	// Should not fail, just return empty
	_, err := insp.RemoveObject(ctx, req)
	if err != nil {
		t.Fatalf("RemoveObject with empty ID should not fail: %v", err)
	}
}

// TestRemoveObject_NodeNotRegistered tests removing object from unregistered node
func TestRemoveObject_NodeNotRegistered(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()
	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    "test-obj-1",
		NodeAddress: "localhost:47000",
	}

	// Should fail with NotFound error
	_, err := insp.RemoveObject(ctx, req)
	if err == nil {
		t.Fatal("RemoveObject should fail when node is not registered")
	}
}

// TestRemoveObject_NonExistentObject tests removing non-existent object
func TestRemoveObject_NonExistentObject(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	// Register a node
	node := models.GoverseNode{
		ID:            "localhost:47000",
		AdvertiseAddr: "localhost:47000",
	}
	pg.AddOrUpdateNode(node)

	ctx := context.Background()
	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    "non-existent-obj",
		NodeAddress: "localhost:47000",
	}

	// Should not fail even if object doesn't exist
	_, err := insp.RemoveObject(ctx, req)
	if err != nil {
		t.Fatalf("RemoveObject should succeed even for non-existent object: %v", err)
	}
}

// TestRemoveObject_MultipleObjects tests removing one object from many
func TestRemoveObject_MultipleObjects(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	// Register a node
	node := models.GoverseNode{
		ID:            "localhost:47000",
		AdvertiseAddr: "localhost:47000",
	}
	pg.AddOrUpdateNode(node)

	// Add multiple objects
	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "localhost:47000"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "localhost:47000"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "localhost:47000"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	// Remove obj2
	ctx := context.Background()
	req := &inspector_pb.RemoveObjectRequest{
		ObjectId:    "obj2",
		NodeAddress: "localhost:47000",
	}

	_, err := insp.RemoveObject(ctx, req)
	if err != nil {
		t.Fatalf("RemoveObject failed: %v", err)
	}

	// Verify only obj2 was removed
	objects := pg.GetObjects()
	if len(objects) != 2 {
		t.Fatalf("Expected 2 objects after removal, got %d", len(objects))
	}

	objIDs := make(map[string]bool)
	for _, obj := range objects {
		objIDs[obj.ID] = true
	}

	if !objIDs["obj1"] {
		t.Fatal("obj1 should still exist")
	}

	if objIDs["obj2"] {
		t.Fatal("obj2 should have been removed")
	}

	if !objIDs["obj3"] {
		t.Fatal("obj3 should still exist")
	}
}

// TestRegisterGate tests gate registration with inspector
func TestRegisterGate(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()
	req := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
	}

	_, err := insp.RegisterGate(ctx, req)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	// Verify gate was registered
	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	if gates[0].ID != "localhost:49000" {
		t.Fatalf("Expected gate ID 'localhost:49000', got '%s'", gates[0].ID)
	}

	if gates[0].Type != "goverse_gate" {
		t.Fatalf("Expected gate type 'goverse_gate', got '%s'", gates[0].Type)
	}
}

// TestRegisterGate_EmptyAddress tests registering gate with empty address
func TestRegisterGate_EmptyAddress(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()
	req := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "",
	}

	_, err := insp.RegisterGate(ctx, req)
	if err == nil {
		t.Fatal("RegisterGate should fail with empty address")
	}
}

// TestUnregisterGate tests gate unregistration
func TestUnregisterGate(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()

	// First register the gate
	regReq := &inspector_pb.RegisterGateRequest{
		AdvertiseAddress: "localhost:49000",
	}
	_, err := insp.RegisterGate(ctx, regReq)
	if err != nil {
		t.Fatalf("RegisterGate failed: %v", err)
	}

	// Verify gate was registered
	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	// Now unregister the gate
	unregReq := &inspector_pb.UnregisterGateRequest{
		AdvertiseAddress: "localhost:49000",
	}
	_, err = insp.UnregisterGate(ctx, unregReq)
	if err != nil {
		t.Fatalf("UnregisterGate failed: %v", err)
	}

	// Verify gate was removed
	gates = pg.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates after unregister, got %d", len(gates))
	}
}

// TestUnregisterGate_NonExistent tests unregistering non-existent gate
func TestUnregisterGate_NonExistent(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()
	req := &inspector_pb.UnregisterGateRequest{
		AdvertiseAddress: "localhost:49000",
	}

	// Should not fail even if gate doesn't exist
	_, err := insp.UnregisterGate(ctx, req)
	if err != nil {
		t.Fatalf("UnregisterGate should succeed even for non-existent gate: %v", err)
	}
}

// TestMultipleGates tests registering multiple gates
func TestMultipleGates(t *testing.T) {
	pg := graph.NewGoverseGraph(0)
	insp := New(pg)

	ctx := context.Background()

	// Register multiple gates
	for _, addr := range []string{"localhost:49000", "localhost:49001", "localhost:49002"} {
		req := &inspector_pb.RegisterGateRequest{
			AdvertiseAddress: addr,
		}
		_, err := insp.RegisterGate(ctx, req)
		if err != nil {
			t.Fatalf("RegisterGate failed for %s: %v", addr, err)
		}
	}

	// Verify all gates were registered
	gates := pg.GetGates()
	if len(gates) != 3 {
		t.Fatalf("Expected 3 gates, got %d", len(gates))
	}

	// Unregister one gate
	unregReq := &inspector_pb.UnregisterGateRequest{
		AdvertiseAddress: "localhost:49001",
	}
	_, err := insp.UnregisterGate(ctx, unregReq)
	if err != nil {
		t.Fatalf("UnregisterGate failed: %v", err)
	}

	// Verify only 2 gates remain
	gates = pg.GetGates()
	if len(gates) != 2 {
		t.Fatalf("Expected 2 gates after unregister, got %d", len(gates))
	}

	// Verify the correct gates remain
	gateIDs := make(map[string]bool)
	for _, gate := range gates {
		gateIDs[gate.ID] = true
	}
	if !gateIDs["localhost:49000"] {
		t.Fatal("Gate localhost:49000 should still exist")
	}
	if gateIDs["localhost:49001"] {
		t.Fatal("Gate localhost:49001 should have been removed")
	}
	if !gateIDs["localhost:49002"] {
		t.Fatal("Gate localhost:49002 should still exist")
	}
}
