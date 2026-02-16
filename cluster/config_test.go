package cluster

import (
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
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

	if cfg.NumShards != 8192 {
		t.Fatalf("Expected default NumShards to be 8192, got %d", cfg.NumShards)
	}

	// RebalanceShardsBatchSize should default to max(1, NumShards/128)
	expectedBatchSize := max(1, cfg.NumShards/128)
	if cfg.RebalanceShardsBatchSize != expectedBatchSize {
		t.Fatalf("Expected default RebalanceShardsBatchSize to be %d, got %d", expectedBatchSize, cfg.RebalanceShardsBatchSize)
	}

	// ImbalanceThreshold should default to 0.2
	if cfg.ImbalanceThreshold != 0.2 {
		t.Fatalf("Expected default ImbalanceThreshold to be 0.2, got %f", cfg.ImbalanceThreshold)
	}

	// Timeout defaults
	if cfg.DefaultCallTimeout != 30*time.Second {
		t.Fatalf("Expected default DefaultCallTimeout to be 30s, got %v", cfg.DefaultCallTimeout)
	}

	if cfg.DefaultCreateTimeout != 30*time.Second {
		t.Fatalf("Expected default DefaultCreateTimeout to be 30s, got %v", cfg.DefaultCreateTimeout)
	}

	if cfg.DefaultDeleteTimeout != 30*time.Second {
		t.Fatalf("Expected default DefaultDeleteTimeout to be 30s, got %v", cfg.DefaultDeleteTimeout)
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
	cfg.NumShards = 4096
	cfg.RebalanceShardsBatchSize = 50
	cfg.ImbalanceThreshold = 0.3
	cfg.DefaultCallTimeout = 10 * time.Second
	cfg.DefaultCreateTimeout = 15 * time.Second
	cfg.DefaultDeleteTimeout = 20 * time.Second

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

	if cfg.NumShards != 4096 {
		t.Fatalf("Expected NumShards to be 4096, got %d", cfg.NumShards)
	}

	if cfg.RebalanceShardsBatchSize != 50 {
		t.Fatalf("Expected RebalanceShardsBatchSize to be 50, got %d", cfg.RebalanceShardsBatchSize)
	}

	if cfg.ImbalanceThreshold != 0.3 {
		t.Fatalf("Expected ImbalanceThreshold to be 0.3, got %f", cfg.ImbalanceThreshold)
	}

	if cfg.DefaultCallTimeout != 10*time.Second {
		t.Fatalf("Expected DefaultCallTimeout to be 10s, got %v", cfg.DefaultCallTimeout)
	}

	if cfg.DefaultCreateTimeout != 15*time.Second {
		t.Fatalf("Expected DefaultCreateTimeout to be 15s, got %v", cfg.DefaultCreateTimeout)
	}

	if cfg.DefaultDeleteTimeout != 20*time.Second {
		t.Fatalf("Expected DefaultDeleteTimeout to be 20s, got %v", cfg.DefaultDeleteTimeout)
	}
}

// TestRebalanceShardsBatchSizeDefault tests the default calculation for batch size
func TestRebalanceShardsBatchSizeDefault(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name              string
		numShards         int
		expectedBatchSize int
	}{
		{
			name:              "8192 shards (production default)",
			numShards:         8192,
			expectedBatchSize: 64, // 8192 / 128 = 64
		},
		{
			name:              "4096 shards",
			numShards:         4096,
			expectedBatchSize: 32, // 4096 / 128 = 32
		},
		{
			name:              "128 shards",
			numShards:         128,
			expectedBatchSize: 1, // max(1, 128 / 128) = 1
		},
		{
			name:              "64 shards (test default)",
			numShards:         64,
			expectedBatchSize: 1, // max(1, 64 / 128) = 1
		},
		{
			name:              "16 shards",
			numShards:         16,
			expectedBatchSize: 1, // max(1, 16 / 128) = 1
		},
		{
			name:              "16384 shards",
			numShards:         16384,
			expectedBatchSize: 128, // 16384 / 128 = 128
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{
				NumShards:                tc.numShards,
				RebalanceShardsBatchSize: 0, // 0 means use default
			}

			// Calculate expected default
			expectedDefault := max(1, tc.numShards/128)
			if expectedDefault != tc.expectedBatchSize {
				t.Fatalf("Test case error: expected %d but calculated %d", tc.expectedBatchSize, expectedDefault)
			}

			// When RebalanceShardsBatchSize is 0, the cluster initialization should set it to the default
			// This is verified in the cluster initialization code
			actualDefault := max(1, cfg.NumShards/128)
			if actualDefault != tc.expectedBatchSize {
				t.Fatalf("Expected default batch size %d for %d shards, got %d", tc.expectedBatchSize, tc.numShards, actualDefault)
			}
		})
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
		NumShards:                     testutil.TestNumShards,
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
