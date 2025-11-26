// Package main demonstrates how to configure a cluster with minimum node requirements.
//
// This example shows how to set MinQuorum to ensure the cluster waits for a quorum
// of nodes before becoming ready. This is useful for production deployments where
// you want to ensure a certain number of nodes are available before accepting traffic.
//
// Usage:
//
//	go run main.go --minQuorum=3 --port=7001
//	go run main.go --minQuorum=3 --port=7002 (in another terminal)
//	go run main.go --minQuorum=3 --port=7003 (in another terminal)
//
// The cluster will only become "ready" when all 3 nodes are registered.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/goverseapi"
)

var (
	minQuorum = flag.Int("minQuorum", 1, "Minimum number of nodes required for cluster to be ready")
	port      = flag.Int("port", 7001, "Port to listen on for node communication")
)

func main() {
	flag.Parse()

	// Configure server with MinQuorum requirement
	config := &goverseapi.ServerConfig{
		ListenAddress:    fmt.Sprintf("localhost:%d", *port),
		AdvertiseAddress: fmt.Sprintf("localhost:%d", *port),
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/goverse-example",
		MinQuorum:        *minQuorum, // Set minimum quorum required
	}

	log.Printf("Starting node on port %d with minimum quorum requirement: %d", *port, *minQuorum)

	server, err := goverseapi.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start a goroutine to monitor cluster readiness
	go func() {
		log.Printf("Waiting for cluster to become ready (requires %d nodes)...", *minQuorum)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		select {
		case <-goverseapi.ClusterReady():
			log.Printf("✓ Cluster is now READY! All %d minimum quorum are available.", *minQuorum)
		case <-ctx.Done():
			log.Printf("✗ Cluster did not become ready within timeout")
		}
	}()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("Shutting down node on port %d", *port)
		os.Exit(0)
	}()

	// Run the server (this blocks)
	log.Printf("Node running on port %d (waiting for %d total nodes)", *port, *minQuorum)
	if err := server.Run(context.Background()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
