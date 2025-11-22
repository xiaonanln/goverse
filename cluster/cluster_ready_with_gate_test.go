package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/util/testutil"
)

// Helper function to create and start a gate cluster with etcd for testing
func mustNewGateCluster(ctx context.Context, t *testing.T, gateAddr string, etcdPrefix string) *Cluster {
	t.Helper()

	// Create gateway config
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}

	// Create cluster with gateway
	gateCluster, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", etcdPrefix)
	if err != nil {
		t.Fatalf("Failed to create gate cluster: %v", err)
	}

	// Start the cluster (which registers the gate)
	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		gateCluster.Stop(ctx)
	})

	return gateCluster
}

// TestNodeReadyWithUnconnectedGate verifies that a node can become ready even when
// a gate is registered in cluster state but has not yet connected to the node.
// This is important because nodes should not wait for gates to connect - gates
// are optional components for client connections, and node readiness should only
// depend on consensus state (shard mapping) being available.
func TestNodeReadyWithUnconnectedGate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Step 1: Create and start a gate cluster (which will register itself in etcd)
	gateCluster := mustNewGateCluster(ctx, t, "localhost:49200", testPrefix)
	t.Logf("Gate cluster started and registered at %s", gateCluster.getAdvertiseAddr())

	// Wait for gate registration to propagate to etcd
	time.Sleep(1 * time.Second)

	// Verify gate is registered in etcd
	gates := gateCluster.GetGates()
	if len(gates) == 0 {
		t.Fatal("Gate should be registered in etcd")
	}
	t.Logf("Gates registered: %v", gates)

	// Step 2: Create and start a node cluster (without connecting to the gate)
	// The node will see the gate in etcd but won't have a gate connection yet
	nodeCluster := mustNewCluster(ctx, t, "localhost:47200", testPrefix)
	t.Logf("Node cluster started at %s", nodeCluster.getAdvertiseAddr())

	// Step 3: Verify that the node cluster becomes ready even without gate connection
	// The node should become ready based on consensus state only
	testutil.WaitForClusterReady(t, nodeCluster)

	// Verify node cluster is ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster IsReady() should return true")
	}

	// Verify the gate is visible to the node in cluster state
	nodesGates := nodeCluster.GetGates()
	if len(nodesGates) == 0 {
		t.Fatal("Node should see gate registered in etcd")
	}
	t.Logf("Node sees gates: %v", nodesGates)

	// Verify node has no gate connections (since gate didn't connect)
	nodeCluster.gateChannelsMu.RLock()
	numGateConns := len(nodeCluster.gateChannels)
	nodeCluster.gateChannelsMu.RUnlock()

	if numGateConns != 0 {
		t.Fatalf("Expected 0 gate connections, got %d (gate hasn't connected yet)", numGateConns)
	}

	t.Logf("✓ Verified: Node is ready with 0 gate connections (gate registered but not connected)")
}

// TestNodeReadyBeforeGateRegisters verifies that a node becomes ready before any gate
// is registered in the cluster. This tests the most basic case where no gates exist yet.
func TestNodeReadyBeforeGateRegisters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Step 1: Create and start a node cluster (no gates in the system yet)
	nodeCluster := mustNewCluster(ctx, t, "localhost:47201", testPrefix)
	t.Logf("Node cluster started at %s", nodeCluster.getAdvertiseAddr())

	// Step 2: Verify that the node cluster becomes ready without any gates
	testutil.WaitForClusterReady(t, nodeCluster)

	// Verify node cluster is ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster IsReady() should return true")
	}

	// Verify no gates exist in cluster state
	gates := nodeCluster.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates in cluster, got %d", len(gates))
	}

	t.Logf("✓ Verified: Node is ready with no gates in cluster state")

	// Step 3: Now register a gate and verify node remains ready
	gateCluster := mustNewGateCluster(ctx, t, "localhost:49201", testPrefix)
	t.Logf("Gate cluster registered at %s", gateCluster.getAdvertiseAddr())

	// Give the watch mechanism time to propagate gate registration
	time.Sleep(500 * time.Millisecond)

	// Node should still be ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster should remain ready after gate registers")
	}

	// Node should now see the gate in cluster state
	gates = nodeCluster.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate in cluster, got %d", len(gates))
	}

	t.Logf("✓ Verified: Node remains ready after gate registers (gates: %v)", gates)
}
