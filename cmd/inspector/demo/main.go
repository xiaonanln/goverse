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
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspectserver"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
)

func main() {
	httpAddr := flag.String("http-addr", ":8080", "HTTP server address")
	grpcAddr := flag.String("grpc-addr", ":8081", "gRPC server address (for API)")
	etcdAddr := flag.String("etcd-addr", "localhost:2379", "etcd server address")
	etcdPrefix := flag.String("etcd-prefix", "/goverse-demo", "etcd key prefix for demo data")
	numNodes := flag.Int("nodes", 3, "Number of demo nodes")
	numGates := flag.Int("gates", 2, "Number of demo gates")
	numObjects := flag.Int("objects", 50, "Number of demo objects")
	numShards := flag.Int("shards", 64, "Number of shards")
	noBrowser := flag.Bool("no-browser", false, "Don't open browser automatically")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	pg := graph.NewGoverseGraph()

	// Generate demo data
	nodeAddrs := generateDemoData(pg, *numNodes, *numGates, *numObjects, *numShards)

	// Initialize etcd with fake shard mappings
	if err := initializeEtcdShardMappings(*etcdAddr, *etcdPrefix, nodeAddrs, *numShards); err != nil {
		log.Printf("Warning: Failed to initialize etcd shard mappings: %v", err)
		log.Printf("Demo will continue without etcd data")
	}

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	serversDone := make(chan struct{}, 2)

	// Determine static directory path - works whether running from root or demo directory
	staticDir := "cmd/inspector/web"
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		// Try relative path from demo directory
		staticDir = "../web"
		if _, err := os.Stat(staticDir); os.IsNotExist(err) {
			log.Fatalf("Cannot find web static directory. Please run from project root or cmd/inspector/demo")
		}
	}
	staticDir, _ = filepath.Abs(staticDir)

	// Create and configure the inspector server
	server := inspectserver.New(pg, inspectserver.Config{
		GRPCAddr:   *grpcAddr,
		HTTPAddr:   *httpAddr,
		StaticDir:  staticDir,
		EtcdAddr:   *etcdAddr,
		EtcdPrefix: *etcdPrefix,
		NumShards:  *numShards,
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

	// Start background goroutine to simulate shard migrations
	ctx, cancel := context.WithCancel(context.Background())
	go simulateShardMigrations(ctx, server, *etcdAddr, *etcdPrefix)
	
	// Start background goroutine to simulate object calls
	go simulateObjectCalls(ctx, pg)

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

func generateDemoData(pg *graph.GoverseGraph, numNodes, numGates, numObjects, numShards int) []string {
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

		// Random number of clients (0-50)
		clientCount := rand.Intn(51)

		pg.AddOrUpdateGate(models.GoverseGate{
			ID:             addr,
			Label:          gateID,
			Type:           "goverse_gate",
			AdvertiseAddr:  addr,
			Color:          "#2196F3", // Blue for gates
			RegisteredAt:   time.Now().Add(-time.Duration(rand.Intn(3600)) * time.Second),
			ConnectedNodes: connectedNodes,
			Clients:        clientCount,
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

	return nodeAddrs
}

func initializeEtcdShardMappings(etcdAddr, etcdPrefix string, nodeAddrs []string, numShards int) error {
	// Connect to etcd
	mgr, err := etcdmanager.NewEtcdManager(etcdAddr, etcdPrefix)
	if err != nil {
		return fmt.Errorf("failed to create etcd manager: %w", err)
	}

	if err := mgr.Connect(); err != nil {
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}
	defer mgr.Close()

	client := mgr.GetClient()

	// Initialize shard mappings with random node assignments
	// Each shard has same target and current node
	log.Printf("Initializing %d shards in etcd at %s with prefix %s", numShards, etcdAddr, etcdPrefix)

	for shardID := 0; shardID < numShards; shardID++ {
		// Assign shard to random node
		nodeAddr := nodeAddrs[rand.Intn(len(nodeAddrs))]

		// Format: "targetNode,currentNode" (both same for stable state)
		shardValue := fmt.Sprintf("%s,%s", nodeAddr, nodeAddr)
		shardKey := fmt.Sprintf("%s/shard/%d", etcdPrefix, shardID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := client.Put(ctx, shardKey, shardValue)
		cancel()
		if err != nil {
			return fmt.Errorf("failed to write shard %d: %w", shardID, err)
		}
	}

	log.Printf("Successfully initialized %d shards in etcd", numShards)
	return nil
}

func simulateDynamicUpdates(ctx context.Context, pg *graph.GoverseGraph, numNodes, numShards int) {
	objectTicker := time.NewTicker(5 * time.Second)
	defer objectTicker.Stop()

	// Separate ticker for connection changes (less frequent)
	connectionTicker := time.NewTicker(15 * time.Second)
	defer connectionTicker.Stop()

	// Separate ticker for gate connection changes
	gateConnectionTicker := time.NewTicker(12 * time.Second)
	defer gateConnectionTicker.Stop()

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
		case <-gateConnectionTicker.C:
			// Simulate gate connection changes (gates temporarily disconnect/reconnect to nodes)
			gates := pg.GetGates()
			if len(gates) > 0 && len(nodeAddrs) > 0 {
				// Pick a random gate
				gateIdx := rand.Intn(len(gates))
				gate := &gates[gateIdx]

				// Randomly either remove some connections or restore full connections
				if rand.Float32() < 0.5 && len(gate.ConnectedNodes) > 1 {
					// Remove some node connections from gate
					numToRemove := 1 + rand.Intn(len(gate.ConnectedNodes)-1)
					newConnections := make([]string, 0)

					// Keep some connections randomly
					for _, addr := range gate.ConnectedNodes {
						if rand.Float32() > float32(numToRemove)/float32(len(gate.ConnectedNodes)) {
							newConnections = append(newConnections, addr)
						}
					}

					// Ensure at least one connection remains
					if len(newConnections) == 0 && len(gate.ConnectedNodes) > 0 {
						newConnections = []string{gate.ConnectedNodes[0]}
					}

					gate.ConnectedNodes = newConnections
					pg.AddOrUpdateGate(*gate)
					log.Printf("[Demo] Gate %s connections reduced to %d nodes", gate.Label, len(newConnections))
				} else {
					// Restore full connections to all nodes
					gate.ConnectedNodes = make([]string, len(nodeAddrs))
					copy(gate.ConnectedNodes, nodeAddrs)
					pg.AddOrUpdateGate(*gate)
					log.Printf("[Demo] Gate %s connections restored to all %d nodes", gate.Label, len(nodeAddrs))
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

// simulateShardMigrations watches for shard migrations (TargetNode != CurrentNode)
// and automatically completes them after a delay to simulate the migration process
// Simulates the real GoVerse behavior: first unclaim (set current to empty), then claim (set current to target)
func simulateShardMigrations(ctx context.Context, server *inspectserver.InspectorServer, etcdAddr, etcdPrefix string) {
	// Connect to etcd
	mgr, err := etcdmanager.NewEtcdManager(etcdAddr, etcdPrefix)
	if err != nil {
		log.Printf("[Migration Simulator] Failed to create etcd manager: %v", err)
		return
	}
	if err := mgr.Connect(); err != nil {
		log.Printf("[Migration Simulator] Failed to connect to etcd: %v", err)
		return
	}
	defer mgr.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Track shards that are in the unclaimed phase (waiting to be claimed by target)
	unclaimedShards := make(map[int]string) // shardID -> targetNode

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Get consensus manager from server
			cm := server.GetConsensusManager()
			if cm == nil {
				continue
			}

			// Check for shards that need migration (TargetNode != CurrentNode)
			shardMapping := cm.GetShardMapping()
			if shardMapping == nil {
				continue
			}

			for shardID, shardInfo := range shardMapping.Shards {
				targetNode := shardInfo.TargetNode
				currentNode := shardInfo.CurrentNode

				// Check if this shard is already in unclaimed phase
				if unclaimedTargetNode, isUnclaimed := unclaimedShards[shardID]; isUnclaimed {
					// Shard is unclaimed, now claim it with the target node
					log.Printf("[Demo] Claiming shard %d by target node %s", shardID, unclaimedTargetNode)

					key := fmt.Sprintf("%s/shard/%d", etcdPrefix, shardID)
					value := fmt.Sprintf("%s,%s", unclaimedTargetNode, unclaimedTargetNode)
					if len(shardInfo.Flags) > 0 {
						for _, flag := range shardInfo.Flags {
							value += ",f=" + flag
						}
					}

					updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := mgr.Put(updateCtx, key, value); err != nil {
						log.Printf("[Demo] Failed to claim shard %d: %v", shardID, err)
					} else {
						log.Printf("[Demo] Completed shard %d migration to %s", shardID, unclaimedTargetNode)
					}
					cancel()

					// Remove from unclaimed tracking
					delete(unclaimedShards, shardID)
				} else if targetNode != "" && currentNode != targetNode {
					// Shard needs migration: first unclaim it (set current to empty)
					log.Printf("[Demo] Unclaiming shard %d from %s (target: %s)",
						shardID, currentNode, targetNode)

					key := fmt.Sprintf("%s/shard/%d", etcdPrefix, shardID)
					value := fmt.Sprintf("%s,", targetNode) // TargetNode set, CurrentNode empty
					if len(shardInfo.Flags) > 0 {
						for _, flag := range shardInfo.Flags {
							value += ",f=" + flag
						}
					}

					updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := mgr.Put(updateCtx, key, value); err != nil {
						log.Printf("[Demo] Failed to unclaim shard %d: %v", shardID, err)
					} else {
						log.Printf("[Demo] Unclaimed shard %d, will be claimed by %s in next tick", shardID, targetNode)
						// Track this shard as unclaimed, to be claimed in next tick
						unclaimedShards[shardID] = targetNode
					}
					cancel()
				}
			}
		}
	}
}

// simulateObjectCalls simulates periodic object method calls for demonstration
func simulateObjectCalls(ctx context.Context, pg *graph.GoverseGraph) {
ticker := time.NewTicker(1500 * time.Millisecond) // Call every 1.5 seconds
defer ticker.Stop()

methods := []string{"GetValue", "Update", "Process", "Sync", "Execute", "Query"}

for {
select {
case <-ctx.Done():
return
case <-ticker.C:
// Get all objects
objects := pg.GetObjects()
if len(objects) == 0 {
continue
}

// Pick a random object
obj := objects[rand.Intn(len(objects))]

// Pick a random method
method := methods[rand.Intn(len(methods))]

// Broadcast the call event
pg.BroadcastObjectCall(obj.ID, obj.Type, method, obj.GoverseNodeID)

log.Printf("[Demo] Simulated call: %s.%s on node %s", obj.ID, method, obj.GoverseNodeID)
}
}
}
