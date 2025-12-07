// Example demonstrating the GetNodesInfo and GetGatesInfo APIs
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
)

func main() {
	// Note: In a real application, you would start the server first using:
	// server := goverseapi.NewServer()
	// go server.Run(ctx)

	// This example assumes a server is already running in the cluster
	// For demonstration purposes, we'll show the API usage patterns

	fmt.Println("Cluster Info Example")
	fmt.Println("====================")
	fmt.Println()

	// Wait for cluster to be ready (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	select {
	case <-goverseapi.ClusterReady():
		fmt.Println("✓ Cluster is ready!")
	case <-ctx.Done():
		fmt.Println("⚠ Cluster not ready (this is expected if no server is running)")
		fmt.Println("\nTo run this example with a live cluster:")
		fmt.Println("1. Start etcd: docker run -d -p 2379:2379 quay.io/coreos/etcd:v3.5.0 ...")
		fmt.Println("2. Start a node: go run cmd/node/main.go --listen :48000 --etcd localhost:2379")
		fmt.Println("3. Then run this example again")
		fmt.Println()
		fmt.Println("Showing API usage below:")
		fmt.Println()
	}

	// Get nodes information
	nodesInfo := goverseapi.GetNodesInfo()
	fmt.Println("\n=== Nodes Information ===")
	if len(nodesInfo) == 0 {
		fmt.Println("No nodes found")
	} else {
		for addr, info := range nodesInfo {
			fmt.Printf("Node: %s\n", addr)
			fmt.Printf("  Configured: %v (from config file)\n", info.Configured)
			fmt.Printf("  IsAlive: %v (registered in etcd)\n", info.IsAlive)
			fmt.Printf("  IsLeader: %v (cluster leader)\n", info.IsLeader)
			fmt.Println()
		}
	}

	// Get gates information
	gatesInfo := goverseapi.GetGatesInfo()
	fmt.Println("=== Gates Information ===")
	if len(gatesInfo) == 0 {
		fmt.Println("No gates found")
	} else {
		for addr, info := range gatesInfo {
			fmt.Printf("Gate: %s\n", addr)
			fmt.Printf("  Configured: %v (from config file)\n", info.Configured)
			fmt.Printf("  Found in Cluster: %v (registered in etcd)\n", info.IsAlive)
			fmt.Println()
		}
	}

	// In a real application, you might want to periodically check this information
	// or trigger alerts if configured nodes are not found in cluster state

	// Example: Check for configured but missing nodes
	fmt.Println("=== Health Check ===")
	for addr, info := range nodesInfo {
		if info.Configured && !info.IsAlive {
			fmt.Printf("WARNING: Configured node %s is not active in the cluster!\n", addr)
		}
	}

	for addr, info := range gatesInfo {
		if info.Configured && !info.IsAlive {
			fmt.Printf("WARNING: Configured gate %s is not active in the cluster!\n", addr)
		}
	}
}
