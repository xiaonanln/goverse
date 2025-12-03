package main

import (
	"fmt"

	"github.com/xiaonanln/goverse/goverseapi"
)

// This example demonstrates the three ways to create object IDs in Goverse:
// 1. Normal object IDs - distributed using hash-based sharding
// 2. Fixed-shard object IDs - pinned to a specific shard
// 3. Fixed-node object IDs - pinned to a specific node

func main() {
	fmt.Println("Goverse Object ID Creation Examples")
	fmt.Println("====================================")
	fmt.Println()

	// Method 1: Create a normal object ID
	// This will be distributed to a node based on hash-based sharding
	fmt.Println("1. Normal Object ID (Hash-Based Sharding)")
	normalID1 := goverseapi.CreateObjectID()
	normalID2 := goverseapi.CreateObjectID()
	normalID3 := goverseapi.CreateObjectID()

	fmt.Printf("   ID 1: %s\n", normalID1)
	fmt.Printf("   ID 2: %s\n", normalID2)
	fmt.Printf("   ID 3: %s\n", normalID3)
	fmt.Println("   Note: Each ID is unique and will be placed on a node based on consistent hashing")
	fmt.Println()

	// Method 2: Create object ID on a specific shard
	// Format: shard#<shardID>/<uniqueID>
	fmt.Println("2. Fixed-Shard Object ID")
	fmt.Println("   (Object is pinned to a specific shard)")
	shardID1 := 5
	shardID2 := 10
	fixedShardID1 := goverseapi.CreateObjectIDOnShard(shardID1)
	fixedShardID2 := goverseapi.CreateObjectIDOnShard(shardID2)
	fixedShardID3 := goverseapi.CreateObjectIDOnShard(shardID1) // Another object on shard 5

	fmt.Printf("   Shard %d ID: %s\n", shardID1, fixedShardID1)
	fmt.Printf("   Shard %d ID: %s\n", shardID2, fixedShardID2)
	fmt.Printf("   Shard %d ID: %s\n", shardID1, fixedShardID3)
	fmt.Println("   Note: Objects with the same shard number will be on the same node")
	fmt.Println()

	// Method 3: Create object ID on a specific node
	// Format: <nodeAddress>/<uniqueID>
	fmt.Println("3. Fixed-Node Object ID")
	fmt.Println("   (Object is pinned to a specific node)")
	nodeAddr1 := "localhost:7001"
	nodeAddr2 := "192.168.1.100:8080"
	fixedNodeID1 := goverseapi.CreateObjectIDOnNode(nodeAddr1)
	fixedNodeID2 := goverseapi.CreateObjectIDOnNode(nodeAddr2)
	fixedNodeID3 := goverseapi.CreateObjectIDOnNode(nodeAddr1) // Another object on the same node

	fmt.Printf("   Node %s ID: %s\n", nodeAddr1, fixedNodeID1)
	fmt.Printf("   Node %s ID: %s\n", nodeAddr2, fixedNodeID2)
	fmt.Printf("   Node %s ID: %s\n", nodeAddr1, fixedNodeID3)
	fmt.Println("   Note: Objects with the same node address will always be on that specific node")
	fmt.Println()

	// Usage example
	fmt.Println("Usage Example:")
	fmt.Println("==============")
	fmt.Println()
	fmt.Println("// Create a normal object")
	fmt.Println("objID := goverseapi.CreateObjectID()")
	fmt.Println("goverseapi.CreateObject(ctx, \"MyObjectType\", objID)")
	fmt.Println()
	fmt.Println("// Create an object on shard 5")
	fmt.Println("objID := goverseapi.CreateObjectIDOnShard(5)")
	fmt.Println("goverseapi.CreateObject(ctx, \"MyObjectType\", objID)")
	fmt.Println()
	fmt.Println("// Create an object on a specific node")
	fmt.Println("objID := goverseapi.CreateObjectIDOnNode(\"localhost:7001\")")
	fmt.Println("goverseapi.CreateObject(ctx, \"MyObjectType\", objID)")
	fmt.Println()

	fmt.Println("Summary:")
	fmt.Println("========")
	fmt.Println("- Normal IDs: Use for general distributed objects")
	fmt.Println("- Fixed-shard IDs: Use when you want objects on the same shard (e.g., related objects)")
	fmt.Println("- Fixed-node IDs: Use when you need objects on a specific node (e.g., node-local resources)")
	fmt.Println()
	fmt.Println("Note: By default, Goverse uses 8192 shards for production deployments.")
}
