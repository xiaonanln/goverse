package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/gate/gateserver"
)

func main() {
	// Parse command line flags
	var (
		configPath     = flag.String("config", "", "Path to YAML config file")
		gateID         = flag.String("gate-id", "", "Gate ID (required if config is provided)")
		listenAddr     = flag.String("listen", ":49000", "Gate listen address")
		advertiseAddr  = flag.String("advertise", "localhost:49000", "Gate advertise address")
		httpListenAddr = flag.String("http-listen", "", "HTTP listen address for REST API and metrics (optional, e.g., ':8080')")
		etcdAddr       = flag.String("etcd", "localhost:2379", "Etcd address")
		etcdPrefix     = flag.String("etcd-prefix", "/goverse", "Etcd key prefix")
	)
	flag.Parse()

	var numShards int
	if *configPath != "" {
		// Load config from file
		cfg, err := config.LoadConfig(*configPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		if *gateID == "" {
			log.Fatalf("--gate-id is required when using --config")
		}
		gateCfg, err := cfg.GetGateByID(*gateID)
		if err != nil {
			log.Fatalf("Failed to find gate config: %v", err)
		}
		// Use config values, with flags as overrides only if they differ from defaults
		if *listenAddr == ":49000" {
			*listenAddr = gateCfg.GRPCAddr
		}
		if *advertiseAddr == "localhost:49000" {
			if gateCfg.AdvertiseAddr != "" {
				*advertiseAddr = gateCfg.AdvertiseAddr
			} else {
				*advertiseAddr = gateCfg.GRPCAddr
			}
		}
		if *httpListenAddr == "" && gateCfg.HTTPAddr != "" {
			*httpListenAddr = gateCfg.HTTPAddr
		}
		if *etcdAddr == "localhost:2379" {
			*etcdAddr = cfg.GetEtcdAddress()
		}
		if *etcdPrefix == "/goverse" {
			*etcdPrefix = cfg.GetEtcdPrefix()
		}
		numShards = cfg.GetNumShards()
	}

	// Create gate server configuration
	gateServerConfig := &gateserver.GateServerConfig{
		ListenAddress:     *listenAddr,
		AdvertiseAddress:  *advertiseAddr,
		HTTPListenAddress: *httpListenAddr,
		EtcdAddress:       *etcdAddr,
		EtcdPrefix:        *etcdPrefix,
		NumShards:         numShards,
	}

	// Create gateserver server
	gateserver, err := gateserver.NewGateServer(gateServerConfig)
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
