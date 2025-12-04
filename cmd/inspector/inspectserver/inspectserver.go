package inspectserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/xiaonanln/goverse/cluster/consensusmanager"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspector"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

// sseHeartbeatInterval is the interval between SSE heartbeat events
const sseHeartbeatInterval = 30 * time.Second

// clusterStateStabilityDuration is the duration to wait for cluster state to stabilize
const clusterStateStabilityDuration = 3 * time.Second

// SSEClient represents a connected SSE client
type SSEClient struct {
	id        string
	eventChan chan graph.GraphEvent
	done      chan struct{}
}

// InspectorServer hosts the gRPC and HTTP servers for the inspector
type InspectorServer struct {
	pg           *graph.GoverseGraph
	grpcServer   *grpc.Server
	httpServer   *http.Server
	grpcAddr     string
	httpAddr     string
	staticDir    string
	shutdownChan chan struct{}

	// SSE clients management
	sseClientsMu sync.RWMutex
	sseClients   map[string]*SSEClient
	clientIDSeq  int64

	// etcd connection (optional)
	etcdManager *etcdmanager.EtcdManager
	etcdPrefix  string

	// consensus manager for cluster state
	consensusManager *consensusmanager.ConsensusManager
}

// Config holds the configuration for InspectorServer
type Config struct {
	GRPCAddr   string
	HTTPAddr   string
	StaticDir  string
	EtcdAddr   string // optional etcd address
	EtcdPrefix string // etcd key prefix
	NumShards  int    // number of shards in the cluster
}

// New creates a new InspectorServer
func New(pg *graph.GoverseGraph, cfg Config) *InspectorServer {
	s := &InspectorServer{
		pg:           pg,
		grpcAddr:     cfg.GRPCAddr,
		httpAddr:     cfg.HTTPAddr,
		staticDir:    cfg.StaticDir,
		shutdownChan: make(chan struct{}),
		sseClients:   make(map[string]*SSEClient),
		etcdPrefix:   cfg.EtcdPrefix,
	}

	// Connect to etcd if address is provided
	if cfg.EtcdAddr != "" {
		mgr, err := etcdmanager.NewEtcdManager(cfg.EtcdAddr, cfg.EtcdPrefix)
		if err != nil {
			log.Printf("Failed to create etcd manager: %v", err)
		} else if err := mgr.Connect(); err != nil {
			log.Printf("Failed to connect to etcd: %v", err)
		} else {
			s.etcdManager = mgr
			log.Printf("Connected to etcd at %s with prefix %s", cfg.EtcdAddr, cfg.EtcdPrefix)

			// Initialize ConsensusManager for watching cluster state
			numShards := cfg.NumShards
			if numShards <= 0 {
				numShards = sharding.NumShards
			}
			shardLock := shardlock.NewShardLock(numShards)
			s.consensusManager = consensusmanager.NewConsensusManager(
				mgr,
				shardLock,
				clusterStateStabilityDuration,
				"", // localNodeAddress (not needed for inspector)
				numShards,
			)

			// Initialize with a timeout context
			initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.consensusManager.Initialize(initCtx); err != nil {
				initCancel()
				log.Panicf("Failed to initialize consensus manager: %v", err)
			}
			initCancel()

			// Start watching with a long-lived context (will be cancelled on shutdown)
			if err := s.consensusManager.StartWatch(context.Background()); err != nil {
				log.Panicf("Failed to start consensus manager watch: %v", err)
			}
			log.Printf("ConsensusManager initialized and watching cluster state")
		}
	}

	// Register as observer to receive graph events
	pg.AddObserver(s)

	return s
}

// GetConsensusManager returns the consensus manager (for demo simulation)
func (s *InspectorServer) GetConsensusManager() *consensusmanager.ConsensusManager {
	return s.consensusManager
}

