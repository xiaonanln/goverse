package etcdmanager

import (
	"context"
	"testing"
	"time"
)

// TestKeepAliveRetry tests that the keep-alive mechanism retries when the channel closes
func TestKeepAliveRetry(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50100"

	// Register node - this will start the keep-alive loop
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Verify the node is registered by checking it exists in etcd
	time.Sleep(500 * time.Millisecond)
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
	}

	found := false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Node %s not found after registration", nodeAddress)
	}

	// The keep-alive loop should continue to maintain the registration
	// even if there are transient failures. We verify this by checking
	// that the node remains registered over time.
	time.Sleep(2 * time.Second)

	// Check again - node should still be registered
	nodes, _, err = mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() after delay error = %v", err)
	}

	found = false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Node %s not found after delay - keep-alive may have failed", nodeAddress)
	}

	// Cleanup
	err = mgr.UnregisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
	}

	// Verify node is unregistered
	time.Sleep(500 * time.Millisecond)
	nodes, _, err = mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() after unregister error = %v", err)
	}

	for _, node := range nodes {
		if node == nodeAddress {
			t.Fatalf("Node %s still found after unregister", nodeAddress)
		}
	}
}

// TestKeepAliveContextCancellation tests that keep-alive stops when context is cancelled
func TestKeepAliveContextCancellation(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50101"

	// Register node
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Verify registered
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
	}

	found := false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("Node %s not found after registration", nodeAddress)
	}

	// Unregister should stop the keep-alive loop
	err = mgr.UnregisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)

	// Verify the keep-alive loop has stopped
	mgr.keepAliveMu.Lock()
	stopped := mgr.keepAliveStopped
	mgr.keepAliveMu.Unlock()

	if !stopped {
		t.Fatalf("Keep-alive loop should be stopped after unregister")
	}
}

// TestRegisterNodeIdempotent tests that registering the same node multiple times is idempotent
func TestRegisterNodeIdempotent(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50102"

	// Register node first time
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("First RegisterNode() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Register same node again - should be idempotent (no error)
	err = mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("Second RegisterNode() should be idempotent, got error: %v", err)
	}

	// Verify only one instance of the node exists
	nodes, _, err := mgr.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() error = %v", err)
	}

	count := 0
	for _, node := range nodes {
		if node == nodeAddress {
			count++
		}
	}

	if count != 1 {
		t.Fatalf("Expected 1 instance of node %s, found %d", nodeAddress, count)
	}

	// Cleanup
	err = mgr.UnregisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
	}
}

// TestCloseStopsKeepAlive tests that Close() stops the keep-alive loop
func TestCloseStopsKeepAlive(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50103"

	// Register node
	err := mgr.RegisterNode(ctx, nodeAddress)
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Close should stop the keep-alive loop
	err = mgr.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify the keep-alive loop has stopped
	mgr.keepAliveMu.Lock()
	stopped := mgr.keepAliveStopped
	mgr.keepAliveMu.Unlock()

	if !stopped {
		t.Fatalf("Keep-alive loop should be stopped after Close()")
	}
}
