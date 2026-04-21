package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/cmd/gate/gateconfig"
	"github.com/xiaonanln/goverse/gate/gateserver"
	"github.com/xiaonanln/goverse/util/logger"
)

var log = logger.NewLogger("Gate")

func main() {
	// Get gate server configuration from flags and/or config file
	loader := gateconfig.NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		log.Fatalf("Failed to load gate config: %v", err)
	}

	// Create gateserver server
	gateserver, err := gateserver.NewGateServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create gate server: %v", err)
	}

	// Create context for server lifecycle
	ctx := context.Background()

	// Start gate server (non-blocking)
	if err := gateserver.Start(ctx); err != nil {
		log.Fatalf("Failed to start gate server: %v", err)
	}

	log.Infof("Gate server started")

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	log.Infof("Received shutdown signal")

	// Stop the gate server
	if err := gateserver.Stop(); err != nil {
		log.Errorf("Error stopping gate: %v", err)
	}

	log.Infof("Gate stopped")
}
