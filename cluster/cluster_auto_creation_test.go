package cluster

import (
	"context"
	"testing"
)

// TestConnectEtcd_AutoCreatesManagers verifies that ConnectEtcd auto-creates
// the etcd manager and consensus manager when they don't exist
func TestConnectEtcd_AutoCreatesManagers(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestAutoCreation")

	// Initially, managers should be nil
	if cluster.etcdManager != nil {
		t.Error("etcdManager should be nil initially")
	}
	if cluster.consensusManager != nil {
		t.Error("consensusManager should be nil initially")
	}

	// Try to connect to etcd with address and prefix
	// This should fail to actually connect (no etcd running), but it should
	// create the managers
	err := cluster.ConnectEtcd("localhost:2379", "/test-prefix")

	// We expect an error because etcd is not running
	if err == nil {
		t.Log("Warning: etcd appears to be running, test may not validate creation properly")
	}

	// But the managers should have been created
	if cluster.etcdManager == nil {
		t.Error("etcdManager should be created after ConnectEtcd call")
	}
	if cluster.consensusManager == nil {
		t.Error("consensusManager should be created after ConnectEtcd call")
	}

	// Verify the addresses were stored
	if cluster.etcdAddress != "localhost:2379" {
		t.Errorf("etcdAddress = %s; want localhost:2379", cluster.etcdAddress)
	}
	if cluster.etcdPrefix != "/test-prefix" {
		t.Errorf("etcdPrefix = %s; want /test-prefix", cluster.etcdPrefix)
	}
}

// TestEnsureEtcdManager_WithoutAddress verifies that ensureEtcdManager
// returns an error when no address is configured
func TestEnsureEtcdManager_WithoutAddress(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestEnsureNoAddress")

	// Try to ensure etcd manager without setting address
	err := cluster.ensureEtcdManager()

	if err == nil {
		t.Error("ensureEtcdManager should return error when etcd address is not set")
	}

	expectedErr := "etcd address not set"
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

// TestStartWatching_AutoCreatesManagers verifies that StartWatching
// auto-creates managers when called with proper etcd configuration
func TestStartWatching_AutoCreatesManagers(t *testing.T) {
	// Create a new cluster for testing
	cluster := newClusterForTesting("TestStartWatchingAuto")

	ctx := context.Background()

	// First, set the etcd address
	cluster.etcdAddress = "localhost:2379"
	cluster.etcdPrefix = "/test-watching"

	// Try to start watching - this should fail (no etcd), but should create managers
	err := cluster.StartWatching(ctx)

	// We expect an error because etcd is not running
	if err == nil {
		t.Log("Warning: etcd appears to be running")
	}

	// But the managers should have been created
	if cluster.etcdManager == nil {
		t.Error("etcdManager should be created after StartWatching call")
	}
	if cluster.consensusManager == nil {
		t.Error("consensusManager should be created after StartWatching call")
	}
}
