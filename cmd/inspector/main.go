package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspectserver"
)

func main() {
	// Parse command-line flags
	grpcAddr := flag.String("grpc-addr", ":8081", "gRPC server address")
	httpAddr := flag.String("http-addr", ":8080", "HTTP server address")
	flag.Parse()

	pg := graph.NewGoverseGraph()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Track server completion
	serversDone := make(chan struct{}, 2)

	// Create and configure the inspector server
	server := inspectserver.New(pg, inspectserver.Config{
		GRPCAddr:  *grpcAddr,
		HTTPAddr:  *httpAddr,
		StaticDir: "cmd/inspector/web",
	})

	// Start gRPC server
	if err := server.ServeGRPC(serversDone); err != nil {
		log.Fatalf("Failed to start gRPC server: %v", err)
	}

	// Start HTTP server
	if err := server.ServeHTTP(serversDone); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal")
	server.Shutdown()

	// Wait for both servers to stop with timeout
	timeout := time.After(5 * time.Second)
	serversShutdown := 0
	for serversShutdown < 2 {
		select {
		case <-serversDone:
			serversShutdown++
		case <-timeout:
			log.Println("Timeout waiting for servers to shutdown")
			return
		}
	}

	log.Println("Inspector stopped")
}
