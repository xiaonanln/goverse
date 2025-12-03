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
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspector"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

// sseHeartbeatInterval is the interval between SSE heartbeat events
const sseHeartbeatInterval = 30 * time.Second

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

	// etcd connection and consensus manager (optional)
	etcdManager      *etcdmanager.EtcdManager
	consensusManager *consensusmanager.ConsensusManager
	etcdPrefix       string
	numShards        int
}

// Config holds the configuration for InspectorServer
type Config struct {
	GRPCAddr   string
	HTTPAddr   string
	StaticDir  string
	EtcdAddr   string // optional etcd address
	EtcdPrefix string // etcd key prefix
	NumShards  int    // number of shards in the cluster, defaults to sharding.NumShards if not set
}

// New creates a new InspectorServer
func New(pg *graph.GoverseGraph, cfg Config) *InspectorServer {
	// Use default number of shards if not specified
	numShards := cfg.NumShards
	if numShards <= 0 {
		numShards = sharding.NumShards
	}

	s := &InspectorServer{
		pg:           pg,
		grpcAddr:     cfg.GRPCAddr,
		httpAddr:     cfg.HTTPAddr,
		staticDir:    cfg.StaticDir,
		shutdownChan: make(chan struct{}),
		sseClients:   make(map[string]*SSEClient),
		etcdPrefix:   cfg.EtcdPrefix,
		numShards:    numShards,
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

			// Create ConsensusManager to watch cluster state
			shardLock := shardlock.NewShardLock(numShards)
			s.consensusManager = consensusmanager.NewConsensusManager(
				mgr,
				shardLock,
				10*time.Second, // stability duration
				"",             // no local node address for inspector
				numShards,
			)

			// Initialize and start watching
			ctx := context.Background()
			if err := s.consensusManager.Initialize(ctx); err != nil {
				log.Printf("Failed to initialize consensus manager: %v", err)
				s.consensusManager = nil
			} else if err := s.consensusManager.StartWatch(ctx); err != nil {
				log.Printf("Failed to start consensus manager watch: %v", err)
				s.consensusManager = nil
			} else {
				log.Printf("ConsensusManager initialized and watching cluster state")
			}
		}
	}

	// Register as observer to receive graph events
	pg.AddObserver(s)

	return s
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
			if err := s.writeSSEEvent(w, flusher, string(event.Type), event); err != nil {
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

	// Get shard mapping from consensus manager (watched state)
	shardMapping := s.consensusManager.GetShardMapping()
	nodes := s.consensusManager.GetNodes()

	shards := make([]ShardInfo, 0, len(shardMapping.Shards))
	nodeSet := make(map[string]bool)

	// Count objects per shard from the graph
	objects := s.pg.GetObjects()
	objectCountPerShard := make(map[int]int)
	for _, obj := range objects {
		objectCountPerShard[obj.ShardID]++
	}

	// Build shard info from consensus manager's watched state
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

	// Get sorted list of nodes from consensus manager
	sortedNodes := make([]string, 0, len(nodes))
	sortedNodes = append(sortedNodes, nodes...)
	sort.Strings(sortedNodes)

	result := map[string]interface{}{
		"shards": shards,
		"nodes":  sortedNodes,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
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
