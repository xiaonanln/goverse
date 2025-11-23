package cluster

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/util/testutil"
)

// TestGateClusterCanBecomeReady verifies that a gate cluster can become ready
// when working with 1 node. This test ensures that:
// 1. A gate cluster can start and register itself
// 2. A node cluster can start and register itself
// 3. Both clusters can see each other via etcd
// 4. The gate cluster becomes ready (has consensus and stable state)
func TestGateClusterCanBecomeReady(t *testing.T) {	addr1 := testutil.GetFreeAddress()
	addr2 := testutil.GetFreeAddress()
	

	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	ctx := context.Background()

	// Create and start a node cluster first
	nodeCluster := mustNewCluster(ctx, t, addr1, testPrefix)
	t.Logf("Node cluster started at %s", nodeCluster.getAdvertiseAddr())

	// Create and start a gate cluster
	gateCluster := mustNewGateCluster(ctx, t, addr2, testPrefix)
	t.Logf("Gate cluster started at %s", gateCluster.getAdvertiseAddr())

	// Wait for both clusters to become ready
	// The gate cluster should become ready once it has consensus state
	testutil.WaitForClusterReady(t, nodeCluster)
	t.Logf("✓ Node cluster is ready")

	testutil.WaitForClusterReady(t, gateCluster)
	t.Logf("✓ Gate cluster is ready")

	// Verify both clusters are ready
	if !nodeCluster.IsReady() {
		t.Fatal("Node cluster IsReady() should return true")
	}

	if !gateCluster.IsReady() {
		t.Fatal("Gate cluster IsReady() should return true")
	}

	// Verify both clusters can see each other
	gates := nodeCluster.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Node should see 1 gate, got %d: %v", len(gates), gates)
	}

	nodes := gateCluster.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Gate should see 1 node, got %d: %v", len(nodes), nodes)
	}

	t.Logf("✓ Verified: Gate cluster is ready and can see node in cluster")
}
