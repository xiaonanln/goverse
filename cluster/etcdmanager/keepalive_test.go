package etcdmanager

import (
	"context"
	"testing"
	"time"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestKeepAliveRetry tests that the keep-alive mechanism retries when the channel closes
func TestKeepAliveRetry(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := addr

	// Register node using shared lease - this will start the keep-alive loop
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddress
	_, err := mgr.RegisterKeyLease(ctx, key, nodeAddress, NodeLeaseTTL)
	if err != nil {
		t.Fatalf("RegisterKeyLease() error = %v", err)
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
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Fatalf("UnregisterKeyLease() error = %v", err)
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
func TestKeepAliveContextCancellation(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := addr

	// Register node using shared lease
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddress
	_, err := mgr.RegisterKeyLease(ctx, key, nodeAddress, NodeLeaseTTL)
	if err != nil {
		t.Fatalf("RegisterKeyLease() error = %v", err)
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
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Fatalf("UnregisterKeyLease() error = %v", err)
	}

	// Wait for cleanup
	time.Sleep(500 * time.Millisecond)

	// Verify the shared lease loop has stopped (since we unregistered the only key)
	mgr.sharedKeysMu.Lock()
	running := mgr.sharedLeaseRunning
	mgr.sharedKeysMu.Unlock()

	if running {
		t.Fatalf("Shared lease loop should be stopped after unregister")
	}
}

// TestRegisterKeyLeaseIdempotent tests that registering the same key multiple times overwrites the value
func TestRegisterKeyLeaseIdempotent(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := addr

	// Register node first time
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddress
	_, err := mgr.RegisterKeyLease(ctx, key, nodeAddress, NodeLeaseTTL)
	if err != nil {
		t.Fatalf("First RegisterKeyLease() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Register same key again with different value - should overwrite
	_, err = mgr.RegisterKeyLease(ctx, key, nodeAddress+"-updated", NodeLeaseTTL)
	if err != nil {
		t.Fatalf("Second RegisterKeyLease() error: %v", err)
	}

	// Wait for update
	time.Sleep(500 * time.Millisecond)

	// Verify the value is updated
	resp, err := mgr.GetClient().Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatalf("Key not found after update")
	}
	if string(resp.Kvs[0].Value) != nodeAddress+"-updated" {
		t.Fatalf("Expected value %s, got %s", nodeAddress+"-updated", string(resp.Kvs[0].Value))
	}

	// Cleanup
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Fatalf("UnregisterKeyLease() error = %v", err)
	}
}

// TestCloseStopsSharedLease tests that Close() stops the shared lease loop
func TestCloseStopsSharedLease(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := addr

	// Register node using shared lease
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddress
	_, err := mgr.RegisterKeyLease(ctx, key, nodeAddress, NodeLeaseTTL)
	if err != nil {
		t.Fatalf("RegisterKeyLease() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Close should stop the shared lease loop
	err = mgr.Close()
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify the shared lease loop has stopped
	mgr.sharedKeysMu.Lock()
	running := mgr.sharedLeaseRunning
	mgr.sharedKeysMu.Unlock()

	if running {
		t.Fatalf("Shared lease loop should be stopped after Close()")
	}
}
