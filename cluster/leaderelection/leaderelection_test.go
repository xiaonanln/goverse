package leaderelection

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// TestFairElection verifies that the first node to campaign wins leadership
func TestFairElection(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create two nodes that will campaign for leadership
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	le1, err := createTestLeaderElection(t, "localhost:2379", prefix, "node1", 10)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le1.Close()

	le2, err := createTestLeaderElection(t, "localhost:2379", prefix, "node2", 10)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le2.Close()

	// Start both leader elections
	if err := le1.Start(ctx); err != nil {
		t.Fatalf("Failed to start le1: %v", err)
	}
	if err := le2.Start(ctx); err != nil {
		t.Fatalf("Failed to start le2: %v", err)
	}

	// Node1 campaigns first
	le1.StartCampaign(ctx)

	// Give it time to become leader
	time.Sleep(2 * time.Second)

	// Node2 campaigns second (should wait in queue)
	le2.StartCampaign(ctx)

	// Wait for leader election to stabilize
	time.Sleep(2 * time.Second)

	// Verify node1 became leader (first to campaign)
	if !le1.IsLeader() {
		t.Errorf("Expected node1 to be leader (campaigned first), but it's not")
	}
	if le2.IsLeader() {
		t.Errorf("Expected node2 to not be leader (campaigned second), but it is")
	}

	// Verify both see the same leader
	leader1 := le1.GetLeader()
	leader2 := le2.GetLeader()
	if leader1 != "node1" {
		t.Errorf("Node1 sees wrong leader: got %s, want node1", leader1)
	}
	if leader2 != "node1" {
		t.Errorf("Node2 sees wrong leader: got %s, want node1", leader2)
	}
}

// TestAutomaticFailover verifies that when the leader crashes, the next candidate becomes leader
func TestAutomaticFailover(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Use shorter TTL for faster failover in tests
	le1, err := createTestLeaderElection(t, "localhost:2379", prefix, "node1", 3)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le1.Close()

	le2, err := createTestLeaderElection(t, "localhost:2379", prefix, "node2", 3)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le2.Close()

	// Start both leader elections
	if err := le1.Start(ctx); err != nil {
		t.Fatalf("Failed to start le1: %v", err)
	}
	if err := le2.Start(ctx); err != nil {
		t.Fatalf("Failed to start le2: %v", err)
	}

	// Both nodes campaign
	le1.StartCampaign(ctx)
	time.Sleep(1 * time.Second)
	le2.StartCampaign(ctx)

	// Wait for election to stabilize
	time.Sleep(2 * time.Second)

	// Verify node1 is leader
	if !le1.IsLeader() {
		t.Fatalf("Expected node1 to be leader initially")
	}

	// Simulate node1 crash by closing it (session will expire)
	t.Logf("Simulating node1 crash by closing leader election")
	if err := le1.Close(); err != nil {
		t.Errorf("Failed to close le1: %v", err)
	}

	// Wait for lease to expire and node2 to take over
	// TTL is 3 seconds, so wait up to 10 seconds for failover
	failedOver := false
	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if le2.IsLeader() {
			failedOver = true
			t.Logf("Failover successful after ~%d ms", i*500)
			break
		}
	}

	if !failedOver {
		t.Errorf("Node2 did not become leader after node1 crashed (waited 10s)")
	}

	// Verify node2 sees itself as leader
	leader2 := le2.GetLeader()
	if leader2 != "node2" {
		t.Errorf("Node2 sees wrong leader: got %s, want node2", leader2)
	}
}

// TestGracefulResign verifies that a leader can voluntarily resign
func TestGracefulResign(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	le1, err := createTestLeaderElection(t, "localhost:2379", prefix, "node1", 10)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le1.Close()

	le2, err := createTestLeaderElection(t, "localhost:2379", prefix, "node2", 10)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le2.Close()

	// Start both leader elections
	if err := le1.Start(ctx); err != nil {
		t.Fatalf("Failed to start le1: %v", err)
	}
	if err := le2.Start(ctx); err != nil {
		t.Fatalf("Failed to start le2: %v", err)
	}

	// Both nodes campaign
	le1.StartCampaign(ctx)
	time.Sleep(1 * time.Second)
	le2.StartCampaign(ctx)

	// Wait for election to stabilize
	time.Sleep(2 * time.Second)

	// Verify node1 is leader
	if !le1.IsLeader() {
		t.Fatalf("Expected node1 to be leader initially")
	}

	// Node1 gracefully resigns
	t.Logf("Node1 gracefully resigning from leadership")
	resignCtx, resignCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resignCancel()

	if err := le1.Resign(resignCtx); err != nil {
		t.Errorf("Failed to resign: %v", err)
	}

	// Wait for node2 to take over
	time.Sleep(2 * time.Second)

	// Verify node1 is no longer leader
	if le1.IsLeader() {
		t.Errorf("Node1 should not be leader after resigning")
	}

	// Verify node2 became leader
	if !le2.IsLeader() {
		t.Errorf("Node2 should be leader after node1 resigned")
	}

	// Verify both see node2 as leader
	leader1 := le1.GetLeader()
	leader2 := le2.GetLeader()
	if leader1 != "node2" {
		t.Errorf("Node1 sees wrong leader after resign: got %s, want node2", leader1)
	}
	if leader2 != "node2" {
		t.Errorf("Node2 sees wrong leader: got %s, want node2", leader2)
	}
}

