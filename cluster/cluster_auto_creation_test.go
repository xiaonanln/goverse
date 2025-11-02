package cluster

import (
	"context"
	"testing"
)

// TestInitializeEtcdForTesting_CreatesManagers verifies that initializeEtcdForTesting
// creates the etcd manager and consensus manager
func TestInitializeEtcdForTesting_CreatesManagers(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestInitialization")

	// Initially, managers should be nil
	if cluster.etcdManager != nil {
		t.Error("etcdManager should be nil initially")
	}
	if cluster.consensusManager != nil {
		t.Error("consensusManager should be nil initially")
	}

	// Try to initialize etcd with address and prefix
	// This should fail to actually connect (no etcd running), but it should
	// create the managers
	err := cluster.initializeEtcdForTesting("localhost:2379", "/test-prefix")

	// We expect an error because etcd is not running
	if err == nil {
		t.Log("Warning: etcd appears to be running, test may not validate creation properly")
	}

	// But the managers should have been created
	if cluster.etcdManager == nil {
		t.Error("etcdManager should be created after initializeEtcdForTesting call")
	}
	if cluster.consensusManager == nil {
		t.Error("consensusManager should be created after initializeEtcdForTesting call")
	}

	// Verify the addresses were stored
	if cluster.etcdAddress != "localhost:2379" {
		t.Errorf("etcdAddress = %s; want localhost:2379", cluster.etcdAddress)
	}
	if cluster.etcdPrefix != "/test-prefix" {
		t.Errorf("etcdPrefix = %s; want /test-prefix", cluster.etcdPrefix)
	}
}

// TestEnsureEtcdManager_WithoutInitialization verifies that ensureEtcdManager
// returns an error when cluster hasn't been initialized with NewCluster
func TestEnsureEtcdManager_WithoutInitialization(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestEnsureNotInitialized")

	// Try to ensure etcd manager without initializing
	err := cluster.ensureEtcdManager()

	if err == nil {
		t.Error("ensureEtcdManager should return error when cluster is not initialized")
	}

	expectedErr := "cluster not initialized - call NewCluster first"
	if err.Error() != expectedErr {
		t.Errorf("ensureEtcdManager error = %v; want %v", err.Error(), expectedErr)
	}
}

// TestGetNodes_LazyInitialization verifies that GetNodes works even when
// managers haven't been explicitly created
func TestGetNodes_LazyInitialization(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestGetNodesLazy")

	// GetNodes should work even without etcd manager being set
	// It should return empty list gracefully
	nodes := cluster.GetNodes()

	if nodes == nil {
		t.Error("GetNodes should not return nil")
	}

	if len(nodes) != 0 {
		t.Errorf("GetNodes should return empty list when no nodes, got %d nodes", len(nodes))
	}
}

// TestStartWatching_RequiresInitialization verifies that StartWatching
// requires cluster to be initialized first
func TestStartWatching_RequiresInitialization(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestStartWatchingInit")

	ctx := context.Background()

	// Try to start watching without initializing - should fail
	err := cluster.StartWatching(ctx)

	if err == nil {
		t.Error("StartWatching should return error when cluster is not initialized")
	}

	expectedErr := "cluster not initialized - call NewCluster first"
	if err.Error() != expectedErr {
		t.Errorf("StartWatching error = %v; want %v", err.Error(), expectedErr)
	}
}
