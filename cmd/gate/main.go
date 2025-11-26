package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/gate/gateserver"
)

func main() {
	// Parse command line flags
	var (
		listenAddr     = flag.String("listen", ":49000", "Gate listen address")
		advertiseAddr  = flag.String("advertise", "localhost:49000", "Gate advertise address")
		httpListenAddr = flag.String("http-listen", "", "HTTP listen address for REST API and metrics (optional, e.g., ':8080')")
		etcdAddr       = flag.String("etcd", "localhost:2379", "Etcd address")
		etcdPrefix     = flag.String("etcd-prefix", "/goverse", "Etcd key prefix")
	)
	flag.Parse()

	// Create gate server configuration
	config := &gateserver.GateServerConfig{
		ListenAddress:     *listenAddr,
		AdvertiseAddress:  *advertiseAddr,
		HTTPListenAddress: *httpListenAddr,
		EtcdAddress:       *etcdAddr,
		EtcdPrefix:        *etcdPrefix,
	}

	// Create gateserver server
	gateserver, err := gateserver.NewGateServer(config)
	if err != nil {
		log.Fatalf("Failed to create gate server: %v", err)
	}

	// Create context for server lifecycle
	ctx := context.Background()

	// Start gate server (non-blocking)
	if err := gateserver.Start(ctx); err != nil {
		log.Fatalf("Failed to start gate server: %v", err)
	}

	log.Println("Gate server started")

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal")

	// Stop the gate server
	if err := gateserver.Stop(); err != nil {
		log.Printf("Error stopping gate: %v", err)
	}

	log.Println("Gate stopped")
}