// OnGraphEvent implements the graph.Observer interface
func (s *InspectorServer) OnGraphEvent(event graph.GraphEvent) {
	s.sseClientsMu.RLock()
	clients := make([]*SSEClient, 0, len(s.sseClients))
	for _, c := range s.sseClients {
		clients = append(clients, c)
	}
	s.sseClientsMu.RUnlock()

	for _, client := range clients {
		select {
		case client.eventChan <- event:
		case <-client.done:
		default:
			// Channel full, log and skip this event for this client
			log.Printf("Event dropped for SSE client %s: channel full", client.id)
		}
	}
}

// createHTTPHandler creates the HTTP handler for the inspector web UI
func (s *InspectorServer) createHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir(s.staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := s.pg.GetNodes()
		gates := s.pg.GetGates()
		objects := s.pg.GetObjects()
		out := struct {
			GoverseNodes   []GoverseNode        `json:"goverse_nodes"`
			GoverseGates   []models.GoverseGate `json:"goverse_gates"`
			GoverseObjects []GoverseObject      `json:"goverse_objects"`
		}{
			GoverseNodes:   nodes,
			GoverseGates:   gates,
			GoverseObjects: objects,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Shard mapping endpoint
	mux.HandleFunc("/shards", s.handleShardMapping)

	// Shard move endpoint
	mux.HandleFunc("/shards/move", s.handleShardMove)

	// SSE endpoint for push-based updates
	mux.HandleFunc("/events/stream", s.handleEventsStream)

	return mux
}

// handleEventsStream handles GET /events/stream for Server-Sent Events (SSE)
func (s *InspectorServer) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed for SSE", http.StatusMethodNotAllowed)
		return
	}

	// Verify the response writer supports flushing (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported by server", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client
	s.sseClientsMu.Lock()
	s.clientIDSeq++
	clientID := fmt.Sprintf("client-%d", s.clientIDSeq)
	client := &SSEClient{
		id:        clientID,
		eventChan: make(chan graph.GraphEvent, 100),
		done:      make(chan struct{}),
	}
	s.sseClients[clientID] = client
	s.sseClientsMu.Unlock()

	log.Printf("SSE client connected: %s", clientID)

	// Cleanup on disconnect
	defer func() {
		close(client.done)
		s.sseClientsMu.Lock()
		delete(s.sseClients, clientID)
		s.sseClientsMu.Unlock()
		log.Printf("SSE client disconnected: %s", clientID)
	}()

	// Send initial full state
	nodes := s.pg.GetNodes()
	gates := s.pg.GetGates()
	objects := s.pg.GetObjects()
	initialData := struct {
		Type           string               `json:"type"`
		GoverseNodes   []GoverseNode        `json:"goverse_nodes"`
		GoverseGates   []models.GoverseGate `json:"goverse_gates"`
		GoverseObjects []GoverseObject      `json:"goverse_objects"`
	}{
		Type:           "initial",
		GoverseNodes:   nodes,
		GoverseGates:   gates,
		GoverseObjects: objects,
	}
	if err := s.writeSSEEvent(w, flusher, "initial", initialData); err != nil {
		log.Printf("Failed to send initial state to SSE client %s: %v", clientID, err)
		return
	}

	// Create heartbeat ticker
	heartbeatTicker := time.NewTicker(sseHeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Stream events to the client
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		case <-heartbeatTicker.C:
			// Send heartbeat to keep connection alive
			if err := s.writeSSEEvent(w, flusher, "heartbeat", struct{}{}); err != nil {
				log.Printf("Failed to send heartbeat to SSE client %s: %v", clientID, err)
				return
			}
		case event := <-client.eventChan:
			eventType := string(event.Type)
			log.Printf("Sending SSE event '%s' to client %s", eventType, clientID)
			if err := s.writeSSEEvent(w, flusher, eventType, event); err != nil {
				log.Printf("Failed to send event to SSE client %s: %v", clientID, err)
				return
			}
		}
	}
}

// writeSSEEvent writes a Server-Sent Event to the response writer
func (s *InspectorServer) writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) error {
	// Marshal data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Write SSE format: event: <type>\ndata: <json>\n\n
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}

	flusher.Flush()
	return nil
}

