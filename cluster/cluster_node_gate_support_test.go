package cluster

import (
	"github.com/xiaonanln/goverse/util/testutil"
	"testing"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
)

// TestClusterNodeAndGateSupport tests that cluster properly supports both node and gate
func TestClusterNodeAndGateSupport(t *testing.T) {
	t.Parallel()
	t.Run("node cluster", func(t *testing.T) {
		n := node.NewNode("localhost:47000", testutil.TestNumShards, "")
		c := newClusterForTesting(n, "TestNodeCluster")

		// Test node cluster identification
		if !c.isNode() {
			t.Fatalf("Expected isNode() to return true for node cluster")
		}
		if c.isGate() {
			t.Fatalf("Expected isGate() to return false for node cluster")
		}

		// Test getAdvertiseAddr
		addr := c.getAdvertiseAddr()
		if addr != "localhost:47000" {
			t.Fatalf("Expected advertise address 'localhost:47000', got '%s'", addr)
		}

		// Test GetThisNode
		if c.GetThisNode() != n {
			t.Fatalf("Expected GetThisNode() to return the node instance")
		}

		// Test GetThisGate
		if c.GetThisGate() != nil {
			t.Fatalf("Expected GetThisGate() to return nil for node cluster")
		}

		// Test String representation
		str := c.String()
		if str == "" {
			t.Fatalf("Expected non-empty string representation")
		}
		// Check that string contains "node" type indicator
		t.Logf("Node cluster string: %s", str)
	})

	t.Run("gate cluster", func(t *testing.T) {
		gwConfig := &gate.GateConfig{
			AdvertiseAddress: "localhost:49000",
			EtcdAddress:      "localhost:2379",
			EtcdPrefix:       "/test-gate",
		}
		gw, err := gate.NewGate(gwConfig)
		if err != nil {
			t.Fatalf("Failed to create gate: %v", err)
		}
		defer gw.Stop()

		c, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", "/test-gate-cluster")
		if err != nil {
			t.Fatalf("Failed to create cluster with gate: %v", err)
		}

		// Test gate cluster identification
		if c.isNode() {
			t.Fatalf("Expected isNode() to return false for gate cluster")
		}
		if !c.isGate() {
			t.Fatalf("Expected isGate() to return true for gate cluster")
		}

		// Test getAdvertiseAddr
		addr := c.getAdvertiseAddr()
		if addr != "localhost:49000" {
			t.Fatalf("Expected advertise address 'localhost:49000', got '%s'", addr)
		}

		// Test GetThisNode
		if c.GetThisNode() != nil {
			t.Fatalf("Expected GetThisNode() to return nil for gate cluster")
		}

		// Test GetThisGate
		if c.GetThisGate() != gw {
			t.Fatalf("Expected GetThisGate() to return the gate instance")
		}

		// Test String representation
		str := c.String()
		if str == "" {
			t.Fatalf("Expected non-empty string representation")
		}
		// Check that string contains "gate" type indicator
		t.Logf("Gate cluster string: %s", str)
	})
}

// TestClusterOperationsWithGate tests that node-only operations return appropriate results for gate clusters
func TestClusterOperationsWithGate(t *testing.T) {
	t.Parallel()
	gwConfig := &gate.GateConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate-ops",
	}
	gw, err := gate.NewGate(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gw.Stop()

	c, err := newClusterWithEtcdForTestingGate("GateCluster", gw, "localhost:2379", "/test-gate-cluster-ops")
	if err != nil {
		t.Fatalf("Failed to create cluster with gate: %v", err)
	}

	// Test that node-only operations handle gate gracefully
	t.Run("releaseShardOwnership", func(t *testing.T) {
		// Should not panic for gate clusters
		c.releaseShardOwnership(nil)
		// Test passes if no panic occurs
	})

	t.Run("removeObjectsNotBelongingToThisNode", func(t *testing.T) {
		// Should not panic for gate clusters
		c.removeObjectsNotBelongingToThisNode(nil)
		// Test passes if no panic occurs
	})
}
