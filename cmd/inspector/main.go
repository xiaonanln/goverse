package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspectserver"
)

func main() {
	// Parse command-line flags
	etcdAddress := flag.String("etcd-address", "localhost:2379", "etcd server address")
	etcdPrefix := flag.String("etcd-prefix", etcdmanager.DefaultPrefix, "etcd key prefix")
	grpcAddr := flag.String("grpc-addr", ":8081", "gRPC server address")
	httpAddr := flag.String("http-addr", ":8080", "HTTP server address")
	flag.Parse()

	pg := graph.NewGoverseGraph()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Track server completion
	serversDone := make(chan struct{}, 2)

	// Setup etcd registration
	etcdMgr, err := etcdmanager.NewEtcdManager(*etcdAddress, *etcdPrefix)
	if err != nil {
		log.Fatalf("Failed to create etcd manager: %v", err)
	}
	if err := etcdMgr.Connect(); err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}

	// Register inspector address in etcd under /goverse/inspector
	inspectorKey := etcdMgr.GetPrefix() + "/inspector"
	// Use grpcAddr as the inspector address (this is the gRPC endpoint that nodes connect to)
	inspectorAddress := *grpcAddr
	ctx := context.Background()
	_, err = etcdMgr.RegisterKeyLease(ctx, inspectorKey, inspectorAddress, etcdmanager.NodeLeaseTTL)
	if err != nil {
		log.Fatalf("Failed to register inspector with etcd: %v", err)
	}
	log.Printf("Registered inspector at %s in etcd with key %s", inspectorAddress, inspectorKey)

	// Create and configure the inspector server
	server := inspectserver.New(pg, inspectserver.Config{
		GRPCAddr:  *grpcAddr,
		HTTPAddr:  *httpAddr,
		StaticDir: "inspector/web",
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

	// Unregister from etcd
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := etcdMgr.UnregisterKeyLease(ctx, inspectorKey); err != nil {
		log.Printf("Failed to unregister inspector from etcd: %v", err)
	} else {
		log.Printf("Unregistered inspector from etcd")
	}
	cancel()
	if err := etcdMgr.Close(); err != nil {
		log.Printf("Failed to close etcd manager: %v", err)
	}

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
