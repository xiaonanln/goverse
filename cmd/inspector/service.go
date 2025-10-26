package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject

// InspectorService implements the inspector gRPC service
type InspectorService struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg *graph.GoverseGraph
}

// NewInspectorService creates a new inspector service
func NewInspectorService(pg *graph.GoverseGraph) *InspectorService {
	return &InspectorService{pg: pg}
}

func randPos() (int, int) {
	return rand.Intn(200), rand.Intn(200)
}

// Ping handles ping requests
func (s *InspectorService) Ping(ctx context.Context, req *inspector_pb.Empty) (*inspector_pb.Empty, error) {
	return &inspector_pb.Empty{}, nil
}

// AddOrUpdateObject handles object addition/update requests
func (s *InspectorService) AddOrUpdateObject(ctx context.Context, req *inspector_pb.AddOrUpdateObjectRequest) (*inspector_pb.Empty, error) {
	o := req.GetObject()
	if o == nil || o.Id == "" {
		return &inspector_pb.Empty{}, nil
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
		GoverseNodeID: req.GetNodeAddress(),
	}
	s.pg.AddOrUpdateObject(obj)
	return &inspector_pb.Empty{}, nil
}

// RegisterNode handles node registration requests
func (s *InspectorService) RegisterNode(ctx context.Context, req *inspector_pb.RegisterNodeRequest) (*inspector_pb.RegisterNodeResponse, error) {
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

// UnregisterNode handles node unregistration requests
func (s *InspectorService) UnregisterNode(ctx context.Context, req *inspector_pb.UnregisterNodeRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()

	s.pg.RemoveNode(addr)
	log.Printf("Node unregistered: advertise_addr=%s", addr)
	return &inspector_pb.Empty{}, nil
}

// CreateHTTPHandler creates the HTTP handler for the inspector web UI
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
