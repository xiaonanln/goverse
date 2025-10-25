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
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/simonlingoogle/pulse/cmd/inspector/graph"
	"github.com/simonlingoogle/pulse/cmd/inspector/models"
	inspector_pb "github.com/simonlingoogle/pulse/inspector/proto"
)

type PulseNode = models.PulseNode
type PulseObject = models.PulseObject

func randPos() (int, int) {
	return rand.Intn(200), rand.Intn(200)
}

type svc struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg *graph.PulseGraph
}

func (s *svc) Ping(ctx context.Context, req *inspector_pb.Empty) (*inspector_pb.Empty, error) {
	return &inspector_pb.Empty{}, nil
}

func (s *svc) AddObject(ctx context.Context, req *inspector_pb.AddObjectRequest) (*inspector_pb.Empty, error) {
	o := req.GetObject()
	if o == nil || o.Id == "" {
		return &inspector_pb.Empty{}, nil
	}
	x, y := randPos()
	obj := PulseObject{
		ID:    o.Id,
		Label: fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
		X:     x, Y: y,
		Size:        10,
		Color:       "#1f77b4",
		Type:        "object",
		PulseNodeID: req.GetNodeAddress(),
	}
	s.pg.AddObject(obj)
	return &inspector_pb.Empty{}, nil
}

func (s *svc) RegisterNode(ctx context.Context, req *inspector_pb.RegisterNodeRequest) (*inspector_pb.RegisterNodeResponse, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("RegisterNode called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	x, y := randPos()
	node := PulseNode{
		ID:            addr,
		Label:         fmt.Sprintf("Node %s", addr),
		X:             x,
		Y:             y,
		Width:         120,
		Height:        80,
		Color:         "#4CAF50",
		Type:          "pulse_node",
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
		obj := PulseObject{
			ID:          o.Id,
			Label:       fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
			X:           x,
			Y:           y,
			Size:        10,
			Color:       "#1f77b4",
			Type:        "object",
			PulseNodeID: addr,
		}
		s.pg.AddObject(obj)
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

func serveHTTP(pg *graph.PulseGraph, addr string) {
	mux := http.NewServeMux()
	staticDir := "inspector/web"

	mux.Handle("/", http.FileServer(http.Dir(staticDir)))
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := pg.GetNodes()
		objects := pg.GetObjects()
		out := struct {
			PulseNodes   []PulseNode   `json:"pulse_nodes"`
			PulseObjects []PulseObject `json:"pulse_objects"`
		}{
			PulseNodes:   nodes,
			PulseObjects: objects,
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
	log.Fatal(srv.ListenAndServe())
}

func serveGRPC(pg *graph.PulseGraph, addr string) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	g := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(g, &svc{pg: pg})
	reflection.Register(g)
	log.Printf("gRPC on %s", addr)
	log.Fatal(g.Serve(l))
}

func main() {
	pg := graph.NewPulseGraph()
	go serveGRPC(pg, ":8081")
	serveHTTP(pg, ":8080")
}
