package main

// This example demonstrates using fixed node addresses for object IDs.
// Object IDs can now use the format "nodeAddress/objectID" (e.g., "localhost:7001/my-object")
// to pin objects to specific nodes, similar to how client IDs work.

import (
	"fmt"
	"strings"
)

// getNodeForFixedAddress extracts the node address from object IDs with fixed node format
func getNodeForFixedAddress(objectID string) (string, bool) {
	if strings.Contains(objectID, "/") {
		parts := strings.SplitN(objectID, "/", 2)
		if len(parts) >= 1 && parts[0] != "" {
			return parts[0], true
		}
	}
	return "", false
}

func main() {
	fmt.Println("Fixed Node Address Example for Object IDs")
	fmt.Println("==========================================")
	fmt.Println()

	// Example 1: Object with fixed node address
	objectID1 := "localhost:7001/my-special-object"
	if node, ok := getNodeForFixedAddress(objectID1); ok {
		fmt.Printf("Object ID: %s\n", objectID1)
		fmt.Printf("Target Node: %s\n", node)
		fmt.Println()
	}

	// Example 2: Another object with different fixed node
	objectID2 := "192.168.1.100:8080/session-abc123"
	if node, ok := getNodeForFixedAddress(objectID2); ok {
		fmt.Printf("Object ID: %s\n", objectID2)
		fmt.Printf("Target Node: %s\n", node)
		fmt.Println()
	}

	// Example 3: Object ID with multiple slashes (path-like structure)
	objectID3 := "localhost:7001/users/sessions/abc-123"
	if node, ok := getNodeForFixedAddress(objectID3); ok {
		fmt.Printf("Object ID: %s\n", objectID3)
		fmt.Printf("Target Node: %s\n", node)
		fmt.Printf("Note: Everything after first '/' is the object identifier\n")
		fmt.Println()
	}

	// Example 4: Regular object ID without fixed node (would use shard mapping)
	objectID4 := "regular-object-without-fixed-node"
	fmt.Printf("Object ID: %s\n", objectID4)
	if _, ok := getNodeForFixedAddress(objectID4); !ok {
		fmt.Printf("Note: This uses shard-based routing (requires shard mapping setup)\n")
	}
	fmt.Println()

	fmt.Println("Summary:")
	fmt.Println("- Object IDs with '/' separator pin objects to specific nodes")
	fmt.Println("- The first '/' separates node address from object identifier")
	fmt.Println("- Object IDs can contain additional slashes (paths)")
	fmt.Println("- Regular object IDs use shard-based routing")
	fmt.Println("- This is similar to how client IDs work in the system")
}
