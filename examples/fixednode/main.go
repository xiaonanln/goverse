package main

// This example demonstrates using fixed node addresses for object IDs.
// Object IDs can now use the format "nodeAddress/objectID" (e.g., "localhost:7001/my-object")
// to pin objects to specific nodes, similar to how client IDs work.

import (
	"context"
	"fmt"
	"log"

	"github.com/xiaonanln/goverse/cluster/sharding"
)

func main() {
	sm := sharding.NewShardMapper(nil)
	ctx := context.Background()

	fmt.Println("Fixed Node Address Example for Object IDs")
	fmt.Println("==========================================")
	fmt.Println()

	// Example 1: Object with fixed node address
	objectID1 := "localhost:7001/my-special-object"
	node1, err := sm.GetNodeForObject(ctx, objectID1)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Printf("Object ID: %s\n", objectID1)
	fmt.Printf("Target Node: %s\n", node1)
	fmt.Println()

	// Example 2: Another object with different fixed node
	objectID2 := "192.168.1.100:8080/session-abc123"
	node2, err := sm.GetNodeForObject(ctx, objectID2)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Printf("Object ID: %s\n", objectID2)
	fmt.Printf("Target Node: %s\n", node2)
	fmt.Println()

	// Example 3: Regular object ID without fixed node (would use shard mapping)
	// Note: This would fail without a configured shard mapping, but shows the syntax
	objectID3 := "regular-object-without-fixed-node"
	fmt.Printf("Object ID: %s\n", objectID3)
	fmt.Printf("Note: This uses shard-based routing (requires shard mapping setup)\n")
	fmt.Println()

	fmt.Println("Summary:")
	fmt.Println("- Object IDs with '/' separator pin objects to specific nodes")
	fmt.Println("- Format: nodeAddress/objectID")
	fmt.Println("- Regular object IDs use shard-based routing")
	fmt.Println("- This is similar to how client IDs work in the system")
}
