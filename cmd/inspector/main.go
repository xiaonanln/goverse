package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

func randPos() (int, int) {
	return rand.Intn(200), rand.Intn(200)
}

type svc struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg *graph.GoverseGraph
}

func (s *svc) Ping(ctx context.Context, req *inspector_pb.Empty) (*inspector_pb.Empty, error) {
	return &inspector_pb.Empty{}, nil
}

func (s *svc) AddOrUpdateObject(ctx context.Context, req *inspector_pb.AddOrUpdateObjectRequest) (*inspector_pb.Empty, error) {
	o := req.GetObject()
	if o == nil || o.Id == "" {
		return &inspector_pb.Empty{}, nil
	}
	x, y := randPos()
	obj := GoverseObject{
		ID:    o.Id,
		Label: fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
		X:     x, Y: y,
		Size:          10,
		Color:         "#1f77b4",
		Type:          "object",
		GoverseNodeID: req.GetNodeAddress(),
	}
	s.pg.AddOrUpdateObject(obj)
	return &inspector_pb.Empty{}, nil
}

func (s *svc) RegisterNode(ctx context.Context, req *inspector_pb.RegisterNodeRequest) (*inspector_pb.RegisterNodeResponse, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("RegisterNode called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	x, y := randPos()
	node := GoverseNode{
		ID:            addr,
		Label:         fmt.Sprintf("Node %s", addr),
		X:             x,
		Y:             y,
		Width:         120,
		Height:        80,
		Color:         "#4CAF50",
		Type:          "goverse_node",
		AdvertiseAddr: addr,
		RegisteredAt:  time.Now(),
	}
	s.pg.AddOrUpdateNode(node)

	currentObjIDs := make(map[string]struct{})
	for _, o := range req.GetObjects() {
		if o != nil && o.Id != "" {
			currentObjIDs[o.Id] = struct{}{}
		}
	}
	s.pg.RemoveStaleObjects(addr, req.GetObjects())

	for _, o := range req.GetObjects() {
		if o == nil || o.Id == "" {
			continue
		}
		x, y := randPos()
		obj := GoverseObject{
			ID:            o.Id,
			Label:         fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
			X:             x,
			Y:             y,
			Size:          10,
			Color:         "#1f77b4",
			Type:          "object",
			GoverseNodeID: addr,
		}
		s.pg.AddOrUpdateObject(obj)
	}

	log.Printf("Node registered: advertise_addr=%s", addr)
	return &inspector_pb.RegisterNodeResponse{}, nil
}

func (s *svc) UnregisterNode(ctx context.Context, req *inspector_pb.UnregisterNodeRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()

	s.pg.RemoveNode(addr)
	log.Printf("Node unregistered: advertise_addr=%s", addr)
	return &inspector_pb.Empty{}, nil
}

func serveHTTP(pg *graph.GoverseGraph, addr string, shutdownChan chan struct{}) error {
	mux := http.NewServeMux()
	staticDir := "inspector/web"

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

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HTTP on %s (serving %s)", addr, filepath.Join(".", staticDir))

	// Handle graceful shutdown
	go func() {
		<-shutdownChan
		log.Println("Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func serveGRPC(pg *graph.GoverseGraph, addr string, shutdownChan chan struct{}) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(g, &svc{pg: pg})
	reflection.Register(g)
	log.Printf("gRPC on %s", addr)

	// Handle graceful shutdown
	go func() {
		<-shutdownChan
		log.Println("Shutting down gRPC server...")
		g.GracefulStop()
	}()

	if err := g.Serve(l); err != nil {
		return err
	}
	return nil
}

func main() {
	pg := graph.NewGoverseGraph()

	// Create shutdown channel
	shutdownChan := make(chan struct{})

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Track server completion
	serversDone := make(chan struct{}, 2)

	// Start gRPC server
	go func() {
		if err := serveGRPC(pg, ":8081", shutdownChan); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
		log.Println("gRPC server stopped")
		serversDone <- struct{}{}
	}()

	// Start HTTP server in a goroutine
	go func() {
		if err := serveHTTP(pg, ":8080", shutdownChan); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
		log.Println("HTTP server stopped")
		serversDone <- struct{}{}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal")
	close(shutdownChan)

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
