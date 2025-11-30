package inspectserver

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/inspector"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

// InspectorServer hosts the gRPC and HTTP servers for the inspector
type InspectorServer struct {
	pg           *graph.GoverseGraph
	grpcServer   *grpc.Server
	httpServer   *http.Server
	grpcAddr     string
	httpAddr     string
	staticDir    string
	shutdownChan chan struct{}
}

// Config holds the configuration for InspectorServer
type Config struct {
	GRPCAddr  string
	HTTPAddr  string
	StaticDir string
}

// New creates a new InspectorServer
func New(pg *graph.GoverseGraph, cfg Config) *InspectorServer {
	return &InspectorServer{
		pg:           pg,
		grpcAddr:     cfg.GRPCAddr,
		httpAddr:     cfg.HTTPAddr,
		staticDir:    cfg.StaticDir,
		shutdownChan: make(chan struct{}),
	}
}

// createHTTPHandler creates the HTTP handler for the inspector web UI
func (s *InspectorServer) createHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir(s.staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := s.pg.GetNodes()
		objects := s.pg.GetObjects()
		out := struct {
			GoverseNodes   []GoverseNode   `json:"goverse_nodes"`
			GoverseObjects []GoverseObject `json:"goverse_objects"`
		}{
			GoverseNodes:   nodes,
			GoverseObjects: objects,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return mux
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

// Shutdown initiates graceful shutdown of all servers
func (s *InspectorServer) Shutdown() {
	close(s.shutdownChan)
}

// CreateHTTPHandler creates the HTTP handler for the inspector web UI (exported for testing)
func CreateHTTPHandler(pg *graph.GoverseGraph, staticDir string) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := pg.GetNodes()
		objects := pg.GetObjects()
		out := struct {
			GoverseNodes   []GoverseNode   `json:"goverse_nodes"`
			GoverseObjects []GoverseObject `json:"goverse_objects"`
		}{
			GoverseNodes:   nodes,
			GoverseObjects: objects,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	return mux
}
