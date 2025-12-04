// Package main demonstrates how to enable pprof profiling in a Goverse node.
//
// pprof is a powerful profiling tool that can help identify performance bottlenecks,
// memory leaks, and other issues in your distributed objects system.
//
// Usage:
//
// # Start node with pprof enabled
// go run main.go
//
// # In another terminal, access pprof endpoints:
// # View available profiles
// curl http://localhost:9090/debug/pprof/
//
// # Get heap profile
// go tool pprof http://localhost:9090/debug/pprof/heap
//
// # Get CPU profile (30 second sample)
// go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30
//
// # Get goroutine profile
// go tool pprof http://localhost:9090/debug/pprof/goroutine
//
// # Get trace (5 second sample)
// curl http://localhost:9090/debug/pprof/trace?seconds=5 > trace.out
// go tool trace trace.out
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/goverseapi"
)

func main() {
	// Configure server with pprof enabled
	config := &goverseapi.ServerConfig{
		ListenAddress:        "localhost:47000",
		AdvertiseAddress:     "localhost:47000",
		MetricsListenAddress: "localhost:9090", // Required for pprof
		EnablePprof:          true,             // Enable pprof endpoints
		EtcdAddress:          "localhost:2379",
		EtcdPrefix:           "/goverse-pprof-example",
	}

	log.Println("Starting Goverse node with pprof enabled...")
	log.Println("Node gRPC server: localhost:47000")
	log.Println("Metrics and pprof: http://localhost:9090")
	log.Println("")
	log.Println("Available pprof endpoints:")
	log.Println("  http://localhost:9090/debug/pprof/          - Index of available profiles")
	log.Println("  http://localhost:9090/debug/pprof/heap      - Heap memory profile")
	log.Println("  http://localhost:9090/debug/pprof/goroutine - Goroutine profile")
	log.Println("  http://localhost:9090/debug/pprof/profile   - CPU profile")
	log.Println("  http://localhost:9090/debug/pprof/trace     - Execution trace")
	log.Println("")
	log.Println("Example commands:")
	log.Println("  go tool pprof http://localhost:9090/debug/pprof/heap")
	log.Println("  go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30")
	log.Println("")

	server, err := goverseapi.NewServerWithConfig(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		os.Exit(0)
	}()

	// Run the server (this blocks)
	log.Println("Node is running. Press Ctrl+C to stop.")
	if err := server.Run(context.Background()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