// ServeHTTP starts the HTTP server in a goroutine and returns immediately
func (s *InspectorServer) ServeHTTP(done chan<- struct{}) error {
	handler := s.createHTTPHandler()

	s.httpServer = &http.Server{
		Addr:              s.httpAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HTTP on %s (serving %s)", s.httpAddr, filepath.Join(".", s.staticDir))

	// Handle graceful shutdown
	go func() {
		<-s.shutdownChan
		log.Println("Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
		log.Println("HTTP server stopped")
		done <- struct{}{}
	}()

	return nil
}

// ServeGRPC starts the gRPC server in a goroutine and returns immediately
func (s *InspectorServer) ServeGRPC(done chan<- struct{}) error {
	l, err := net.Listen("tcp", s.grpcAddr)
	if err != nil {
		return err
	}
	s.grpcServer = grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(s.grpcServer, inspector.New(s.pg))
	reflection.Register(s.grpcServer)
	log.Printf("gRPC on %s", s.grpcAddr)

	// Handle graceful shutdown
	go func() {
		<-s.shutdownChan
		log.Println("Shutting down gRPC server...")
		s.grpcServer.GracefulStop()
	}()

	go func() {
		if err := s.grpcServer.Serve(l); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
		log.Println("gRPC server stopped")
		done <- struct{}{}
	}()

	return nil
}

// handleShardMapping handles GET /shards for shard mapping information
func (s *InspectorServer) handleShardMapping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// If consensus manager is not available, return empty result
	if s.consensusManager == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"shards": []interface{}{},
			"nodes":  []string{},
		})
		return
	}

	// Parse shard mappings
	type ShardInfo struct {
		ShardID     int    `json:"shard_id"`
		TargetNode  string `json:"target_node"`
		CurrentNode string `json:"current_node"`
		ObjectCount int    `json:"object_count"`
		Flags       string `json:"flags,omitempty"`
	}

	shards := make([]ShardInfo, 0)
	nodeSet := make(map[string]bool)

	// Count objects per shard from the graph
	objects := s.pg.GetObjects()
	objectCountPerShard := make(map[int]int)
	for _, obj := range objects {
		objectCountPerShard[obj.ShardID]++
	}

	// Get shard mapping from ConsensusManager (in-memory, already watched)
	shardMapping := s.consensusManager.GetShardMapping()
	if shardMapping != nil {
		for shardID, shardInfo := range shardMapping.Shards {
			shards = append(shards, ShardInfo{
				ShardID:     shardID,
				TargetNode:  shardInfo.TargetNode,
				CurrentNode: shardInfo.CurrentNode,
				ObjectCount: objectCountPerShard[shardID],
				Flags:       strings.Join(shardInfo.Flags, ","),
			})

			if shardInfo.TargetNode != "" {
				nodeSet[shardInfo.TargetNode] = true
			}
			if shardInfo.CurrentNode != "" {
				nodeSet[shardInfo.CurrentNode] = true
			}
		}
	}

	// Get sorted list of nodes
	nodes := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		nodes = append(nodes, node)
	}

	// Sort nodes alphabetically
	sort.Strings(nodes)

	result := map[string]interface{}{
		"shards": shards,
		"nodes":  nodes,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// handleShardMove handles POST /shards/move for moving a shard to a different node
func (s *InspectorServer) handleShardMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// If consensus manager is not available, return error
	if s.consensusManager == nil {
		http.Error(w, "Consensus manager not available. Start inspector with --etcd-addr flag.", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	type MoveShardRequest struct {
		ShardID    int    `json:"shard_id"`
		TargetNode string `json:"target_node"`
	}

	var req MoveShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	log.Printf("Received shard move request: shard_id=%d, target_node=%s", req.ShardID, req.TargetNode)

	// Validate shard ID (shards are 0-indexed and range from 0 to numShards-1)
	if req.ShardID < 0 || req.ShardID >= s.consensusManager.GetNumShards() {
		http.Error(w, fmt.Sprintf("Invalid shard ID: %d", req.ShardID), http.StatusBadRequest)
		return
	}

	// Validate target node is not empty
	if req.TargetNode == "" {
		http.Error(w, "Target node cannot be empty", http.StatusBadRequest)
		return
	}

	// Get current shard mapping
	shardMapping := s.consensusManager.GetShardMapping()
	if shardMapping == nil {
		http.Error(w, "Shard mapping not available", http.StatusInternalServerError)
		return
	}

	currentShardInfo, exists := shardMapping.Shards[req.ShardID]
	if !exists {
		http.Error(w, fmt.Sprintf("Shard %d not found in mapping", req.ShardID), http.StatusNotFound)
		return
	}

	// Prepare update: set TargetNode to the new node, keep CurrentNode and Flags unchanged
	updateShards := make(map[int]consensusmanager.ShardInfo)
	updateShards[req.ShardID] = consensusmanager.ShardInfo{
		TargetNode:  req.TargetNode,
		CurrentNode: currentShardInfo.CurrentNode,
		Flags:       currentShardInfo.Flags,
		ModRevision: currentShardInfo.ModRevision,
	}

	// Use a timeout context for the etcd operation
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Store the updated shard mapping
	successCount, err := s.consensusManager.StoreShardMapping(ctx, updateShards)
	if err != nil {
		log.Printf("Failed to move shard %d to node %s: %v", req.ShardID, req.TargetNode, err)
		http.Error(w, fmt.Sprintf("Failed to move shard: %v", err), http.StatusInternalServerError)
		return
	}

	if successCount == 0 {
		http.Error(w, "Failed to move shard: no shards updated", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully updated shard %d target to %s (current=%s)", req.ShardID, req.TargetNode, currentShardInfo.CurrentNode)

	// Broadcast shard update event to all SSE clients
	s.broadcastShardUpdate()

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success":     true,
		"shard_id":    req.ShardID,
		"target_node": req.TargetNode,
		"message":     fmt.Sprintf("Shard %d target updated to %s", req.ShardID, req.TargetNode),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

// broadcastShardUpdate sends a shard update event to all SSE clients
func (s *InspectorServer) broadcastShardUpdate() {
	event := graph.GraphEvent{
		Type: "shard_update",
	}

	s.sseClientsMu.RLock()
	clients := make([]*SSEClient, 0, len(s.sseClients))
	for _, c := range s.sseClients {
		clients = append(clients, c)
	}
	s.sseClientsMu.RUnlock()

	log.Printf("Broadcasting shard_update to %d SSE clients", len(clients))

	for _, client := range clients {
		select {
		case client.eventChan <- event:
			log.Printf("Sent shard_update to SSE client %s", client.id)
		case <-client.done:
			log.Printf("Client %s already disconnected", client.id)
		default:
			log.Printf("Event dropped for SSE client %s: channel full", client.id)
		}
	}
}

// Shutdown initiates graceful shutdown of all servers
func (s *InspectorServer) Shutdown() {
	close(s.shutdownChan)
	if s.consensusManager != nil {
		s.consensusManager.StopWatch()
	}
	if s.etcdManager != nil {
		s.etcdManager.Close()
	}
}

// CreateHTTPHandler creates the HTTP handler for the inspector web UI (exported for testing)
func CreateHTTPHandler(pg *graph.GoverseGraph, staticDir string) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := pg.GetNodes()
		gates := pg.GetGates()
		objects := pg.GetObjects()
		out := struct {
			GoverseNodes   []GoverseNode        `json:"goverse_nodes"`
			GoverseGates   []models.GoverseGate `json:"goverse_gates"`
			GoverseObjects []GoverseObject      `json:"goverse_objects"`
		}{
			GoverseNodes:   nodes,
			GoverseGates:   gates,
			GoverseObjects: objects,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return mux
}
