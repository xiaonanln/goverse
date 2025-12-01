// Inspector Demo - Runs a standalone inspector with demo data
// No etcd required, automatically opens browser with sample cluster visualization
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspectserver"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
)

func main() {
	httpAddr := flag.String("http-addr", ":8080", "HTTP server address")
	grpcAddr := flag.String("grpc-addr", ":8081", "gRPC server address (for API)")
	numNodes := flag.Int("nodes", 3, "Number of demo nodes")
	numGates := flag.Int("gates", 2, "Number of demo gates")
	numObjects := flag.Int("objects", 50, "Number of demo objects")
	numShards := flag.Int("shards", 64, "Number of shards")
	noBrowser := flag.Bool("no-browser", false, "Don't open browser automatically")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	pg := graph.NewGoverseGraph()

	// Generate demo data
	generateDemoData(pg, *numNodes, *numGates, *numObjects, *numShards)

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

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

	// Wait for HTTP server to be ready
	httpURL := fmt.Sprintf("http://localhost%s", *httpAddr)
	waitForServer(httpURL)

	log.Printf("Inspector Demo running at %s", httpURL)
	log.Printf("  Nodes: %d, Gates: %d, Objects: %d, Shards: %d", *numNodes, *numGates, *numObjects, *numShards)

	// Open browser
	if !*noBrowser {
		openBrowser(httpURL)
	}

	// Start background goroutine to simulate dynamic updates
	ctx, cancel := context.WithCancel(context.Background())
	go simulateDynamicUpdates(ctx, pg, *numNodes, *numShards)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down...")
	cancel()
	server.Shutdown()

	// Wait for servers to stop
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
	log.Println("Shutdown complete")
}

func generateDemoData(pg *graph.GoverseGraph, numNodes, numGates, numObjects, numShards int) {
	// Generate nodes - first pass to collect all addresses
	nodeAddrs := make([]string, numNodes)
	for i := 0; i < numNodes; i++ {
		addr := fmt.Sprintf("localhost:%d", 47000+i)
		nodeAddrs[i] = addr
	}

	// Generate nodes - second pass with connections
	for i := 0; i < numNodes; i++ {
		nodeID := fmt.Sprintf("node-%d", i+1)
		addr := nodeAddrs[i]

		// Each node is connected to all other nodes (fully connected mesh)
		connectedNodes := make([]string, 0, numNodes-1)
		for j := 0; j < numNodes; j++ {
			if i != j {
				connectedNodes = append(connectedNodes, nodeAddrs[j])
			}
		}

		pg.AddOrUpdateNode(models.GoverseNode{
			ID:             nodeID,
			Label:          nodeID,
			Type:           "node",
			AdvertiseAddr:  addr,
			Color:          "#4CAF50", // Green for nodes
			RegisteredAt:   time.Now().Add(-time.Duration(rand.Intn(3600)) * time.Second),
			ConnectedNodes: connectedNodes,
		})
	}

	// Generate gates
	for i := 0; i < numGates; i++ {
		gateID := fmt.Sprintf("gate-%d", i+1)
		addr := fmt.Sprintf("localhost:%d", 49000+i)

		// Each gate is connected to all nodes
		connectedNodes := make([]string, 0, numNodes)
		for j := 0; j < numNodes; j++ {
			connectedNodes = append(connectedNodes, nodeAddrs[j])
		}

		pg.AddOrUpdateGate(models.GoverseGate{
			ID:             addr,
			Label:          gateID,
			Type:           "goverse_gate",
			AdvertiseAddr:  addr,
			Color:          "#2196F3", // Blue for gates
			RegisteredAt:   time.Now().Add(-time.Duration(rand.Intn(3600)) * time.Second),
			ConnectedNodes: connectedNodes,
		})
	}

	// Object types for demo
	objectTypes := []string{"Counter", "ChatRoom", "Player", "GameSession", "Inventory", "Leaderboard"}
	typeColors := map[string]string{
		"Counter":     "#FF9800", // Orange
		"ChatRoom":    "#9C27B0", // Purple
		"Player":      "#E91E63", // Pink
		"GameSession": "#00BCD4", // Cyan
		"Inventory":   "#795548", // Brown
		"Leaderboard": "#607D8B", // Blue Grey
	}

	// Generate objects distributed across nodes and shards
	for i := 0; i < numObjects; i++ {
		objType := objectTypes[rand.Intn(len(objectTypes))]
		objID := fmt.Sprintf("%s-%d", objType, i+1)
		shardID := rand.Intn(numShards)
		nodeIdx := shardID % numNodes // Simple shard-to-node mapping
		nodeID := fmt.Sprintf("node-%d", nodeIdx+1)

		pg.AddOrUpdateObject(models.GoverseObject{
			ID:            objID,
			Label:         objID,
			Type:          objType,
			ShardID:       shardID,
			GoverseNodeID: nodeID,
			Color:         typeColors[objType],
			Size:          10 + rand.Float64()*20,
		})
	}

	log.Printf("Generated demo data: %d nodes, %d gates, %d objects across %d shards",
		numNodes, numGates, numObjects, numShards)
}

