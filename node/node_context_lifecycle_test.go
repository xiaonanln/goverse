package node

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
)

// TestObjectWithContext is a test object that uses its context
type TestObjectWithContext struct {
	object.BaseObject
}

func (t *TestObjectWithContext) OnCreated() {
	// Simple test implementation
}

// TestDeleteObject_CancelsObjectContext verifies that DeleteObject cancels the object's lifetime context
func TestDeleteObject_CancelsObjectContext(t *testing.T) {
	node := NewNode("localhost:47000", testNumShards)
	node.RegisterObjectType((*TestObjectWithContext)(nil))

	ctx := context.Background()
	objID := "context-test-obj"

	// Create object
	_, err := node.CreateObject(ctx, "TestObjectWithContext", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get the object to access its context
	node.objectsMu.RLock()
	obj := node.objects[objID]
	node.objectsMu.RUnlock()

	if obj == nil {
		t.Fatal("Object should exist after creation")
	}

	// Get the object's context
	baseObj, ok := obj.(*TestObjectWithContext)
	if !ok {
		t.Fatal("Object should be of type TestObjectWithContext")
	}
	objCtx := baseObj.Context()

	// Verify context is not cancelled before deletion
	select {
	case <-objCtx.Done():
		t.Fatal("Object context should not be cancelled before deletion")
	default:
		// Good - context is not cancelled
	}

	// Delete the object
	err = node.DeleteObject(ctx, objID)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Verify context is now cancelled
	select {
	case <-objCtx.Done():
		// Good - context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Object context should be cancelled after deletion")
	}

	// Verify object is removed
	node.objectsMu.RLock()
	_, exists := node.objects[objID]
	node.objectsMu.RUnlock()

	if exists {
		t.Fatal("Object should not exist after deletion")
	}
}

// TestDeleteObject_CancelsContext_MultipleObjects verifies that each object has its own independent context
func TestDeleteObject_CancelsContext_MultipleObjects(t *testing.T) {
	node := NewNode("localhost:47001", testNumShards)
	node.RegisterObjectType((*TestObjectWithContext)(nil))

	ctx := context.Background()

	// Create multiple objects
	obj1ID := "context-multi-obj-1"
	obj2ID := "context-multi-obj-2"
	obj3ID := "context-multi-obj-3"

	_, err := node.CreateObject(ctx, "TestObjectWithContext", obj1ID)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}

	_, err = node.CreateObject(ctx, "TestObjectWithContext", obj2ID)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	_, err = node.CreateObject(ctx, "TestObjectWithContext", obj3ID)
	if err != nil {
		t.Fatalf("Failed to create object 3: %v", err)
	}

	// Get all object contexts
	node.objectsMu.RLock()
	obj1 := node.objects[obj1ID].(*TestObjectWithContext)
	obj2 := node.objects[obj2ID].(*TestObjectWithContext)
	obj3 := node.objects[obj3ID].(*TestObjectWithContext)
	node.objectsMu.RUnlock()

	ctx1 := obj1.Context()
	ctx2 := obj2.Context()
	ctx3 := obj3.Context()

	// Delete object 2
	err = node.DeleteObject(ctx, obj2ID)
	if err != nil {
		t.Fatalf("Failed to delete object 2: %v", err)
	}

	// Verify only object 2's context is cancelled
	select {
	case <-ctx2.Done():
		// Good - object 2's context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Object 2's context should be cancelled after deletion")
	}

	// Verify object 1 and 3 contexts are NOT cancelled
	select {
	case <-ctx1.Done():
		t.Fatal("Object 1's context should not be cancelled")
	default:
		// Good
	}

	select {
	case <-ctx3.Done():
		t.Fatal("Object 3's context should not be cancelled")
	default:
		// Good
	}

	// Delete object 1
	err = node.DeleteObject(ctx, obj1ID)
	if err != nil {
		t.Fatalf("Failed to delete object 1: %v", err)
	}

	// Verify object 1's context is now cancelled
	select {
	case <-ctx1.Done():
		// Good - object 1's context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Object 1's context should be cancelled after deletion")
	}

	// Verify object 3's context is still not cancelled
	select {
	case <-ctx3.Done():
		t.Fatal("Object 3's context should not be cancelled")
	default:
		// Good
	}
}

// TestStop_CancelsAllObjectContexts verifies that Stop cancels contexts for all objects
func TestStop_CancelsAllObjectContexts(t *testing.T) {
	node := NewNode("localhost:47002", testNumShards)
	node.RegisterObjectType((*TestObjectWithContext)(nil))

	ctx := context.Background()

	// Create multiple objects
	objIDs := []string{"stop-ctx-1", "stop-ctx-2", "stop-ctx-3"}
	objContexts := make([]context.Context, len(objIDs))

	for i, objID := range objIDs {
		_, err := node.CreateObject(ctx, "TestObjectWithContext", objID)
		if err != nil {
			t.Fatalf("Failed to create object %s: %v", objID, err)
		}

		// Get object context
		node.objectsMu.RLock()
		obj := node.objects[objID].(*TestObjectWithContext)
		node.objectsMu.RUnlock()
		objContexts[i] = obj.Context()
	}

	// Stop the node
	err := node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Verify all object contexts are cancelled
	for i, objCtx := range objContexts {
		select {
		case <-objCtx.Done():
			// Good - context is cancelled
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Object %s context should be cancelled after Stop", objIDs[i])
		}
	}
}

// TestDeleteObject_IdempotentCancellation verifies that deleting an already-deleted object doesn't cause issues
func TestDeleteObject_IdempotentCancellation(t *testing.T) {
	node := NewNode("localhost:47003", testNumShards)
	node.RegisterObjectType((*TestObjectWithContext)(nil))

	ctx := context.Background()
	objID := "idempotent-cancel-obj"

	// Create object
	_, err := node.CreateObject(ctx, "TestObjectWithContext", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get the object's context
	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectWithContext)
	node.objectsMu.RUnlock()
	objCtx := obj.Context()

	// Delete the object
	err = node.DeleteObject(ctx, objID)
	if err != nil {
		t.Fatalf("Failed to delete object: %v", err)
	}

	// Verify context is cancelled
	select {
	case <-objCtx.Done():
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context should be cancelled after first deletion")
	}

	// Delete again (idempotent) - should not panic or error
	err = node.DeleteObject(ctx, objID)
	if err != nil {
		t.Fatalf("Second delete should succeed (idempotent): %v", err)
	}

	// Context should still be cancelled
	select {
	case <-objCtx.Done():
		// Good
	default:
		t.Fatal("Context should remain cancelled")
	}
}
