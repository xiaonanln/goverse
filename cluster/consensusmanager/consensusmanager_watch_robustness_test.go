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
	err = mgr.RegisterNode(ctx, "localhost:47001")
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

	// Wait a bit to ensure the watch detects the disconnection
	time.Sleep(500 * time.Millisecond)

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
	time.Sleep(1 * time.Second) // Give watch time to reconnect

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

	err = mgr2.RegisterNode(ctx, "localhost:47002")
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

// TestWatchEventRecovery tests that no events are lost during etcd downtime using revision tracking
func TestWatchEventRecovery(t *testing.T) {
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

	// Register initial node
	err = mgr.RegisterNode(ctx, "localhost:47001")
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

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

	// Restart etcd
	time.Sleep(500 * time.Millisecond)
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

	// Create a new manager for registration
	mgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create second etcd manager: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Failed to connect second etcd manager: %v", err)
	}
	defer mgr2.Close()

	// Register nodes while the watch should be reconnecting
	err = mgr2.RegisterNode(ctx, "localhost:47002")
	if err != nil {
		t.Fatalf("Failed to register second node: %v", err)
	}

	// Create a third manager for third node
	mgr3, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create third etcd manager: %v", err)
	}
	err = mgr3.Connect()
	if err != nil {
		t.Fatalf("Failed to connect third etcd manager: %v", err)
	}
	defer mgr3.Close()

	err = mgr3.RegisterNode(ctx, "localhost:47003")
	if err != nil {
		t.Fatalf("Failed to register third node: %v", err)
	}

	// Wait for the watch to catch up
	time.Sleep(1 * time.Second)

	// Verify all nodes are present (no events lost)
	nodes := cm.GetNodes()
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes (no events lost), got %d: %v", len(nodes), nodes)
	}

	// Verify the watch received all events (even if revision reset due to etcd restart)
	// The important part is that all node registrations were processed
	expectedNodes := map[string]bool{
		"localhost:47001": false,
		"localhost:47002": false,
		"localhost:47003": false,
	}
	for _, node := range nodes {
		expectedNodes[node] = true
	}
	for node, found := range expectedNodes {
		if !found {
			t.Errorf("Node %s was not found in the cluster state after recovery", node)
		}
	}
}

