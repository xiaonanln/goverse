package main

import (
	"log"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
)

// This example demonstrates how to configure a custom NodeStabilityDuration
// for the cluster. The NodeStabilityDuration determines how long the cluster
// waits for the node list to be stable before updating shard mapping.
//
// Use cases for custom durations:
// - Shorter duration (e.g., 3s): For development/testing environments where faster
//   cluster convergence is preferred
// - Longer duration (e.g., 30s): For production environments with frequent node
//   churn where you want to avoid premature shard reassignments

func main() {
	// Create server configuration with custom NodeStabilityDuration
	config := &goverseapi.ServerConfig{
		ListenAddress:    "localhost:7000",
		AdvertiseAddress: "localhost:7000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/myapp",
		MinQuorum:        1,
		// Set custom stability duration (default is 10s if not specified)
		// A shorter duration means the cluster will update shard mapping sooner
		// after node changes, but may cause more frequent rebalancing
		NodeStabilityDuration: 5 * time.Second,
	}

	// Create the server
	server, err := goverseapi.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("Server created with custom NodeStabilityDuration: %v", config.NodeStabilityDuration)
	log.Printf("The cluster will wait %v for node list to stabilize before updating shard mapping", config.NodeStabilityDuration)

	// Register your object and client types here
	// goverseapi.RegisterObjectType((*MyObject)(nil))
	// goverseapi.RegisterClientType((*MyClient)(nil))

	// Start the server (this blocks until shutdown)
	if err := server.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
