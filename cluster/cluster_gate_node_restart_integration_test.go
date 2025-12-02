package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

const (
	// Test timing constants for node restart scenario
	nodeShutdownDetectionTimeout = 5 * time.Second  // Time to wait for gate to detect node shutdown
	reconnectionTimeout          = 15 * time.Second // Maximum time to wait for reconnection
	reconnectionCheckInterval    = 2 * time.Second  // Interval between reconnection checks
)

// makeTestClusterConfig creates a standard cluster config for testing
func makeTestClusterConfig(etcdPrefix string) Config {
	return Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 3 * time.Second,
		ShardMappingCheckInterval:     1 * time.Second,
		NumShards:                     testutil.TestNumShards,
	}
}

// TestGateReconnectsToNodeAfterRestart tests that when a node shuts down and restarts
// with the same address, the gate automatically reconnects to it
func TestGateReconnectsToNodeAfterRestart(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Use unique ports to avoid conflicts with other tests
	nodeAddr := "localhost:47999"

	// Create a gate cluster
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49999",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       testPrefix,
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}

	gateCluster, err := newClusterWithEtcdForTestingGate("gateCluster", gw, "localhost:2379", testPrefix)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	// Create and start the first node cluster
	// We don't use mustNewCluster here because we need to explicitly stop and restart
	// the node mid-test to simulate a node restart scenario
	t.Logf("Creating first node cluster at %s", nodeAddr)
	n1 := node.NewNode(nodeAddr, testutil.TestNumShards, "")
	err = n1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start first node: %v", err)
	}

	clusterCfg := makeTestClusterConfig(testPrefix)

	nodeCluster1, err := NewClusterWithNode(clusterCfg, n1)
	if err != nil {
		n1.Stop(ctx)
		t.Fatalf("Failed to create first cluster: %v", err)
	}

	err = nodeCluster1.Start(ctx, n1)
	if err != nil {
		n1.Stop(ctx)
		t.Fatalf("Failed to start first cluster: %v", err)
	}

	// Wait for gate to discover and register with the node
	time.Sleep(testutil.WaitForShardMappingTimeout)

	// Verify gate sees the node
	nodes := gateCluster.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Gate should see 1 node before restart, got %d", len(nodes))
	}
	t.Logf("Gate successfully discovered node: %v", nodes)

	// Verify gate has established connection to the node
	gateNodeConns := gateCluster.GetNodeConnections().GetAllConnections()
	if len(gateNodeConns) != 1 {
		t.Fatalf("Gate should have 1 node connection before restart, got %d", len(gateNodeConns))
	}
	t.Logf("Gate has %d node connection(s)", len(gateNodeConns))

	// Verify node sees the gate (gate is registered)
	gates := nodeCluster1.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Node should see 1 gate before restart, got %d", len(gates))
	}
	t.Logf("Node successfully registered gate: %v", gates)

	// Stop the first node cluster (simulating node shutdown)
	t.Logf("Stopping first node cluster")
	err = nodeCluster1.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop first node cluster: %v", err)
	}
	err = n1.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop first node: %v", err)
	}

	// Wait for gate to detect the node is down
	// The node should be removed from etcd when the cluster stops
	// Give time for etcd update and cluster state propagation
	t.Logf("Waiting for gate to detect node shutdown...")
	time.Sleep(nodeShutdownDetectionTimeout)

	// Verify gate no longer sees the node (or sees it as unavailable)
	nodes = gateCluster.GetNodes()
	t.Logf("After node shutdown, gate sees %d nodes: %v", len(nodes), nodes)
	if len(nodes) != 0 {
		t.Logf("Warning: Gate still sees node after shutdown, may need more time for cleanup")
	}

	// Verify gate closed the connection
	gateNodeConns = gateCluster.GetNodeConnections().GetAllConnections()
	t.Logf("After node shutdown, gate has %d node connection(s)", len(gateNodeConns))
	if len(gateNodeConns) != 0 {
		t.Logf("Warning: Gate still has connection after shutdown, may need more time for cleanup")
	}

	// Create and start a new node cluster with the SAME address (simulating restart)
	t.Logf("Starting second node cluster at same address %s", nodeAddr)
	n2 := node.NewNode(nodeAddr, testutil.TestNumShards, "")
	err = n2.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start second node: %v", err)
	}
	defer n2.Stop(ctx)

	// Reuse the same cluster config for the restarted node
	nodeCluster2, err := NewClusterWithNode(clusterCfg, n2)
	if err != nil {
		t.Fatalf("Failed to create second cluster: %v", err)
	}
	defer nodeCluster2.Stop(ctx)

	err = nodeCluster2.Start(ctx, n2)
	if err != nil {
		t.Fatalf("Failed to start second cluster: %v", err)
	}

	// Wait for gate to discover the restarted node and reconnect
	// The gate's periodic cluster management should trigger:
	// 1. updateNodeConnections() - creates new gRPC connection to the node
	// 2. registerGateWithNodes() - calls RegisterGate on the new connection
	t.Logf("Waiting for gate to reconnect to restarted node...")

	// Wait longer to ensure multiple cluster management ticks occur
	// Each tick should attempt to reconnect if not already connected
	deadline := time.Now().Add(reconnectionTimeout)
	reconnected := false

	for time.Now().Before(deadline) {
		time.Sleep(reconnectionCheckInterval)

		// Check if gate sees the node
		nodes = gateCluster.GetNodes()
		gates = nodeCluster2.GetGates()

		t.Logf("Status check: gate sees %d nodes, node sees %d gates", len(nodes), len(gates))

		if len(nodes) == 1 && len(gates) == 1 {
			reconnected = true
			t.Logf("Gate successfully reconnected!")
			break
		}
	}

	if !reconnected {
		// Print final state for debugging
		nodes = gateCluster.GetNodes()
		gates = nodeCluster2.GetGates()
		gateNodeConns = gateCluster.GetNodeConnections().GetAllConnections()

		t.Logf("Final state after waiting %v:", reconnectionTimeout)
		t.Logf("  - Gate sees %d nodes: %v", len(nodes), nodes)
		t.Logf("  - Gate has %d connections", len(gateNodeConns))
		t.Logf("  - Node sees %d gates: %v", len(gates), gates)

		t.Fatalf("Gate failed to reconnect to node within %v", reconnectionTimeout)
	}

	// Verify gate sees the node again
	nodes = gateCluster.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Gate should see 1 node after restart, got %d: %v", len(nodes), nodes)
	}
	t.Logf("Gate successfully rediscovered restarted node: %v", nodes)

	// Verify gate has connection to the node
	gateNodeConns = gateCluster.GetNodeConnections().GetAllConnections()
	if len(gateNodeConns) != 1 {
		t.Fatalf("Gate should have 1 node connection after restart, got %d", len(gateNodeConns))
	}
	t.Logf("Gate has %d node connection(s)", len(gateNodeConns))

	// Verify node sees the gate (gate successfully re-registered)
	gates = nodeCluster2.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Restarted node should see 1 gate, got %d: %v", len(gates), gates)
	}
	t.Logf("Restarted node successfully registered gate: %v", gates)

	t.Logf("Gate successfully reconnected to node after restart")
}
