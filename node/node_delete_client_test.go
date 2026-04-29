package node

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/config"
)

// TestNode_DeleteClientObject_UsesRealTypeForCheck closes the spoof
// gap codex flagged on PR #552: a client could claim an allowed type
// (e.g. "TempSession") while supplying an id that resolves to a
// protected object (e.g. a "TestNonPersistentObject" instance with
// the default INTERNAL DELETE rule). Because cluster.DeleteObject
// routes by id alone, the gate's claimed-type authorization was
// vacuous. This test pins the fix: when the call is from a client,
// node.DeleteClientObject authorizes against CheckClientDelete using
// the object's *real* type as fetched from the node's registry — so
// even with no operator rule, the default DELETE=INTERNAL semantics
// kick in and the spoofed delete is rejected.
func TestNode_DeleteClientObject_UsesRealTypeForCheck(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	// Operator has a permissive rule for some other type (the type
	// the attacker claims) but the protected type has no rule, so it
	// gets the default DELETE=INTERNAL — clients can't delete it but
	// nodes can.
	rules := []config.LifecycleRule{
		{Type: "TempSession", Lifecycle: "DELETE", Access: "ALLOW"},
	}
	validator, err := config.NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("NewLifecycleValidator: %v", err)
	}
	node.SetLifecycleValidator(validator)

	ctx := context.Background()

	if err := node.createObject(ctx, "TestNonPersistentObject", "protected-1"); err != nil {
		t.Fatalf("createObject: %v", err)
	}

	// Spoofed client delete: the gate's vacuous "claimed-type"
	// authorization would have let this through. The node's
	// real-type CheckClientDelete must catch it.
	err = node.DeleteClientObject(ctx, "protected-1")
	if err == nil {
		t.Fatalf("DeleteClientObject succeeded against TestNonPersistentObject under default DELETE=INTERNAL; want delete denied")
	}
	if !strings.Contains(err.Error(), "delete denied") {
		t.Fatalf("DeleteClientObject error = %v; want \"delete denied\"", err)
	}
	if node.NumObjects() != 1 {
		t.Fatalf("object should still exist after rejected client delete; NumObjects=%d", node.NumObjects())
	}

	// Same id via the node-internal entry point: CheckNodeDelete
	// passes (default INTERNAL allows nodes), so the object is
	// removed.
	if err := node.DeleteObject(ctx, "protected-1"); err != nil {
		t.Fatalf("internal DeleteObject failed: %v", err)
	}
	if node.NumObjects() != 0 {
		t.Fatalf("object should be gone after internal delete; NumObjects=%d", node.NumObjects())
	}
}
