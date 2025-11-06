package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestWatchReconnection tests that the watch reconnects after etcd is stopped and restarted
func TestWatchReconnection(t *testing.T) {
	if !testutil.IsGitHubActions() {
		t.Skip("Skipping watch robustness test - not in CI environment")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create etcd manager and consensus manager
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}

	err = mgr.Connect()
	if err != nil {
		t.Skipf("Skipping test - etcd connection failed: %v", err)
		return
	}
	defer mgr.Close()

	cm := NewConsensusManager(mgr)

	// Initialize and start watch
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
	}

	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	// Register a node before stopping etcd
	nodesPrefix := mgr.GetPrefix() + "/nodes/"
	key := nodesPrefix + "localhost:47001"
	_, err = mgr.RegisterKeyLease(ctx, key, "localhost:47001", etcdmanager.NodeLeaseTTL)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Wait a bit for the watch to process the event
	time.Sleep(200 * time.Millisecond)

	// Verify the node was registered
	nodes := cm.GetNodes()
	if len(nodes) != 1 || nodes[0] != "localhost:47001" {
		t.Fatalf("Expected 1 node (localhost:47001), got %v", nodes)
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

	// Stop for longer (20 seconds) to ensure watch detects disconnection
	time.Sleep(20 * time.Second)

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

	// The watch should automatically reconnect
	// Register another node to verify the watch is working
	time.Sleep(10 * time.Second) // Wait for 10s for watch to reconnect

	// Create a new manager connection for registration (old connection may be stale)
	mgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create second etcd manager: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Failed to connect second etcd manager: %v", err)
	}
	defer mgr2.Close()

	nodesPrefix2 := mgr2.GetPrefix() + "/nodes/"
	key2 := nodesPrefix2 + "localhost:47002"
	_, err = mgr2.RegisterKeyLease(ctx, key2, "localhost:47002", etcdmanager.NodeLeaseTTL)
	if err != nil {
		t.Fatalf("Failed to register second node: %v", err)
	}

	// Wait for the watch to process the new event
	// The watch should have reconnected and picked up the new node
	time.Sleep(500 * time.Millisecond)

	// Verify both nodes are present (watch reconnected successfully)
	nodes = cm.GetNodes()
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes after reconnection, got %d: %v", len(nodes), nodes)
	}
}
