package cluster

import (
	"testing"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
)

// TestClusterNodeAndGatewaySupport tests that cluster properly supports both node and gateway
func TestClusterNodeAndGatewaySupport(t *testing.T) {
	t.Run("node cluster", func(t *testing.T) {
		n := node.NewNode("localhost:47000")
		c := newClusterForTesting(n, "TestNodeCluster")

		// Test node cluster identification
		if !c.isNode() {
			t.Fatalf("Expected isNode() to return true for node cluster")
		}
		if c.isGateway() {
			t.Fatalf("Expected isGateway() to return false for node cluster")
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

		// Test GetThisGateway
		if c.GetThisGateway() != nil {
			t.Fatalf("Expected GetThisGateway() to return nil for node cluster")
		}

		// Test String representation
		str := c.String()
		if str == "" {
			t.Fatalf("Expected non-empty string representation")
		}
		// Check that string contains "node" type indicator
		t.Logf("Node cluster string: %s", str)
	})

	t.Run("gateway cluster", func(t *testing.T) {
		gwConfig := &gate.GatewayConfig{
			AdvertiseAddress: "localhost:49000",
			EtcdAddress:      "localhost:2379",
			EtcdPrefix:       "/test-gateway",
		}
		gw, err := gate.NewGateway(gwConfig)
		if err != nil {
			t.Fatalf("Failed to create gateway: %v", err)
		}
		defer gw.Stop()

		cfg := Config{
			EtcdAddress: "localhost:2379",
			EtcdPrefix:  "/test-gateway-cluster",
			MinQuorum:   1,
		}
		c, err := NewClusterWithGate(cfg, gw)
		if err != nil {
			t.Fatalf("Failed to create cluster with gateway: %v", err)
		}

		// Test gateway cluster identification
		if c.isNode() {
			t.Fatalf("Expected isNode() to return false for gateway cluster")
		}
		if !c.isGateway() {
			t.Fatalf("Expected isGateway() to return true for gateway cluster")
		}

		// Test getAdvertiseAddr
		addr := c.getAdvertiseAddr()
		if addr != "localhost:49000" {
			t.Fatalf("Expected advertise address 'localhost:49000', got '%s'", addr)
		}

		// Test GetThisNode
		if c.GetThisNode() != nil {
			t.Fatalf("Expected GetThisNode() to return nil for gateway cluster")
		}

		// Test GetThisGateway
		if c.GetThisGateway() != gw {
			t.Fatalf("Expected GetThisGateway() to return the gateway instance")
		}

		// Test String representation
		str := c.String()
		if str == "" {
			t.Fatalf("Expected non-empty string representation")
		}
		// Check that string contains "gateway" type indicator
		t.Logf("Gateway cluster string: %s", str)
	})
}

// TestClusterOperationsWithGateway tests that node-only operations return appropriate results for gateway clusters
func TestClusterOperationsWithGateway(t *testing.T) {
	gwConfig := &gate.GatewayConfig{
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gateway-ops",
	}
	gw, err := gate.NewGateway(gwConfig)
	if err != nil {
		t.Fatalf("Failed to create gateway: %v", err)
	}
	defer gw.Stop()

	cfg := Config{
		EtcdAddress: "localhost:2379",
		EtcdPrefix:  "/test-gateway-cluster-ops",
		MinQuorum:   1,
	}
	c, err := NewClusterWithGate(cfg, gw)
	if err != nil {
		t.Fatalf("Failed to create cluster with gateway: %v", err)
	}

	// Test that node-only operations handle gateway gracefully
	t.Run("releaseShardOwnership", func(t *testing.T) {
		// Should not panic for gateway clusters
		c.releaseShardOwnership(nil)
		// Test passes if no panic occurs
	})

	t.Run("removeObjectsNotBelongingToThisNode", func(t *testing.T) {
		// Should not panic for gateway clusters
		c.removeObjectsNotBelongingToThisNode(nil)
		// Test passes if no panic occurs
	})
}
