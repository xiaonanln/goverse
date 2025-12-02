package sharding

import (
	"hash/fnv"
	"strconv"
	"strings"
)

const NumShards = 8192

// GetShardID computes the shard ID for a given object ID using FNV-1a hash
// If the object ID is in fixed-shard format (shard#<shardID>/<objectID>),
// the shard ID is extracted directly from the object ID.
func GetShardID(objectID string, numShards int) int {
	// Check if object ID specifies a fixed shard
	// Format: shard#<shardID>/<objectID> (e.g., "shard#5/object-123")
	if strings.HasPrefix(objectID, "shard#") {
		// Find the position of the slash
		slashIdx := strings.Index(objectID, "/")
		if slashIdx > 6 { // "shard#" is 6 chars, need at least one digit
			// Extract shard ID between "shard#" and "/"
			shardIDStr := objectID[6:slashIdx]
			if shardID, err := strconv.Atoi(shardIDStr); err == nil {
				// Validate shard ID is within valid range
				if shardID >= 0 && shardID < numShards {
					return shardID
				}
			}
		}
	}

	// Use FNV-1a hash for regular object IDs
	hasher := fnv.New32a()
	hasher.Write([]byte(objectID))
	hashValue := hasher.Sum32()
	return int(hashValue) % numShards
}
