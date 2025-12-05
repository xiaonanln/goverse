package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestGetLeaderNode_WithLeader(t *testing.T) {
	t.Parallel()
	mgr, _ := etcdmanager.NewEtcdManager("localhost:2379", "/test")
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "localhost:47001", testNumShards)

	// Set leader in internal state
	cm.mu.Lock()
	cm.state.Leader = "localhost:47002"
	cm.state.LeaderModRevision = 123
	cm.mu.Unlock()

	leader := cm.GetLeaderNode()
	if leader != "localhost:47002" {
		t.Fatalf("Expected leader localhost:47002, got %s", leader)
	}
}

func TestLeaderElection_NoLeaderExists(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	nodeAddr := "localhost:47001"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, nodeAddr, testNumShards)

	// Add this node to the cluster state
	cm.mu.Lock()
	cm.state.Nodes[nodeAddr] = true
	cm.mu.Unlock()

	ctx := context.Background()

	// Initialize and load state
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Try to become leader
	err = cm.tryBecomeLeader(ctx)
	if err != nil {
		t.Fatalf("Failed to become leader: %v", err)
	}

	// Wait a bit for watch events to propagate
	time.Sleep(100 * time.Millisecond)

	// Verify this node became leader
	leader := cm.GetLeaderNode()
	if leader != nodeAddr {
		t.Fatalf("Expected node %s to become leader, got %s", nodeAddr, leader)
	}

	// Verify the leader key is set in etcd
	client := mgr.GetClient()
	leaderKey := prefix + "/leader"
	resp, err := client.Get(ctx, leaderKey)
	if err != nil {
		t.Fatalf("Failed to get leader key from etcd: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatal("Leader key not found in etcd")
	}
	if string(resp.Kvs[0].Value) != nodeAddr {
		t.Fatalf("Expected leader key value %s, got %s", nodeAddr, string(resp.Kvs[0].Value))
	}
}

func TestLeaderElection_LeaderStaysStable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	node1Addr := "localhost:47001"
	node2Addr := "localhost:47002"

	cm1 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node1Addr, testNumShards)
	cm2 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node2Addr, testNumShards)

	// Add both nodes to cluster state
	cm1.mu.Lock()
	cm1.state.Nodes[node1Addr] = true
	cm1.state.Nodes[node2Addr] = true
	cm1.mu.Unlock()

	cm2.mu.Lock()
	cm2.state.Nodes[node1Addr] = true
	cm2.state.Nodes[node2Addr] = true
	cm2.mu.Unlock()

	ctx := context.Background()

	// Initialize both
	err = cm1.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm1: %v", err)
	}
	err = cm2.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm2: %v", err)
	}

	// Node 1 becomes leader first
	err = cm1.tryBecomeLeader(ctx)
	if err != nil {
		t.Fatalf("Failed for node1 to become leader: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify node1 is leader
	leader1 := cm1.GetLeaderNode()
	if leader1 != node1Addr {
		t.Fatalf("Expected node1 %s to be leader, got %s", node1Addr, leader1)
	}

	// Node 2 tries to become leader (should fail because node1 is alive)
	err = cm2.tryBecomeLeader(ctx)
	if err != nil {
		t.Fatalf("tryBecomeLeader should not error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify leader is still node1
	leader2 := cm2.GetLeaderNode()
	if leader2 != node1Addr {
		t.Fatalf("Expected leader to remain %s, got %s", node1Addr, leader2)
	}
}

func TestLeaderElection_NewLeaderWhenCurrentFails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	node1Addr := "localhost:47001"
	node2Addr := "localhost:47002"

	cm1 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node1Addr, testNumShards)
	cm2 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node2Addr, testNumShards)

	ctx := context.Background()

	// Initialize both
	err = cm1.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm1: %v", err)
	}
	err = cm2.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm2: %v", err)
	}

	// Start watches for both
	err = cm1.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch for cm1: %v", err)
	}
	defer cm1.StopWatch()

	err = cm2.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch for cm2: %v", err)
	}
	defer cm2.StopWatch()

	// Add node1 to etcd (to simulate it being alive)
	client := mgr.GetClient()
	node1Key := prefix + "/nodes/node1"
	_, err = client.Put(ctx, node1Key, node1Addr)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}

	// Add node2 to etcd
	node2Key := prefix + "/nodes/node2"
	_, err = client.Put(ctx, node2Key, node2Addr)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}

	// Wait for watches to pick up nodes
	time.Sleep(200 * time.Millisecond)

	// Node 1 becomes leader
	err = cm1.tryBecomeLeader(ctx)
	if err != nil {
		t.Fatalf("Failed for node1 to become leader: %v", err)
	}

	// Wait for leader to be established
	time.Sleep(200 * time.Millisecond)

	// Verify node1 is leader
	if cm2.GetLeaderNode() != node1Addr {
		t.Fatalf("Expected node1 to be leader, got %s", cm2.GetLeaderNode())
	}

	// Simulate node1 failure by removing it from nodes
	_, err = client.Delete(ctx, node1Key)
	if err != nil {
		t.Fatalf("Failed to delete node1: %v", err)
	}

	// Wait for watch to process the deletion
	time.Sleep(200 * time.Millisecond)

	// Node 2 tries to become leader (should succeed now)
	err = cm2.tryBecomeLeader(ctx)
	if err != nil {
		t.Fatalf("Failed for node2 to become leader: %v", err)
	}

	// Wait for leader change to propagate
	time.Sleep(200 * time.Millisecond)

	// Verify node2 is now the leader
	leader := cm2.GetLeaderNode()
	if leader != node2Addr {
		t.Fatalf("Expected node2 %s to become leader after node1 failed, got %s", node2Addr, leader)
	}
}

