package cluster

import (
	"time"

	"github.com/xiaonanln/goverse/config"
)

// Config holds cluster-level configuration parameters
type Config struct {
	// EtcdAddress is the etcd server address (e.g., "localhost:2379")
	EtcdAddress string

	// EtcdPrefix is the etcd key prefix for this cluster (e.g., "/goverse")
	EtcdPrefix string

	// MinQuorum is the minimal number of nodes required for cluster to be considered stable
	// Default: 1
	MinQuorum int

	// ClusterStateStabilityDuration is how long the node list must be stable before updating shard mapping
	// Default: 10 seconds
	ClusterStateStabilityDuration time.Duration

	// ShardMappingCheckInterval is how often to check if shard mapping needs updating
	// Default: 5 seconds
	ShardMappingCheckInterval time.Duration

	// NumShards is the number of shards in the cluster
	// Default: 8192
	NumShards int

	// RebalanceShardsBatchSize is the maximum number of shards to migrate in a single rebalance operation
	// Default: max(1, NumShards / 128)
	RebalanceShardsBatchSize int

	// ImbalanceThreshold is the threshold for shard imbalance as a fraction of ideal load
	// Default: 0.2 (20% of ideal load)
	ImbalanceThreshold float64

	// AutoLoadObjects is the list of objects to automatically load when the node starts
	AutoLoadObjects []config.AutoLoadObjectConfig

	// DefaultCallTimeout is the default timeout for CallObject operations.
	// If zero, falls back to the hardcoded default (30s).
	DefaultCallTimeout time.Duration

	// DefaultCreateTimeout is the default timeout for CreateObject operations.
	// If zero, falls back to the hardcoded default (30s).
	DefaultCreateTimeout time.Duration

	// DefaultDeleteTimeout is the default timeout for DeleteObject operations.
	// If zero, falls back to the hardcoded default (30s).
	DefaultDeleteTimeout time.Duration

	// ConfigFile is the loaded config file (if any)
	// If nil, the cluster was started with CLI flags instead of a config file
	ConfigFile *config.Config
}

// DefaultConfig returns a Config with production default values
func DefaultConfig() Config {
	numShards := 8192
	return Config{
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 10 * time.Second,
		ShardMappingCheckInterval:     5 * time.Second,
		NumShards:                     numShards,
		RebalanceShardsBatchSize:      max(1, numShards/128),
		ImbalanceThreshold:            0.2,
		DefaultCallTimeout:            30 * time.Second,
		DefaultCreateTimeout:          30 * time.Second,
		DefaultDeleteTimeout:          30 * time.Second,
	}
}
