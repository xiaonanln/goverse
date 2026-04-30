package node

import (
	"context"
	"strings"
	"testing"
)

// TestNode_DeleteObject_RejectsTypeSpoof closes the spoof gap codex
// flagged on PR #552: a client could claim an allowed type (e.g.
// "TempSession") while supplying an id that resolves to a protected
// object (e.g. a "TestNonPersistentObject" instance). Because
// cluster.DeleteObject routes by id alone, the gate's claimed-type
// authorization was vacuous on its own.
//
// Fix: the gate forwards the claimed type to the receiving node;
// node.DeleteObject verifies it matches the object's real type from
// the registry. This test pins that mismatch is rejected and the
// object survives.
func TestNode_DeleteObject_RejectsTypeSpoof(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	ctx := context.Background()

	if err := node.createObject(ctx, "TestNonPersistentObject", "protected-1"); err != nil {
		t.Fatalf("createObject: %v", err)
	}

	// Spoofed claim: real type is TestNonPersistentObject, claim
	// "TempSession". Must be rejected.
	err := node.DeleteObject(ctx, "TempSession", "protected-1")
	if err == nil {
		t.Fatalf("DeleteObject succeeded with spoofed type; want type mismatch")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("DeleteObject error = %v; want \"type mismatch\"", err)
	}
	if node.NumObjects() != 1 {
		t.Fatalf("object should still exist after rejected spoof; NumObjects=%d", node.NumObjects())
	}

	// Honest claim with the real type goes through.
	if err := node.DeleteObject(ctx, "TestNonPersistentObject", "protected-1"); err != nil {
		t.Fatalf("DeleteObject with honest type failed: %v", err)
	}
	if node.NumObjects() != 0 {
		t.Fatalf("object should be gone after honest delete; NumObjects=%d", node.NumObjects())
	}
}

// TestNode_DeleteObject_RequiresType pins that node.DeleteObject is
// always called with a non-empty type — it's a programming-contract
// error to omit it (the gate / cluster layers always have type by
// the time they reach here).
func TestNode_DeleteObject_RequiresType(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	ctx := context.Background()

	err := node.DeleteObject(ctx, "", "anything")
	if err == nil {
		t.Fatalf("DeleteObject with empty type succeeded; want error")
	}
	if !strings.Contains(err.Error(), "non-empty type") {
		t.Fatalf("error = %v; want \"non-empty type\"", err)
	}
}
