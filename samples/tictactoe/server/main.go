package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/gate/gateserver"
	"github.com/xiaonanln/goverse/goverseapi"
	"github.com/xiaonanln/goverse/util/logger"
)

var serverLogger = logger.NewLogger("TicTacToeServer")

func main() {
	var (
		nodeAddr = flag.String("node-addr", "localhost:50051", "Node listen/advertise address")
		gateAddr = flag.String("gate-addr", "localhost:49000", "Gate gRPC listen address")
		httpAddr = flag.String("http-addr", ":8080", "HTTP listen address for REST API")
	)
	flag.Parse()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	serverLogger.Infof("Starting TicTacToe server...")
	serverLogger.Infof("  Node address: %s", *nodeAddr)
	serverLogger.Infof("  Gate address: %s", *gateAddr)
	serverLogger.Infof("  HTTP address: %s", *httpAddr)

	// Start the node server in a goroutine
	go func() {
		config := &goverseapi.ServerConfig{
			ListenAddress:    *nodeAddr,
			AdvertiseAddress: *nodeAddr,
		}
		server, err := goverseapi.NewServerWithConfig(config)
		if err != nil {
			serverLogger.Errorf("Failed to create server: %v", err)
			cancel()
			return
		}

		// Register TicTacToeService object type
		goverseapi.RegisterObjectType((*TicTacToeService)(nil))

		// Keep service objects running
		go ensureServiceObjects(ctx)

		err = server.Run(ctx)
		if err != nil {
			serverLogger.Errorf("Server error: %v", err)
			cancel()
		}
	}()

	// Give the node some time to start
	time.Sleep(2 * time.Second)

	// Start the gate server
	gateConfig := &gateserver.GateServerConfig{
		ListenAddress:     *gateAddr,
		AdvertiseAddress:  *gateAddr,
		HTTPListenAddress: *httpAddr,
		EtcdAddress:       "localhost:2379",
	}

	gateServer, err := gateserver.NewGateServer(gateConfig)
	if err != nil {
		serverLogger.Errorf("Failed to create gate server: %v", err)
		return
	}

	// Start gate server in a goroutine
	go func() {
		err := gateServer.Start(ctx)
		if err != nil {
			serverLogger.Errorf("Failed to start gate server: %v", err)
			cancel()
		}
	}()

	// Give gate some time to start
	time.Sleep(500 * time.Millisecond)

	serverLogger.Infof("TicTacToe server started successfully!")
	serverLogger.Infof("  REST API: http://localhost%s", *httpAddr)
	serverLogger.Infof("  Web UI: Serve samples/tictactoe/web with a web server")
	fmt.Println("\nPress Ctrl+C to stop the server...")

	// Wait for shutdown signal or context cancellation
	select {
	case <-sigCh:
		serverLogger.Infof("Received shutdown signal, stopping servers...")
	case <-ctx.Done():
		serverLogger.Infof("Context cancelled, stopping servers...")
	}

	// Cancel context to stop all goroutines
	cancel()

	// Give servers time to gracefully shutdown
	time.Sleep(500 * time.Millisecond)

	serverLogger.Infof("TicTacToe server stopped.")
}

// ensureServiceObjects keeps the service objects alive by periodically creating them
// This goroutine runs until context is cancelled
// Creating an object that already exists is a no-op
func ensureServiceObjects(ctx context.Context) {
	serverLogger.Infof("Waiting for cluster to be ready...")
	select {
	case <-goverseapi.ClusterReady():
		serverLogger.Infof("Cluster is ready, starting service object maintenance...")
	case <-ctx.Done():
		serverLogger.Infof("Context cancelled while waiting for cluster")
		return
	}

	// Initial delay before first creation
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		// Create/ensure all service objects exist
		for i := 1; i <= 10; i++ {
			serviceID := fmt.Sprintf("service-%d", i)
			_, err := goverseapi.CreateObject(ctx, "TicTacToeService", serviceID)
			if err != nil {
				// This is expected if object already exists
				serverLogger.Debugf("CreateObject %s: %v", serviceID, err)
			} else {
				serverLogger.Infof("Created service object: %s", serviceID)
			}
		}

		// Wait before next check or context cancellation
		select {
		case <-ticker.C:
			// Continue to next iteration
		case <-ctx.Done():
			serverLogger.Infof("Service object maintenance stopped")
			return
		}
	}
}
