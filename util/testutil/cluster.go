package testutil

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
)

// MustNewCluster creates a cluster with etcd, creates and starts a node, and registers cleanup.
// It uses localhost:2379 for etcd by default.
// The test will fail with t.Fatalf() if etcd is not available or connection fails.
//
// Parameters:
//   - ctx: context for operations
//   - t: testing.T instance
//   - nodeAddr: address for the node (e.g., "localhost:50000")
//   - etcdPrefix: etcd prefix for cluster isolation (obtain via PrepareEtcdPrefix)
//
// Returns:
//   - *cluster.Cluster: the created and started cluster
//
// The function automatically:
//   - Creates and starts a node with the specified address
//   - Creates cluster with etcd connection using the provided prefix
//   - Starts the cluster (registers node, starts watching, etc.)
//   - Registers cleanup to stop cluster and node when test completes
//
// Usage:
//
//	prefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
//	c1 := testutil.MustNewCluster(ctx, t, "localhost:50000", prefix)
//	c2 := testutil.MustNewCluster(ctx, t, "localhost:50001", prefix)  // Reuse same prefix
func MustNewCluster(ctx context.Context, t *testing.T, nodeAddr string, etcdPrefix string) *cluster.Cluster {
	t.Helper()

	// Create a node
	n := node.NewNode(nodeAddr)

	// Start the node
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create cluster with etcd
	c, err := cluster.NewCluster(n, "localhost:2379", etcdPrefix)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster creation fails
		t.Fatalf("Failed to create cluster: %v", err)
	}

	// Start the cluster (register node, start watching, etc.)
	err = c.Start(ctx, n)
	if err != nil {
		n.Stop(ctx) // Clean up node if cluster start fails
		t.Fatalf("Failed to start cluster: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		c.Stop(ctx)
		n.Stop(ctx)
	})

	return c
}
