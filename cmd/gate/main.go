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
		listenAddr        = flag.String("listen", ":49000", "Gateway listen address")
		advertiseAddr     = flag.String("advertise", "localhost:49000", "Gateway advertise address")
		metricsListenAddr = flag.String("metrics-listen", "", "Metrics listen address (optional, e.g., ':9091')")
		etcdAddr          = flag.String("etcd", "localhost:2379", "Etcd address")
		etcdPrefix        = flag.String("etcd-prefix", "/goverse", "Etcd key prefix")
	)
	flag.Parse()

	// Create gateway server configuration
	config := &gateserver.GatewayServerConfig{
		ListenAddress:        *listenAddr,
		AdvertiseAddress:     *advertiseAddr,
		MetricsListenAddress: *metricsListenAddr,
		EtcdAddress:          *etcdAddr,
		EtcdPrefix:           *etcdPrefix,
	}

	// Create gateserver server
	gateserver, err := gateserver.NewGatewayServer(config)
	if err != nil {
		log.Fatalf("Failed to create gateway server: %v", err)
	}

	// Create context for server lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start gateway server in goroutine
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
			log.Printf("Gateway server error: %v", err)
		}
	}

	// Stop the gateway server
	if err := gateserver.Stop(); err != nil {
		log.Printf("Error stopping gateway: %v", err)
	}

	log.Println("Gateway stopped")
}
