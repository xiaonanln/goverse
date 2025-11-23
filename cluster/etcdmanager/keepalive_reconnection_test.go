//go:build etcd_restart
// +build etcd_restart

package etcdmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

// canEtcdRestart checks if etcd can be restarted in the current environment
func canEtcdRestart() bool {
	return testutil.IsGitHubActions()
}

// TestRegisterKeyLeaseReconnection tests that RegisterKeyLease persists through etcd reconnection
func TestRegisterKeyLeaseReconnection(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	if !canEtcdRestart() {
		t.Skip("Skipping reconnection test - not in CI environment")
	}

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
	time.Sleep(20 * time.Second)

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
		t.Fatalf("Node %s not found after etcd reconnection - keepalive may have failed to reconnect", nodeAddress)
	}

	// Cleanup - unregister using the original manager
	err = mgr.UnregisterKeyLease(ctx, key)
	if err != nil {
		t.Logf("UnregisterKeyLease() error (expected if connection not recovered): %v", err)
	}
}