// TestLeaderStability verifies that adding a new node doesn't cause leader flapping
func TestLeaderStability(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start node1 and make it leader
	le1, err := createTestLeaderElection(t, "localhost:2379", prefix, "node1", 10)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer le1.Close()

	if err := le1.Start(ctx); err != nil {
		t.Fatalf("Failed to start le1: %v", err)
	}
	le1.StartCampaign(ctx)

	// Wait for node1 to become leader
	time.Sleep(2 * time.Second)

	if !le1.IsLeader() {
		t.Fatalf("Expected node1 to be leader initially")
	}

	initialLeader := le1.GetLeader()
	t.Logf("Initial leader: %s", initialLeader)

	// Add node3 with a "smaller" ID (would win in lexicographic election)
	// but should NOT become leader because node1 is already leading
	le3, err := createTestLeaderElection(t, "localhost:2379", prefix, "alpha-node", 10)
	if err != nil {
		t.Fatalf("Failed to create le3: %v", err)
	}
	defer le3.Close()

	if err := le3.Start(ctx); err != nil {
		t.Fatalf("Failed to start le3: %v", err)
	}
	le3.StartCampaign(ctx)

	// Wait and verify leader didn't change
	time.Sleep(3 * time.Second)

	// Node1 should still be leader
	if !le1.IsLeader() {
		t.Errorf("Leader changed when new node joined - leader flapping detected!")
	}

	// Node3 should not be leader
	if le3.IsLeader() {
		t.Errorf("New node with smaller address stole leadership - leader flapping detected!")
	}

	// Both should see node1 as leader
	leader1 := le1.GetLeader()
	leader3 := le3.GetLeader()
	if leader1 != initialLeader {
		t.Errorf("Leader changed from %s to %s - flapping detected", initialLeader, leader1)
	}
	if leader3 != initialLeader {
		t.Errorf("Node3 sees different leader: got %s, want %s", leader3, initialLeader)
	}

	t.Logf("Leader stability verified - no flapping on new node join")
}

// TestMultipleCandidates verifies behavior with multiple nodes campaigning concurrently
func TestMultipleCandidates(t *testing.T) {
	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	numNodes := 5
	elections := make([]*LeaderElection, numNodes)

	// Create and start all nodes
	for i := 0; i < numNodes; i++ {
		nodeID := fmt.Sprintf("node%d", i)
		le, err := createTestLeaderElection(t, "localhost:2379", prefix, nodeID, 10)
		if err != nil {
			t.Skipf("Skipping - etcd unavailable: %v", err)
			return
		}
		defer le.Close()

		if err := le.Start(ctx); err != nil {
			t.Fatalf("Failed to start node %d: %v", i, err)
		}

		elections[i] = le
	}

	// All nodes campaign concurrently
	for i := 0; i < numNodes; i++ {
		elections[i].StartCampaign(ctx)
	}

	// Wait for election to stabilize
	time.Sleep(3 * time.Second)

	// Verify exactly one leader
	leaderCount := 0
	var leaderID string
	for i := 0; i < numNodes; i++ {
		if elections[i].IsLeader() {
			leaderCount++
			leaderID = elections[i].nodeID
		}
	}

	if leaderCount != 1 {
		t.Errorf("Expected exactly 1 leader, got %d", leaderCount)
	}

	// Verify all nodes agree on the leader
	for i := 0; i < numNodes; i++ {
		observedLeader := elections[i].GetLeader()
		if observedLeader != leaderID {
			t.Errorf("Node %d sees leader %s, but actual leader is %s",
				i, observedLeader, leaderID)
		}
	}

	t.Logf("Multiple candidates test passed - 1 leader elected: %s", leaderID)
}

// createTestLeaderElection creates a leader election for testing
func createTestLeaderElection(t *testing.T, etcdAddr, prefix, nodeID string, ttl int) (*LeaderElection, error) {
	t.Helper()

	// Create etcd client
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{etcdAddr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	// Test connection
	testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Get(testCtx, "test-connection")
	if err != nil {
		client.Close()
		return nil, err
	}

	// Create leader election
	electionPrefix := prefix + "/leader"
	le, err := NewLeaderElection(client, electionPrefix, nodeID, ttl)
	if err != nil {
		client.Close()
		return nil, err
	}

	return le, nil
}
