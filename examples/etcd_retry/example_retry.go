// Example demonstrating the automatic keep-alive retry mechanism
//
// This example shows how the EtcdManager automatically maintains node registration
// even when the etcd connection becomes unreliable. The keep-alive mechanism will:
// 1. Continuously maintain the lease for a registered node
// 2. Automatically retry with exponential backoff if the keep-alive channel closes
// 3. Re-register the node when etcd becomes available again
//
// Usage:
//   go run example_retry.go
//
// To test the retry mechanism:
// 1. Start etcd: etcd --listen-client-urls http://localhost:2379 --advertise-client-urls http://localhost:2379
// 2. Run this example
// 3. Stop etcd temporarily to simulate unreliability
// 4. Observe the retry messages in the logs
// 5. Restart etcd and see the node re-register automatically

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	// Create etcd manager
	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", "/goverse-retry-example")
	if err != nil {
		fmt.Printf("Failed to create EtcdManager: %v\n", err)
		os.Exit(1)
	}

	// Connect to etcd
	err = mgr.Connect()
	if err != nil {
		fmt.Printf("Failed to connect to etcd: %v\n", err)
		fmt.Println("Make sure etcd is running at localhost:2379")
		os.Exit(1)
	}
	defer mgr.Close()

	fmt.Println("Connected to etcd")

	ctx := context.Background()
	nodeAddress := "localhost:50000"

	// Register node using the shared lease API - this starts the automatic keep-alive retry loop
	key := mgr.GetNodesPrefix() + nodeAddress
	_, err = mgr.RegisterKeyLease(ctx, key, nodeAddress, etcdmanager.NodeLeaseTTL)
	if err != nil {
		fmt.Printf("Failed to register node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Registered node %s\n", nodeAddress)
	fmt.Println("Node will stay registered even if etcd becomes temporarily unavailable")
	fmt.Println("Try stopping and restarting etcd to see the automatic retry in action")
	fmt.Println("Press Ctrl+C to exit")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Periodically check and display node status
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Try to get all nodes from etcd (this might fail if etcd is down)
			nodesPrefix := mgr.GetNodesPrefix()
			client := mgr.GetClient()
			if client != nil {
				ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				resp, err := client.Get(ctx, nodesPrefix, clientv3.WithPrefix())
				cancel()

				if err != nil {
					fmt.Printf("Could not query nodes (etcd may be down): %v\n", err)
				} else {
					fmt.Printf("Currently tracking %d node(s)\n", len(resp.Kvs))
				}
			} else {
				fmt.Println("etcd client not available")
			}

		case <-sigChan:
			fmt.Println("\nReceived interrupt, cleaning up...")

			// Unregister node using the shared lease API
			key := mgr.GetNodesPrefix() + nodeAddress
			err = mgr.UnregisterKeyLease(ctx, key)
			if err != nil {
				fmt.Printf("Failed to unregister node: %v\n", err)
			} else {
				fmt.Printf("Unregistered node %s\n", nodeAddress)
			}

			return
		}
	}
}
