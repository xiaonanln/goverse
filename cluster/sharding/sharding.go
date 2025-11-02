package sharding

import (
	"hash/fnv"
)

const NumShards = 8192

// GetShardID computes the shard ID for a given object ID using FNV-1a hash
func GetShardID(objectID string) int {
	hasher := fnv.New32a()
	hasher.Write([]byte(objectID))
	hashValue := hasher.Sum32()
	return int(hashValue) % NumShards
}
