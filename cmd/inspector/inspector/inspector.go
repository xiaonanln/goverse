package inspector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GoverseNode = models.GoverseNode
type GoverseObject = models.GoverseObject
type GoverseGate = models.GoverseGate

// Inspector implements the inspector gRPC service and manages inspector logic
type Inspector struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg *graph.GoverseGraph
}

// New creates a new Inspector
func New(pg *graph.GoverseGraph) *Inspector {
	return &Inspector{pg: pg}
}

func randPos() (int, int) {
	return rand.Intn(200), rand.Intn(200)
}

// Ping handles ping requests
func (i *Inspector) Ping(ctx context.Context, req *inspector_pb.Empty) (*inspector_pb.Empty, error) {
	return &inspector_pb.Empty{}, nil
}

// AddOrUpdateObject handles object addition/update requests
func (i *Inspector) AddOrUpdateObject(ctx context.Context, req *inspector_pb.AddOrUpdateObjectRequest) (*inspector_pb.Empty, error) {
	o := req.GetObject()
	if o == nil || o.Id == "" {
		return &inspector_pb.Empty{}, nil
	}

	// Check if the node is registered
	nodeAddress := req.GetNodeAddress()
	if !i.pg.IsNodeRegistered(nodeAddress) {
		return nil, status.Errorf(codes.NotFound, "node not registered")
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
		GoverseNodeID: nodeAddress,
		ShardID:       int(o.ShardId),
	}
	i.pg.AddOrUpdateObject(obj)
	return &inspector_pb.Empty{}, nil
}

// RemoveObject handles object removal requests
func (i *Inspector) RemoveObject(ctx context.Context, req *inspector_pb.RemoveObjectRequest) (*inspector_pb.Empty, error) {
	objectID := req.GetObjectId()
	if objectID == "" {
		return &inspector_pb.Empty{}, nil
	}

	// Check if the node is registered
	nodeAddress := req.GetNodeAddress()
	if !i.pg.IsNodeRegistered(nodeAddress) {
		return nil, status.Errorf(codes.NotFound, "node not registered")
	}

	i.pg.RemoveObject(objectID)
	log.Printf("Object removed: object_id=%s, node=%s", objectID, nodeAddress)
	return &inspector_pb.Empty{}, nil
}

// RegisterNode handles node registration requests
func (i *Inspector) RegisterNode(ctx context.Context, req *inspector_pb.RegisterNodeRequest) (*inspector_pb.RegisterNodeResponse, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("RegisterNode called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	x, y := randPos()
	node := GoverseNode{
		ID:             addr,
		Label:          fmt.Sprintf("Node %s", addr),
		X:              x,
		Y:              y,
		Width:          120,
		Height:         80,
		Color:          "#4CAF50",
		Type:           "goverse_node",
		AdvertiseAddr:  addr,
		RegisteredAt:   time.Now(),
		ConnectedNodes: req.GetConnectedNodes(),
	}
	i.pg.AddOrUpdateNode(node)

	currentObjIDs := make(map[string]struct{})
	for _, o := range req.GetObjects() {
		if o != nil && o.Id != "" {
			currentObjIDs[o.Id] = struct{}{}
		}
	}
	i.pg.RemoveStaleObjects(addr, req.GetObjects())

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
			ShardID:       int(o.ShardId),
		}
		i.pg.AddOrUpdateObject(obj)
	}

	log.Printf("Node registered: advertise_addr=%s, connected_nodes=%v", addr, req.GetConnectedNodes())
	return &inspector_pb.RegisterNodeResponse{}, nil
}

// UnregisterNode handles node unregistration requests
func (i *Inspector) UnregisterNode(ctx context.Context, req *inspector_pb.UnregisterNodeRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()

	i.pg.RemoveNode(addr)
	log.Printf("Node unregistered: advertise_addr=%s", addr)
	return &inspector_pb.Empty{}, nil
}

// RegisterGate handles gate registration requests
func (i *Inspector) RegisterGate(ctx context.Context, req *inspector_pb.RegisterGateRequest) (*inspector_pb.RegisterGateResponse, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("RegisterGate called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	x, y := randPos()
	gate := GoverseGate{
		ID:            addr,
		Label:         fmt.Sprintf("Gate %s", addr),
		X:             x,
		Y:             y,
		Width:         120,
		Height:        80,
		Color:         "#2196F3",
		Type:          "goverse_gate",
		AdvertiseAddr: addr,
		RegisteredAt:  time.Now(),
	}
	i.pg.AddOrUpdateGate(gate)

	log.Printf("Gate registered: advertise_addr=%s", addr)
	return &inspector_pb.RegisterGateResponse{}, nil
}

// UnregisterGate handles gate unregistration requests
func (i *Inspector) UnregisterGate(ctx context.Context, req *inspector_pb.UnregisterGateRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()

	i.pg.RemoveGate(addr)
	log.Printf("Gate unregistered: advertise_addr=%s", addr)
	return &inspector_pb.Empty{}, nil
}
