package testutil

import (
	"testing"
	"time"
)

// ClusterReadyWaiter is an interface for waiting on cluster readiness.
// This allows testutil to work with cluster types without importing the cluster package.
type ClusterReadyWaiter interface {
	// ClusterReady returns a channel that closes when the cluster is ready
	ClusterReady() <-chan bool
	String() string
}

// WaitForClusterReady waits for a cluster to become ready or fails the test on timeout.
// It returns immediately if the cluster is already ready.
// If the cluster does not become ready within WaitForShardMappingTimeout, it calls t.Fatal.
//
// Usage:
//
//	cluster := mustNewCluster(ctx, t, "localhost:47101", testPrefix)
//	testutil.WaitForClusterReady(t, cluster)
//
// Parameters:
//   - t: The testing.TB instance for the current test (testing.T or testing.B)
//   - cluster: Any type that implements ClusterReadyWaiter (e.g., *cluster.Cluster)
func WaitForClusterReady(t testing.TB, cluster ClusterReadyWaiter) {
	t.Helper()

	t.Logf("Waiting for cluster to become ready: %s ...", cluster.String())

	select {
	case <-cluster.ClusterReady():
		// Cluster is ready
		return
	case <-time.After(WaitForShardMappingTimeout):
		t.Fatalf("Cluster %s did not become ready within %v", cluster.String(), WaitForShardMappingTimeout)
	}
}
