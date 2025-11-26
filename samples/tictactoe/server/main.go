package main

import (
	"context"
	"flag"
	"fmt"
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
		server, err := goverseapi.NewServer(config)
		if err != nil {
			serverLogger.Errorf("Failed to create server: %v", err)
			panic(err)
		}

		// Register TicTacToeService object type
		goverseapi.RegisterObjectType((*TicTacToeService)(nil))

		// Keep service objects running - this goroutine runs forever
		go ensureServiceObjects()

		err = server.Run()
		if err != nil {
			serverLogger.Errorf("Server error: %v", err)
			panic(err)
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
		panic(err)
	}

	ctx := context.Background()
	err = gateServer.Start(ctx)
	if err != nil {
		serverLogger.Errorf("Failed to start gate server: %v", err)
		panic(err)
	}

	serverLogger.Infof("TicTacToe server started successfully!")
	serverLogger.Infof("  REST API: http://localhost%s", *httpAddr)
	serverLogger.Infof("  Web UI: Serve samples/tictactoe/web with a web server")

	// Wait forever
	select {}
}

// ensureServiceObjects keeps the service objects alive by periodically creating them
// This goroutine runs forever until server shutdown
// Creating an object that already exists is a no-op
func ensureServiceObjects() {
	serverLogger.Infof("Waiting for cluster to be ready...")
	<-goverseapi.ClusterReady()
	serverLogger.Infof("Cluster is ready, starting service object maintenance...")

	// Initial delay before first creation
	time.Sleep(5 * time.Second)

	ctx := context.Background()
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

		// Wait before next check - objects should stay alive, but we periodically ensure they exist
		time.Sleep(30 * time.Second)
	}
}
