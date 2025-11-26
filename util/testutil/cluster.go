package testutil

import (
	"context"
	"testing"
	"time"
)

// WaitForClustersReadyTimeout is the timeout for WaitForClustersReady
const WaitForClustersReadyTimeout = 60 * time.Second

// ClusterReadyWaiter is an interface for waiting on cluster readiness.
// This allows testutil to work with cluster types without importing the cluster package.
type ClusterReadyWaiter interface {
	// ClusterReady returns a channel that closes when the cluster is ready
	ClusterReady() <-chan bool
	String() string
}

// FullClusterChecker extends ClusterReadyWaiter with additional methods needed
// for comprehensive cluster readiness verification in tests.
// This interface allows WaitForClustersReady to perform thorough readiness checks
// without importing the cluster package.
type FullClusterChecker interface {
	ClusterReadyWaiter
	// GetNodes returns a list of all registered nodes in the cluster
	GetNodes() []string
	// GetGates returns a list of all registered gates in the cluster
	GetGates() []string
	// IsReady returns true if the cluster is ready
	IsReady() bool
	// IsGateConnected returns true if the specified gate is connected to this node
	// This only applies to node clusters; gate clusters always return false
	IsGateConnected(gateAddr string) bool
	// GetAdvertiseAddress returns the advertise address of this cluster
	GetAdvertiseAddress() string
	// IsNode returns true if this cluster is a node cluster
	IsNode() bool
	// IsGate returns true if this cluster is a gate cluster
	IsGate() bool
	// IsShardMappingComplete returns true if all shards have CurrentNode == TargetNode
	// numShards is the expected total number of shards
	IsShardMappingComplete(ctx context.Context, numShards int) bool
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

// WaitForClustersReady waits for multiple clusters (nodes and gates) to be fully ready.
// This function performs comprehensive readiness checks including:
//   - All nodes are connected to each other
//   - All gates are registered to all nodes
//   - All clusters have a stable state
//   - All gates and nodes are visible in the cluster state
//   - All shard mappings are complete (CurrentNode == TargetNode for all shards)
//
// The function uses a 60 second timeout (WaitForClustersReadyTimeout).
//
// Usage:
//
//	testutil.WaitForClustersReady(t, nodeCluster1, nodeCluster2, gateCluster)
//
// Parameters:
//   - t: The testing.TB instance for the current test
//   - clusters: Variadic list of clusters implementing FullClusterChecker
func WaitForClustersReady(t testing.TB, clusters ...FullClusterChecker) {
	t.Helper()

	if len(clusters) == 0 {
		t.Fatal("WaitForClustersReady requires at least one cluster")
	}

	ctx := context.Background()

	// Collect expected nodes and gates
	var expectedNodes []string
	var expectedGates []string
	for _, c := range clusters {
		if c.IsNode() {
			expectedNodes = append(expectedNodes, c.GetAdvertiseAddress())
		} else if c.IsGate() {
			expectedGates = append(expectedGates, c.GetAdvertiseAddress())
		} else {
			t.Fatalf("WaitForClustersReady: cluster %s is neither a node nor a gate", c.String())
		}
	}

	t.Logf("WaitForClustersReady: waiting for %d nodes and %d gates to be fully ready",
		len(expectedNodes), len(expectedGates))

	// Use WaitFor with comprehensive checks
	WaitFor(t, WaitForClustersReadyTimeout, "all clusters to be fully ready", func() bool {
		// Check 1: All clusters are marked as ready
		for _, c := range clusters {
			if !c.IsReady() {
				t.Logf("  Cluster %s is not ready yet", c.String())
				return false
			}
		}

		// Check 2: All clusters see all expected nodes and gates in their state
		for _, c := range clusters {
			nodes := c.GetNodes()
			gates := c.GetGates()

			// Check that all expected nodes are visible
			nodeSet := make(map[string]bool)
			for _, n := range nodes {
				nodeSet[n] = true
			}
			for _, expectedNode := range expectedNodes {
				if !nodeSet[expectedNode] {
					t.Logf("  Cluster %s doesn't see node %s in cluster state", c.String(), expectedNode)
					return false
				}
			}

			// Check that all expected gates are visible
			gateSet := make(map[string]bool)
			for _, g := range gates {
				gateSet[g] = true
			}
			for _, expectedGate := range expectedGates {
				if !gateSet[expectedGate] {
					t.Logf("  Cluster %s doesn't see gate %s in cluster state", c.String(), expectedGate)
					return false
				}
			}
		}

		// Check 3: All gates are registered (connected) to all nodes
		for _, c := range clusters {
			if c.IsNode() {
				for _, gateAddr := range expectedGates {
					if !c.IsGateConnected(gateAddr) {
						t.Logf("  Node %s doesn't have gate %s connected", c.String(), gateAddr)
						return false
					}
				}
			}
		}

		// Check 4: All shard mappings are complete (CurrentNode == TargetNode)
		for _, c := range clusters {
			if !c.IsShardMappingComplete(ctx, TestNumShards) {
				t.Logf("  Cluster %s shard mapping is not complete", c.String())
				return false
			}
		}

		return true
	})

	t.Logf("WaitForClustersReady: all %d clusters are fully ready", len(clusters))
}
