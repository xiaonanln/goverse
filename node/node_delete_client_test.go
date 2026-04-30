package node

import (
	"context"
	"strings"
	"testing"
)

// TestNode_DeleteClientObject_RejectsTypeSpoof closes the spoof gap
// codex flagged on PR #552: a client could claim an allowed type
// (e.g. "TempSession") while supplying an id that resolves to a
// protected object (e.g. a "TestNonPersistentObject" instance).
// Because cluster.DeleteObject routes by id alone, the gate's
// claimed-type authorization was vacuous on its own.
//
// Fix: the gate forwards the claimed type to the receiving node;
// node.DeleteClientObject verifies it matches the object's real
// type from the registry. This test pins that mismatch is rejected
// and the object survives.
func TestNode_DeleteClientObject_RejectsTypeSpoof(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	ctx := context.Background()

	if err := node.createObject(ctx, "TestNonPersistentObject", "protected-1"); err != nil {
		t.Fatalf("createObject: %v", err)
	}

	// Spoofed claim: real type is TestNonPersistentObject, client
	// claims TempSession. Must be rejected.
	err := node.DeleteClientObject(ctx, "TempSession", "protected-1")
	if err == nil {
		t.Fatalf("DeleteClientObject succeeded with spoofed type; want type mismatch")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("DeleteClientObject error = %v; want \"type mismatch\"", err)
	}
	if node.NumObjects() != 1 {
		t.Fatalf("object should still exist after rejected spoof; NumObjects=%d", node.NumObjects())
	}

	// Honest claim with the real type goes through.
	if err := node.DeleteClientObject(ctx, "TestNonPersistentObject", "protected-1"); err != nil {
		t.Fatalf("DeleteClientObject with honest type failed: %v", err)
	}
	if node.NumObjects() != 0 {
		t.Fatalf("object should be gone after honest delete; NumObjects=%d", node.NumObjects())
	}
}

// TestNode_DeleteClientObject_RequiresClaimedType pins that calling
// DeleteClientObject without a claimed type is a programming error —
// the gate-originated path always carries the claimed type, so an
// empty claim signals a caller bug rather than a node-internal
// delete (which uses DeleteObject instead).
func TestNode_DeleteClientObject_RequiresClaimedType(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	ctx := context.Background()

	err := node.DeleteClientObject(ctx, "", "anything")
	if err == nil {
		t.Fatalf("DeleteClientObject with empty type succeeded; want error")
	}
	if !strings.Contains(err.Error(), "non-empty claimed type") {
		t.Fatalf("error = %v; want \"non-empty claimed type\"", err)
	}
}
