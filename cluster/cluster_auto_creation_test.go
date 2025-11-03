package cluster

import (
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

