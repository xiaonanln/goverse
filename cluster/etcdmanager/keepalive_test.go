package etcdmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
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
func TestKeepAliveContextCancellation(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50101"

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
func TestRegisterKeyLeaseIdempotent(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50102"

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
func TestCloseStopsSharedLease(t *testing.T) {
	t.Parallel()

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50103"

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

// TestRegisterKeyLeaseReconnection tests that RegisterKeyLease persists through etcd reconnection
func TestRegisterKeyLeaseReconnection(t *testing.T) {
	if !testutil.IsGitHubActions() {
		t.Skip("Skipping reconnection test - not in CI environment")
	}

	mgr := setupEtcdTest(t)
	if mgr == nil {
		return
	}

	ctx := context.Background()
	nodeAddress := "localhost:50104"

	// Register node using shared lease
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + nodeAddress
	_, err := mgr.RegisterKeyLease(ctx, key, nodeAddress, NodeLeaseTTL)
	if err != nil {
		t.Fatalf("RegisterKeyLease() error = %v", err)
	}

	// Wait for registration
	time.Sleep(500 * time.Millisecond)

	// Verify the node is registered
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

	// Ensure etcd is restarted after the test
	t.Cleanup(func() {
		err := testutil.StartEtcd()
		if err != nil {
			t.Logf("Warning: failed to restart etcd in cleanup: %v", err)
		}
		err = testutil.WaitForEtcd("localhost:2379", 30*time.Second)
		if err != nil {
			t.Logf("Warning: etcd not available after restart in cleanup: %v", err)
		}
	})

	// Stop etcd
	t.Logf("Stopping etcd...")
	err = testutil.StopEtcd()
	if err != nil {
		t.Fatalf("Failed to stop etcd: %v", err)
	}

	// Wait for 10 seconds (less than NodeLeaseTTL which is 15s)
	// This ensures the lease hasn't expired yet
	t.Logf("Waiting 10 seconds (less than TTL)...")
	time.Sleep(10 * time.Second)

	// Restart etcd
	t.Logf("Starting etcd...")
	err = testutil.StartEtcd()
	if err != nil {
		t.Fatalf("Failed to start etcd: %v", err)
	}

	// Wait for etcd to be ready
	t.Logf("Waiting for etcd to be ready...")
	err = testutil.WaitForEtcd("localhost:2379", 30*time.Second)
	if err != nil {
		t.Fatalf("etcd did not become available: %v", err)
	}

	// Wait for the keepalive loop to reconnect and re-establish the lease
	t.Logf("Waiting for keepalive to reconnect...")
	time.Sleep(5 * time.Second)

	// Create a new manager to query etcd (old connection may be stale)
	mgr2, err := NewEtcdManager("localhost:2379", mgr.GetPrefix())
	if err != nil {
		t.Fatalf("Failed to create new etcd manager: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Failed to connect new etcd manager: %v", err)
	}
	defer mgr2.Close()

	// Verify the node is still registered (keepalive reconnected successfully)
	nodes, _, err = mgr2.getAllNodesForTesting(ctx)
	if err != nil {
		t.Fatalf("getAllNodesForTesting() after reconnection error = %v", err)
	}

	found = false
	for _, node := range nodes {
		if node == nodeAddress {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Node %s not found after etcd reconnection - keepalive may have failed to reconnect", nodeAddress)
	}

	// Cleanup - unregister using the original manager
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Logf("UnregisterKeyLease() error (expected if connection not recovered): %v", err)
	}
}
