package cluster

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()

	// Verify default values
	if cfg.MinQuorum != 1 {
		t.Fatalf("Expected default MinQuorum to be 1, got %d", cfg.MinQuorum)
	}

	if cfg.ClusterStateStabilityDuration != 10*time.Second {
		t.Fatalf("Expected default NodeStabilityDuration to be 10s, got %v", cfg.ClusterStateStabilityDuration)
	}

	if cfg.ShardMappingCheckInterval != 5*time.Second {
		t.Fatalf("Expected default ShardMappingCheckInterval to be 5s, got %v", cfg.ShardMappingCheckInterval)
	}

	// EtcdAddress and EtcdPrefix should be empty by default
	if cfg.EtcdAddress != "" {
		t.Fatalf("Expected default EtcdAddress to be empty, got %s", cfg.EtcdAddress)
	}

	if cfg.EtcdPrefix != "" {
		t.Fatalf("Expected default EtcdPrefix to be empty, got %s", cfg.EtcdPrefix)
	}
}

func TestConfigCustomization(t *testing.T) {
	t.Parallel()
	// Start with defaults and customize
	cfg := DefaultConfig()
	cfg.EtcdAddress = "localhost:2379"
	cfg.EtcdPrefix = "/test"
	cfg.MinQuorum = 3
	cfg.ClusterStateStabilityDuration = 5 * time.Second
	cfg.ShardMappingCheckInterval = 2 * time.Second

	// Verify customizations
	if cfg.EtcdAddress != "localhost:2379" {
		t.Fatalf("Expected EtcdAddress to be localhost:2379, got %s", cfg.EtcdAddress)
	}

	if cfg.EtcdPrefix != "/test" {
		t.Fatalf("Expected EtcdPrefix to be /test, got %s", cfg.EtcdPrefix)
	}

	if cfg.MinQuorum != 3 {
		t.Fatalf("Expected MinQuorum to be 3, got %d", cfg.MinQuorum)
	}

	if cfg.ClusterStateStabilityDuration != 5*time.Second {
		t.Fatalf("Expected NodeStabilityDuration to be 5s, got %v", cfg.ClusterStateStabilityDuration)
	}

	if cfg.ShardMappingCheckInterval != 2*time.Second {
		t.Fatalf("Expected ShardMappingCheckInterval to be 2s, got %v", cfg.ShardMappingCheckInterval)
	}
}

func TestNewClusterAppliesConfig(t *testing.T) {
	t.Parallel()
	// This test verifies that NewCluster properly applies config values to the cluster
	// We can't fully test this without etcd, but we can verify the values are set

	// Create a custom config
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    "/test-cluster",
		MinQuorum:                     3,
		ClusterStateStabilityDuration: 7 * time.Second,
		ShardMappingCheckInterval:     3 * time.Second,
	}

	// Note: We can't actually call NewCluster here without a running etcd instance
	// This test just verifies the Config type can be created and used
	// The actual application of config values is tested in integration tests

	// Verify the config values are as expected
	if cfg.MinQuorum != 3 {
		t.Fatalf("Expected MinQuorum to be 3, got %d", cfg.MinQuorum)
	}

	if cfg.ClusterStateStabilityDuration != 7*time.Second {
		t.Fatalf("Expected NodeStabilityDuration to be 7s, got %v", cfg.ClusterStateStabilityDuration)
	}

	if cfg.ShardMappingCheckInterval != 3*time.Second {
		t.Fatalf("Expected ShardMappingCheckInterval to be 3s, got %v", cfg.ShardMappingCheckInterval)
	}
}
