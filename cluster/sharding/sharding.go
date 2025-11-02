package sharding

import (
	"hash/fnv"
)

const NumShards = 8192

// ShardMapping represents the mapping of shards to nodes
// Note: Nodes and Version fields have been moved to ClusterState in consensusmanager
type ShardMapping struct {
	// Map from shard ID to node address
	Shards map[int]string `json:"shards"`
}

// GetShardID computes the shard ID for a given object ID using FNV-1a hash
func GetShardID(objectID string) int {
	hasher := fnv.New32a()
	hasher.Write([]byte(objectID))
	hashValue := hasher.Sum32()
	return int(hashValue) % NumShards
}