func simulateDynamicUpdates(ctx context.Context, pg *graph.GoverseGraph, numNodes, numShards int) {
	objectTicker := time.NewTicker(5 * time.Second)
	defer objectTicker.Stop()

	// Separate ticker for connection changes (less frequent)
	connectionTicker := time.NewTicker(15 * time.Second)
	defer connectionTicker.Stop()

	objectTypes := []string{"Counter", "ChatRoom", "Player", "GameSession", "Inventory", "Leaderboard"}
	typeColors := map[string]string{
		"Counter":     "#FF9800",
		"ChatRoom":    "#9C27B0",
		"Player":      "#E91E63",
		"GameSession": "#00BCD4",
		"Inventory":   "#795548",
		"Leaderboard": "#607D8B",
	}

	objectCounter := 100

	// Generate node addresses for connection simulation
	nodeAddrs := make([]string, numNodes)
	for i := 0; i < numNodes; i++ {
		nodeAddrs[i] = fmt.Sprintf("localhost:%d", 47000+i)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-objectTicker.C:
			// Randomly add or remove objects
			if rand.Float32() < 0.7 {
				// Add a new object
				objType := objectTypes[rand.Intn(len(objectTypes))]
				objID := fmt.Sprintf("%s-%d", objType, objectCounter)
				objectCounter++
				shardID := rand.Intn(numShards)
				nodeIdx := shardID % numNodes
				nodeID := fmt.Sprintf("node-%d", nodeIdx+1)

				pg.AddOrUpdateObject(models.GoverseObject{
					ID:            objID,
					Label:         objID,
					Type:          objType,
					ShardID:       shardID,
					GoverseNodeID: nodeID,
					Color:         typeColors[objType],
					Size:          10 + rand.Float64()*20,
				})
				log.Printf("[Demo] Created object: %s on %s (shard %d)", objID, nodeID, shardID)
			} else {
				// Remove a random object
				objects := pg.GetObjects()
				if len(objects) > 10 {
					obj := objects[rand.Intn(len(objects))]
					pg.RemoveObject(obj.ID)
					log.Printf("[Demo] Removed object: %s", obj.ID)
				}
			}
		case <-connectionTicker.C:
			// Simulate connection changes (node temporarily disconnects/reconnects)
			nodes := pg.GetNodes()
			if len(nodes) > 1 {
				// Build address-to-node map for quick lookup
				addrToNode := make(map[string]*models.GoverseNode)
				for i := range nodes {
					addrToNode[nodes[i].AdvertiseAddr] = &nodes[i]
				}

				// Pick a random node
				nodeIdx := rand.Intn(len(nodes))
				node := &nodes[nodeIdx]

				// Randomly either remove some connections or restore full mesh
				if rand.Float32() < 0.5 && len(node.ConnectedNodes) > 1 {
					// Remove some connections symmetrically
					numToRemove := 1 + rand.Intn(len(node.ConnectedNodes)-1)
					connectionsToRemove := make(map[string]bool)

					// Select which connections to remove
					for i := 0; i < numToRemove; i++ {
						targetAddr := node.ConnectedNodes[rand.Intn(len(node.ConnectedNodes))]
						connectionsToRemove[targetAddr] = true
					}

					// Remove connections from the selected node
					newConnections := make([]string, 0)
					for _, addr := range node.ConnectedNodes {
						if !connectionsToRemove[addr] {
							newConnections = append(newConnections, addr)
						}
					}
					node.ConnectedNodes = newConnections
					pg.AddOrUpdateNode(*node)

					// Remove reverse connections from the other nodes
					for targetAddr := range connectionsToRemove {
						if targetNode, ok := addrToNode[targetAddr]; ok {
							// Remove node's address from target's connections
							filteredConnections := make([]string, 0)
							for _, addr := range targetNode.ConnectedNodes {
								if addr != node.AdvertiseAddr {
									filteredConnections = append(filteredConnections, addr)
								}
							}
							targetNode.ConnectedNodes = filteredConnections
							pg.AddOrUpdateNode(*targetNode)
						}
					}

					log.Printf("[Demo] Node %s connections reduced to %d (removed from both ends)", node.ID, len(newConnections))
				} else {
					// Restore full mesh for all nodes
					for i := range nodes {
						n := &nodes[i]
						connectedNodes := make([]string, 0, len(nodeAddrs)-1)
						for _, addr := range nodeAddrs {
							if addr != n.AdvertiseAddr {
								connectedNodes = append(connectedNodes, addr)
							}
						}
						n.ConnectedNodes = connectedNodes
						pg.AddOrUpdateNode(*n)
					}
					log.Printf("[Demo] All nodes connections restored to full mesh")
				}
			}
		}
	}
}

func waitForServer(url string) {
	for i := 0; i < 50; i++ {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // Linux and others
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open browser: %v", err)
		log.Printf("Please open %s manually", url)
	}
}
