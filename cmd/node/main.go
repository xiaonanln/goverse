package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/server"
)

func main() {
	// Parse command line flags
	var (
		configFile = flag.String("config", "", "Path to YAML configuration file")
		nodeID     = flag.String("node-id", "", "Node ID from the configuration file")
		// Legacy flags for direct configuration (can be used without config file)
		listenAddr    = flag.String("listen", "", "gRPC listen address (e.g., ':9000')")
		advertiseAddr = flag.String("advertise", "", "Advertise address (e.g., 'localhost:9000')")
		metricsAddr   = flag.String("metrics", "", "HTTP address for Prometheus metrics (optional, e.g., ':9090')")
		etcdAddr      = flag.String("etcd", "localhost:2379", "Etcd address")
		etcdPrefix    = flag.String("etcd-prefix", "/goverse", "Etcd key prefix")
	)
	flag.Parse()

	var serverConfig *server.ServerConfig

	if *configFile != "" {
		// Load configuration from file
		if *nodeID == "" {
			log.Fatal("--node-id is required when using --config")
		}

		cfg, err := config.LoadConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load configuration: %v", err)
		}

		nodeCfg, err := cfg.GetNodeByID(*nodeID)
		if err != nil {
			log.Fatalf("Failed to find node configuration: %v", err)
		}

		serverConfig = &server.ServerConfig{
			ListenAddress:        nodeCfg.GRPCAddr,
			AdvertiseAddress:     nodeCfg.AdvertiseAddr,
			MetricsListenAddress: nodeCfg.HTTPAddr,
			EtcdAddress:          cfg.GetEtcdAddress(),
			EtcdPrefix:           cfg.GetEtcdPrefix(),
			NumShards:            cfg.GetNumShards(),
		}

		log.Printf("Starting node %s with configuration from %s", *nodeID, *configFile)
	} else {
		// Use legacy flags
		if *listenAddr == "" {
			log.Fatal("--listen is required when not using --config")
		}
		if *advertiseAddr == "" {
			log.Fatal("--advertise is required when not using --config")
		}

		serverConfig = &server.ServerConfig{
			ListenAddress:        *listenAddr,
			AdvertiseAddress:     *advertiseAddr,
			MetricsListenAddress: *metricsAddr,
			EtcdAddress:          *etcdAddr,
			EtcdPrefix:           *etcdPrefix,
		}

		log.Printf("Starting node with direct configuration (listen: %s, advertise: %s)", *listenAddr, *advertiseAddr)
	}

	// Create server
	srv, err := server.NewServer(serverConfig)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Create context for server lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Run(ctx)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	case err := <-errChan:
		if err != nil {
			log.Printf("Server error: %v", err)
		}
	}

	log.Println("Node stopped")
}