func TestLeaderElection_RaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	node1Addr := "localhost:47001"
	node2Addr := "localhost:47002"

	cm1 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node1Addr, testNumShards)
	cm2 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, node2Addr, testNumShards)

	ctx := context.Background()

	// Initialize both
	err = cm1.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm1: %v", err)
	}
	err = cm2.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize cm2: %v", err)
	}

	// Start watches
	err = cm1.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch for cm1: %v", err)
	}
	defer cm1.StopWatch()

	err = cm2.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch for cm2: %v", err)
	}
	defer cm2.StopWatch()

	// Add both nodes to etcd
	client := mgr.GetClient()
	node1Key := prefix + "/nodes/node1"
	_, err = client.Put(ctx, node1Key, node1Addr)
	if err != nil {
		t.Fatalf("Failed to register node1: %v", err)
	}

	node2Key := prefix + "/nodes/node2"
	_, err = client.Put(ctx, node2Key, node2Addr)
	if err != nil {
		t.Fatalf("Failed to register node2: %v", err)
	}

	// Wait for watches to pick up nodes
	time.Sleep(200 * time.Millisecond)

	// Both nodes try to become leader simultaneously
	done := make(chan error, 2)
	go func() {
		done <- cm1.tryBecomeLeader(ctx)
	}()
	go func() {
		done <- cm2.tryBecomeLeader(ctx)
	}()

	// Wait for both attempts to complete
	err1 := <-done
	err2 := <-done

	if err1 != nil {
		t.Fatalf("tryBecomeLeader failed for cm1: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("tryBecomeLeader failed for cm2: %v", err2)
	}

	// Wait for state to propagate
	time.Sleep(200 * time.Millisecond)

	// Verify only one leader exists in etcd
	leaderKey := prefix + "/leader"
	resp, err := client.Get(ctx, leaderKey)
	if err != nil {
		t.Fatalf("Failed to get leader key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatal("No leader elected")
	}

	leaderAddr := string(resp.Kvs[0].Value)
	if leaderAddr != node1Addr && leaderAddr != node2Addr {
		t.Fatalf("Invalid leader address: %s", leaderAddr)
	}

	// Verify both nodes agree on the leader
	leader1 := cm1.GetLeaderNode()
	leader2 := cm2.GetLeaderNode()
	if leader1 != leader2 {
		t.Fatalf("Nodes disagree on leader: cm1=%s, cm2=%s", leader1, leader2)
	}
	if leader1 != leaderAddr {
		t.Fatalf("Nodes' view of leader (%s) doesn't match etcd (%s)", leader1, leaderAddr)
	}
}

func TestLeaderElection_WatchUpdatesState(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	nodeAddr := "localhost:47001"
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, nodeAddr, testNumShards)

	ctx := context.Background()

	// Initialize
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Start watch
	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	// Verify no leader initially
	if cm.GetLeaderNode() != "" {
		t.Fatal("Expected no leader initially")
	}

	// Manually set leader in etcd
	client := mgr.GetClient()
	leaderKey := prefix + "/leader"
	_, err = client.Put(ctx, leaderKey, nodeAddr)
	if err != nil {
		t.Fatalf("Failed to set leader in etcd: %v", err)
	}

	// Wait for watch to pick up the change
	time.Sleep(200 * time.Millisecond)

	// Verify state was updated
	leader := cm.GetLeaderNode()
	if leader != nodeAddr {
		t.Fatalf("Expected leader %s, got %s", nodeAddr, leader)
	}

	// Delete leader key
	_, err = client.Delete(ctx, leaderKey)
	if err != nil {
		t.Fatalf("Failed to delete leader key: %v", err)
	}

	// Wait for watch to pick up the deletion
	time.Sleep(200 * time.Millisecond)

	// Verify state was updated
	if cm.GetLeaderNode() != "" {
		t.Fatalf("Expected no leader after deletion, got %s", cm.GetLeaderNode())
	}
}

func TestLeaderElection_AutomaticLeaderElectionLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}

	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", prefix)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer mgr.Close()

	err = mgr.Connect()
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}

	nodeAddr := "localhost:47001"
	// Use shorter interval for faster test
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, nodeAddr, testNumShards)
	cm.SetLeaderCheckInterval(500 * time.Millisecond)

	ctx := context.Background()

	// Initialize
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Register node in etcd
	client := mgr.GetClient()
	nodeKey := prefix + "/nodes/node1"
	_, err = client.Put(ctx, nodeKey, nodeAddr)
	if err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Start watch (this also starts leader election loop)
	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}
	defer cm.StopWatch()

	// Wait for watch to process node registration
	time.Sleep(200 * time.Millisecond)

	// Wait for leader election loop to run (should become leader within 1 second)
	time.Sleep(1500 * time.Millisecond)

	// Verify this node became leader automatically
	leader := cm.GetLeaderNode()
	if leader != nodeAddr {
		t.Fatalf("Expected node %s to become leader automatically, got %s", nodeAddr, leader)
	}

	// Verify leader key is in etcd
	leaderKey := prefix + "/leader"
	resp, err := client.Get(ctx, leaderKey)
	if err != nil {
		t.Fatalf("Failed to get leader key: %v", err)
	}
	if len(resp.Kvs) == 0 {
		t.Fatal("Leader key not found in etcd")
	}
	if string(resp.Kvs[0].Value) != nodeAddr {
		t.Fatalf("Expected leader %s, got %s", nodeAddr, string(resp.Kvs[0].Value))
	}
}
