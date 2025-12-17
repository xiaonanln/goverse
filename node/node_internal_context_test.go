package node

import (
	"context"
	"testing"
	"time"
)

// TestNode_ContextCreatedInStart verifies that the node's context is created during Start
func TestNode_ContextCreatedInStart(t *testing.T) {
	node := NewNode("localhost:50000", testNumShards)

	// Context should be nil before Start
	if node.Context() != nil {
		t.Fatal("Node context should be nil before Start()")
	}

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Context should not be nil after Start
	nodeCtx := node.Context()
	if nodeCtx == nil {
		t.Fatal("Node context should not be nil after Start()")
	}

	// Context should not be cancelled yet
	select {
	case <-nodeCtx.Done():
		t.Fatal("Node context should not be cancelled after Start()")
	default:
		// Good - context is not cancelled
	}
}

// TestNode_ContextCancelledInStop verifies that the node's context is cancelled during Stop
func TestNode_ContextCancelledInStop(t *testing.T) {
	node := NewNode("localhost:50001", testNumShards)

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	nodeCtx := node.Context()
	if nodeCtx == nil {
		t.Fatal("Node context should not be nil after Start()")
	}

	// Verify context is not cancelled before Stop
	select {
	case <-nodeCtx.Done():
		t.Fatal("Node context should not be cancelled before Stop()")
	default:
		// Good - context is not cancelled
	}

	// Stop the node
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Verify context is now cancelled
	select {
	case <-nodeCtx.Done():
		// Good - context is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Node context should be cancelled after Stop()")
	}
}

// TestNode_ContextInheritsCancellation verifies that the node's context respects parent context cancellation
func TestNode_ContextInheritsCancellation(t *testing.T) {
	node := NewNode("localhost:50002", testNumShards)

	// Create a cancellable parent context
	parentCtx, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	err := node.Start(parentCtx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(context.Background())

	nodeCtx := node.Context()
	if nodeCtx == nil {
		t.Fatal("Node context should not be nil after Start()")
	}

	// Verify context is not cancelled yet
	select {
	case <-nodeCtx.Done():
		t.Fatal("Node context should not be cancelled initially")
	default:
		// Good
	}

	// Cancel the parent context
	cancelParent()

	// Verify node's context is also cancelled (inherits from parent)
	select {
	case <-nodeCtx.Done():
		// Good - context is cancelled when parent is cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Node context should be cancelled when parent context is cancelled")
	}
}

// TestNode_StopWithoutStart verifies that Stop works even if Start was never called
func TestNode_StopWithoutStart(t *testing.T) {
	node := NewNode("localhost:50003", testNumShards)

	// Context should be nil before Start
	if node.Context() != nil {
		t.Fatal("Node context should be nil before Start()")
	}

	// Stop should not panic even if Start was never called
	ctx := context.Background()
	err := node.Stop(ctx)
	if err != nil {
		t.Fatalf("Stop should succeed even without Start: %v", err)
	}
}

// TestNode_MultipleStopCalls verifies that Stop is idempotent
func TestNode_MultipleStopCalls(t *testing.T) {
	node := NewNode("localhost:50004", testNumShards)

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	nodeCtx := node.Context()
	if nodeCtx == nil {
		t.Fatal("Node context should not be nil after Start()")
	}

	// First Stop
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("First Stop should succeed: %v", err)
	}

	// Verify context is cancelled
	select {
	case <-nodeCtx.Done():
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Node context should be cancelled after Stop()")
	}

	// Second Stop should also succeed (idempotent)
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Second Stop should succeed (idempotent): %v", err)
	}

	// Context should still be cancelled
	select {
	case <-nodeCtx.Done():
		// Good
	default:
		t.Fatal("Node context should remain cancelled")
	}
}
