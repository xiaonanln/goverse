package cluster

import (
	"time"
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

	// DefaultPushMessageTimeout is the default timeout for PushMessageToClient operations.
	// Push delivery should be immediate or fail fast.
	// Default: 5 seconds
	DefaultPushMessageTimeout time.Duration
}

// DefaultPushMessageTimeout is the default timeout for PushMessageToClient operations.
// Push delivery should be immediate or fail fast (5 seconds per TIMEOUT_DESIGN.md).
const DefaultPushMessageTimeout = 5 * time.Second

// DefaultConfig returns a Config with production default values
func DefaultConfig() Config {
	return Config{
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 10 * time.Second,
		ShardMappingCheckInterval:     5 * time.Second,
		NumShards:                     8192,
		DefaultPushMessageTimeout:     DefaultPushMessageTimeout,
	}
}
