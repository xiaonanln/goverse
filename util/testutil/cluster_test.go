package testutil

import (
	"context"
	"testing"
)

// TestMustNewCluster tests that MustNewCluster creates and starts a cluster properly
func TestMustNewCluster(t *testing.T) {
	ctx := context.Background()

	// Create cluster using MustNewCluster
	c := MustNewCluster(ctx, t, "localhost:60001")

	// Verify cluster was created
	if c == nil {
		t.Fatal("MustNewCluster returned nil cluster")
	}

	// Verify node is set
	if c.GetThisNode() == nil {
		t.Fatal("Cluster should have a node set")
	}

	// Verify node address
	if c.GetThisNode().GetAdvertiseAddress() != "localhost:60001" {
		t.Errorf("Expected node address localhost:60001, got %s", c.GetThisNode().GetAdvertiseAddress())
	}

	// Cleanup is handled automatically by t.Cleanup registered in MustNewCluster
}

// TestMustNewCluster_MultipleInstances tests creating multiple clusters
func TestMustNewCluster_MultipleInstances(t *testing.T) {
	ctx := context.Background()

	// Create first cluster in a subtest
	t.Run("Cluster1", func(t *testing.T) {
		c1 := MustNewCluster(ctx, t, "localhost:60002")
		if c1 == nil {
			t.Fatal("Failed to create first cluster")
		}

		// Verify node is set
		if c1.GetThisNode() == nil {
			t.Fatal("Cluster should have a node set")
		}

		// Verify node address
		if c1.GetThisNode().GetAdvertiseAddress() != "localhost:60002" {
			t.Errorf("Expected node address localhost:60002, got %s", c1.GetThisNode().GetAdvertiseAddress())
		}
	})

	// Create second cluster in a separate subtest
	t.Run("Cluster2", func(t *testing.T) {
		c2 := MustNewCluster(ctx, t, "localhost:60003")
		if c2 == nil {
			t.Fatal("Failed to create second cluster")
		}

		// Verify node is set
		if c2.GetThisNode() == nil {
			t.Fatal("Cluster should have a node set")
		}

		// Verify node address
		if c2.GetThisNode().GetAdvertiseAddress() != "localhost:60003" {
			t.Errorf("Expected node address localhost:60003, got %s", c2.GetThisNode().GetAdvertiseAddress())
		}
	})
}
