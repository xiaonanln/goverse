package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/gate/gateserver"
)

func main() {
	// Create gate server configuration
	config := &gateserver.GateServerConfig{
		ListenAddress:    ":49000",
		AdvertiseAddress: "localhost:49000",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/goverse",
	}

	// Create gateserver server
	gateserver, err := gateserver.NewGateServer(config)
	if err != nil {
		log.Fatalf("Failed to create gate server: %v", err)
	}

	// Create context for server lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start gate server in goroutine
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- gateserver.Start(ctx)
	}()

	// Wait for shutdown signal or server error
	select {
	case <-sigChan:
		log.Println("Received shutdown signal")
		cancel() // Cancel context to trigger server shutdown
	case err := <-serverDone:
		if err != nil {
			log.Printf("Gate server error: %v", err)
		}
	}

	// Stop the gate server
	if err := gateserver.Stop(); err != nil {
		log.Printf("Error stopping gate: %v", err)
	}

	log.Println("Gate stopped")
}