// TestWatchStateConsistency tests that cluster state remains accurate after etcd restart
func TestWatchStateConsistency(t *testing.T) {
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

	// Initialize with nodes and shard mapping
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
	}

	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	// Set up cluster state with nodes
	cm.mu.Lock()
	cm.state.Nodes["localhost:47001"] = true
	cm.state.Nodes["localhost:47002"] = true
	cm.state.ShardMapping = &ShardMapping{
		Shards: make(map[int]ShardInfo),
	}
	for i := 0; i < 10; i++ {
		cm.state.ShardMapping.Shards[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47001",
		}
	}
	cm.mu.Unlock()

	// Store shard mapping to etcd
	shardsToStore := make(map[int]ShardInfo)
	for i := 0; i < 10; i++ {
		shardsToStore[i] = ShardInfo{
			TargetNode:  "localhost:47001",
			CurrentNode: "localhost:47001",
			ModRevision: 0,
		}
	}
	_, err = cm.storeShardMapping(ctx, shardsToStore)
	if err != nil {
		t.Fatalf("Failed to store shard mapping: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

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

	// Stop and restart etcd
	t.Logf("Stopping etcd...")
	err = testutil.StopEtcd()
	if err != nil {
		t.Fatalf("Failed to stop etcd: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	t.Logf("Starting etcd...")
	err = testutil.StartEtcd()
	if err != nil {
		t.Fatalf("Failed to start etcd: %v", err)
	}

	t.Logf("Waiting for etcd to be ready...")
	err = testutil.WaitForEtcd("localhost:2379", 30*time.Second)
	if err != nil {
		t.Fatalf("etcd did not become available: %v", err)
	}

	// Wait for watch to reconnect
	time.Sleep(1 * time.Second)

	// Reload state from etcd to verify consistency
	mgr2, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create second etcd manager: %v", err)
	}
	err = mgr2.Connect()
	if err != nil {
		t.Fatalf("Failed to connect second etcd manager: %v", err)
	}
	defer mgr2.Close()

	cm2 := NewConsensusManager(mgr2)
	err = cm2.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize second consensus manager: %v", err)
	}

	// Verify shard mapping is consistent
	mapping, err := cm2.GetShardMapping()
	if err != nil {
		t.Fatalf("Failed to get shard mapping: %v", err)
	}

	if len(mapping.Shards) != 10 {
		t.Errorf("Expected 10 shards in mapping, got %d", len(mapping.Shards))
	}

	for i := 0; i < 10; i++ {
		shardInfo, ok := mapping.Shards[i]
		if !ok {
			t.Errorf("Shard %d not found in mapping", i)
			continue
		}
		if shardInfo.TargetNode != "localhost:47001" {
			t.Errorf("Shard %d: expected target node localhost:47001, got %s", i, shardInfo.TargetNode)
		}
		if shardInfo.CurrentNode != "localhost:47001" {
			t.Errorf("Shard %d: expected current node localhost:47001, got %s", i, shardInfo.CurrentNode)
		}
	}
}

// TestWatchChannelClosure tests proper handling when etcd connection drops
func TestWatchChannelClosure(t *testing.T) {
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

	// Stop etcd to trigger channel closure
	t.Logf("Stopping etcd to trigger watch channel closure...")
	err = testutil.StopEtcd()
	if err != nil {
		t.Fatalf("Failed to stop etcd: %v", err)
	}

	// Wait a bit for the watch to detect the closure
	// The watch goroutine should handle the channel closure gracefully
	time.Sleep(2 * time.Second)

	// The consensus manager should still be functional (not crashed)
	// Verify we can still call methods without panic
	nodes := cm.GetNodes()
	t.Logf("Nodes after watch channel closure: %v", nodes)

	// Restart etcd
	t.Logf("Restarting etcd...")
	err = testutil.StartEtcd()
	if err != nil {
		t.Fatalf("Failed to start etcd: %v", err)
	}

	err = testutil.WaitForEtcd("localhost:2379", 30*time.Second)
	if err != nil {
		t.Fatalf("etcd did not become available: %v", err)
	}

	// Note: The watch will not automatically reconnect after channel closure
	// This is expected behavior - the watch needs to be explicitly restarted
	// This test verifies that the manager handles the closure gracefully
}

// TestWatchConcurrentOperations tests multiple Start/Stop operations during etcd instability
func TestWatchConcurrentOperations(t *testing.T) {
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

	// Initialize
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize consensus manager: %v", err)
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

	// Start and stop watch multiple times
	for i := 0; i < 3; i++ {
		t.Logf("Watch cycle %d: Starting...", i+1)
		err = cm.StartWatch(ctx)
		if err != nil {
			t.Fatalf("Failed to start watch (cycle %d): %v", i+1, err)
		}

		time.Sleep(200 * time.Millisecond)

		t.Logf("Watch cycle %d: Stopping...", i+1)
		cm.StopWatch()

		time.Sleep(200 * time.Millisecond)
	}

	// Stop and restart etcd while watch is running
	t.Logf("Starting watch for etcd instability test...")
	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	t.Logf("Stopping etcd...")
	err = testutil.StopEtcd()
	if err != nil {
		t.Fatalf("Failed to stop etcd: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	t.Logf("Starting etcd...")
	err = testutil.StartEtcd()
	if err != nil {
		t.Fatalf("Failed to start etcd: %v", err)
	}

	err = testutil.WaitForEtcd("localhost:2379", 30*time.Second)
	if err != nil {
		t.Fatalf("etcd did not become available: %v", err)
	}

	// Verify the manager is still functional
	time.Sleep(500 * time.Millisecond)
	nodes := cm.GetNodes()
	t.Logf("Nodes after etcd restart: %v", nodes)

	// The test passes if no panics occurred
}
