package uniqueid

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/rand"
	"time"
)

// UniqueId generates a unique identifier string.
// The generated ID is guaranteed to never contain '/' or '#' characters,
// which are reserved for special object ID routing formats:
//   - Fixed-node format: "nodeAddr/uuid"
//   - Fixed-shard format: "shard#N/uuid"
//
// The function uses base64 URL encoding (RFC 4648) which produces only
// alphanumeric characters plus '-' and '_', ensuring the generated IDs
// are safe to use as object identifiers without conflicting with routing logic.
func UniqueId() string {
	b := make([]byte, 16)

	ts := time.Now().UnixMicro()
	binary.BigEndian.PutUint64(b[:8], uint64(ts))

	_, err := rand.Read(b[8:])
	if err != nil {
		panic(err)
	}

	encBuffer := make([]byte, 0, 32)
	encBufferWriter := bytes.NewBuffer(encBuffer)
	encoder := base64.NewEncoder(base64.URLEncoding, encBufferWriter)
	defer encoder.Close()

	_, err = encoder.Write(b)
	if err != nil {
		panic(err)
	}

	return encBufferWriter.String()
}

// CreateObjectID creates a normal object ID using a unique identifier.
// The object will be distributed to a node based on hash-based sharding.
//
// Example:
//
//	objID := uniqueid.CreateObjectID()
//	// Returns something like "AZGvL8S3gAABj2nMZzE"
func CreateObjectID() string {
	return UniqueId()
}

// CreateObjectIDOnShard creates an object ID that will be placed on a specific shard.
// The format is "shard#<shardID>/<objectID>".
// The shard ID must be in the valid range [0, numShards).
//
// Example:
//
//	objID := uniqueid.CreateObjectIDOnShard(5, 8192)
//	// Returns something like "shard#5/AZGvL8S3gAABj2nMZzE"
func CreateObjectIDOnShard(shardID int, numShards int) string {
	if shardID < 0 || shardID >= numShards {
		panic(fmt.Sprintf("shardID %d out of range [0, %d)", shardID, numShards))
	}
	return fmt.Sprintf("shard#%d/%s", shardID, UniqueId())
}

// CreateObjectIDOnNode creates an object ID that will be placed on a specific node.
// The format is "<nodeAddress>/<objectID>".
// The node address should be in the format "host:port".
//
// Example:
//
//	objID := uniqueid.CreateObjectIDOnNode("localhost:7001")
//	// Returns something like "localhost:7001/AZGvL8S3gAABj2nMZzE"
func CreateObjectIDOnNode(nodeAddress string) string {
	if nodeAddress == "" {
		panic("nodeAddress cannot be empty")
	}
	return fmt.Sprintf("%s/%s", nodeAddress, UniqueId())
}
