// Example demonstrating the GetNodesInfo and GetGatesInfo APIs
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
)

func main() {
	// Create and start a server (in a real application, this would be in main)
	server := goverseapi.NewServer()

	// Start the server in a goroutine
	go func() {
		if err := server.Start(); err != nil {
			fmt.Printf("Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for cluster to be ready
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case <-goverseapi.ClusterReady():
		fmt.Println("Cluster is ready!")
	case <-ctx.Done():
		fmt.Println("Timeout waiting for cluster to be ready")
		return
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
			fmt.Printf("  Found in Cluster: %v (registered in etcd)\n", info.FoundInClusterState)
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
			fmt.Printf("  Found in Cluster: %v (registered in etcd)\n", info.FoundInClusterState)
			fmt.Println()
		}
	}

	// In a real application, you might want to periodically check this information
	// or trigger alerts if configured nodes are not found in cluster state

	// Example: Check for configured but missing nodes
	fmt.Println("=== Health Check ===")
	for addr, info := range nodesInfo {
		if info.Configured && !info.FoundInClusterState {
			fmt.Printf("WARNING: Configured node %s is not active in the cluster!\n", addr)
		}
	}

	for addr, info := range gatesInfo {
		if info.Configured && !info.FoundInClusterState {
			fmt.Printf("WARNING: Configured gate %s is not active in the cluster!\n", addr)
		}
	}
}
