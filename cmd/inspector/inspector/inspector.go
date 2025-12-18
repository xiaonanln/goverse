package inspector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
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

// callMetric represents a single call metric entry
type callMetric struct {
	timestamp time.Time
	duration  int32 // duration in milliseconds
}

// objectCallMetrics tracks call metrics for an object
type objectCallMetrics struct {
	calls []callMetric // rolling window of calls (last 1 minute)
	mu    sync.RWMutex
}

// Inspector implements the inspector gRPC service and manages inspector logic
type Inspector struct {
	inspector_pb.UnimplementedInspectorServiceServer
	pg          *graph.GoverseGraph
	callMetrics map[string]*objectCallMetrics // objectID -> metrics
	metricsMu   sync.RWMutex
}

// New creates a new Inspector
func New(pg *graph.GoverseGraph) *Inspector {
	i := &Inspector{
		pg:          pg,
		callMetrics: make(map[string]*objectCallMetrics),
	}

	// Start background cleanup goroutine for old metrics
	go i.cleanupOldMetrics()

	// Start background goroutine for periodic metrics refresh
	go i.refreshObjectMetrics()

	return i
}

// refreshObjectMetrics periodically updates objects with fresh metrics
func (i *Inspector) refreshObjectMetrics() {
	ticker := time.NewTicker(metricsRefreshInterval)
	defer ticker.Stop()

	for range ticker.C {
		// Get all objects
		objects := i.pg.GetObjects()

		// Update each object with fresh metrics
		for _, obj := range objects {
			callsPerMin, avgDur := i.getCallMetrics(obj.ID)

			// Only update if metrics have changed
			if obj.CallsPerMinute != callsPerMin || obj.AvgExecutionDurationMs != avgDur {
				obj.CallsPerMinute = callsPerMin
				obj.AvgExecutionDurationMs = avgDur
				i.pg.AddOrUpdateObject(obj)
			}
		}
	}
}

const (
	// metricsCleanupInterval is how often old metrics are cleaned up
	metricsCleanupInterval = 30 * time.Second
	// metricsRetentionDuration is how long metrics are kept in memory
	metricsRetentionDuration = 2 * time.Minute
	// metricsRefreshInterval is how often object metrics are refreshed
	metricsRefreshInterval = 15 * time.Second
)

// cleanupOldMetrics periodically removes old call metrics
func (i *Inspector) cleanupOldMetrics() {
	ticker := time.NewTicker(metricsCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		cutoff := now.Add(-metricsRetentionDuration)

		// Collect object IDs to delete
		toDelete := make([]string, 0)

		i.metricsMu.Lock()
		for objectID, metrics := range i.callMetrics {
			metrics.mu.Lock()
			// Remove calls older than cutoff
			validCalls := make([]callMetric, 0, len(metrics.calls))
			for _, call := range metrics.calls {
				if call.timestamp.After(cutoff) {
					validCalls = append(validCalls, call)
				}
			}
			metrics.calls = validCalls

			// Mark for deletion if empty
			if len(metrics.calls) == 0 {
				toDelete = append(toDelete, objectID)
			}
			metrics.mu.Unlock()
		}

		// Delete empty metrics objects
		for _, objectID := range toDelete {
			delete(i.callMetrics, objectID)
		}
		i.metricsMu.Unlock()
	}
}

// getCallMetrics calculates calls per minute and average duration for an object
func (i *Inspector) getCallMetrics(objectID string) (callsPerMinute int, avgDuration float64) {
	i.metricsMu.RLock()
	metrics, exists := i.callMetrics[objectID]
	i.metricsMu.RUnlock()

	if !exists {
		return 0, 0
	}

	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	now := time.Now()
	oneMinuteAgo := now.Add(-1 * time.Minute)

	var count int
	var totalDuration int64

	for _, call := range metrics.calls {
		if call.timestamp.After(oneMinuteAgo) {
			count++
			totalDuration += int64(call.duration)
		}
	}

	if count > 0 {
		avgDuration = float64(totalDuration) / float64(count)
	}

	return count, avgDuration
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

	// Get call metrics for this object
	callsPerMin, avgDur := i.getCallMetrics(o.Id)

	obj := GoverseObject{
		ID:                     o.Id,
		Label:                  fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
		Size:                   10,
		Color:                  "#1f77b4",
		Type:                   o.GetClass(),
		GoverseNodeID:          nodeAddress,
		ShardID:                int(o.ShardId),
		CallsPerMinute:         callsPerMin,
		AvgExecutionDurationMs: avgDur,
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

	node := GoverseNode{
		ID:              addr,
		Label:           fmt.Sprintf("Node %s", addr),
		Color:           "#4CAF50",
		Type:            "goverse_node",
		AdvertiseAddr:   addr,
		RegisteredAt:    time.Now(),
		ConnectedNodes:  req.GetConnectedNodes(),
		RegisteredGates: req.GetRegisteredGates(),
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
		// Get call metrics for this object
		callsPerMin, avgDur := i.getCallMetrics(o.Id)

		obj := GoverseObject{
			ID:                     o.Id,
			Label:                  fmt.Sprintf("%s (%s)", o.GetClass(), o.GetId()),
			Size:                   10,
			Color:                  "#1f77b4",
			Type:                   o.GetClass(),
			GoverseNodeID:          addr,
			ShardID:                int(o.ShardId),
			CallsPerMinute:         callsPerMin,
			AvgExecutionDurationMs: avgDur,
		}
		i.pg.AddOrUpdateObject(obj)
	}

	log.Printf("Node registered: advertise_addr=%s, connected_nodes=%v, registered_gates=%v", addr, req.GetConnectedNodes(), req.GetRegisteredGates())
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

	gate := GoverseGate{
		ID:             addr,
		Label:          fmt.Sprintf("Gate %s", addr),
		Color:          "#2196F3",
		Type:           "goverse_gate",
		AdvertiseAddr:  addr,
		RegisteredAt:   time.Now(),
		ConnectedNodes: req.GetConnectedNodes(),
		Clients:        int(req.GetClients()),
	}
	i.pg.AddOrUpdateGate(gate)

	log.Printf("Gate registered: advertise_addr=%s, connected_nodes=%v, clients=%d", addr, req.GetConnectedNodes(), req.GetClients())
	return &inspector_pb.RegisterGateResponse{}, nil
}

// UnregisterGate handles gate unregistration requests
func (i *Inspector) UnregisterGate(ctx context.Context, req *inspector_pb.UnregisterGateRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()

	i.pg.RemoveGate(addr)
	log.Printf("Gate unregistered: advertise_addr=%s", addr)
	return &inspector_pb.Empty{}, nil
}

// UpdateConnectedNodes handles connected nodes update requests for both nodes and gates.
// It uses the advertise address to determine whether the request is from a node or gate.
func (i *Inspector) UpdateConnectedNodes(ctx context.Context, req *inspector_pb.UpdateConnectedNodesRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("UpdateConnectedNodes called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	connectedNodes := req.GetConnectedNodes()
	if connectedNodes == nil {
		connectedNodes = []string{}
	}

	// Check if this is a node or gate by looking up in the graph
	// Try to update as node first, then as gate
	if i.pg.IsNodeRegistered(addr) {
		i.pg.UpdateNodeConnectedNodes(addr, connectedNodes)
		log.Printf("Node connected_nodes updated: advertise_addr=%s, connected_nodes=%v", addr, connectedNodes)
	} else if i.pg.IsGateRegistered(addr) {
		i.pg.UpdateGateConnectedNodes(addr, connectedNodes)
		log.Printf("Gate connected_nodes updated: advertise_addr=%s, connected_nodes=%v", addr, connectedNodes)
	} else {
		// Neither node nor gate is registered, log but don't error
		log.Printf("UpdateConnectedNodes: no node or gate registered with address %s", addr)
	}

	return &inspector_pb.Empty{}, nil
}

// UpdateGateClients handles gate client count update requests.
func (i *Inspector) UpdateGateClients(ctx context.Context, req *inspector_pb.UpdateGateClientsRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("UpdateGateClients called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	clients := int(req.GetClients())

	// Update gate clients in the graph
	if i.pg.IsGateRegistered(addr) {
		i.pg.UpdateGateClients(addr, clients)
		log.Printf("Gate clients updated: advertise_addr=%s, clients=%d", addr, clients)
	} else {
		log.Printf("UpdateGateClients: no gate registered with address %s", addr)
	}

	return &inspector_pb.Empty{}, nil
}

// UpdateRegisteredGates handles registered gates update requests from nodes.
func (i *Inspector) UpdateRegisteredGates(ctx context.Context, req *inspector_pb.UpdateRegisteredGatesRequest) (*inspector_pb.Empty, error) {
	addr := req.GetAdvertiseAddress()
	if addr == "" {
		log.Println("UpdateRegisteredGates called with empty advertise address")
		return nil, errors.New("advertise address cannot be empty")
	}

	registeredGates := req.GetRegisteredGates()
	if registeredGates == nil {
		registeredGates = []string{}
	}

	// Only nodes can have registered gates
	if i.pg.IsNodeRegistered(addr) {
		i.pg.UpdateNodeRegisteredGates(addr, registeredGates)
		log.Printf("Node registered_gates updated: advertise_addr=%s, registered_gates=%v", addr, registeredGates)
	} else {
		// Node is not registered, log but don't error
		log.Printf("UpdateRegisteredGates: no node registered with address %s", addr)
	}

	return &inspector_pb.Empty{}, nil
}

// ReportObjectCall handles object call reports from nodes.
func (i *Inspector) ReportObjectCall(ctx context.Context, req *inspector_pb.ReportObjectCallRequest) (*inspector_pb.Empty, error) {
	objectID := req.GetObjectId()
	if objectID == "" {
		return &inspector_pb.Empty{}, nil
	}

	objectClass := req.GetObjectClass()
	method := req.GetMethod()
	nodeAddress := req.GetNodeAddress()
	duration := req.GetExecutionDurationMs()

	// Check if the node is registered
	if !i.pg.IsNodeRegistered(nodeAddress) {
		// Silently ignore calls from unregistered nodes (may occur during startup/shutdown)
		return &inspector_pb.Empty{}, nil
	}

	// Record call metrics
	i.metricsMu.Lock()
	metrics, exists := i.callMetrics[objectID]
	if !exists {
		metrics = &objectCallMetrics{
			calls: make([]callMetric, 0, 100),
		}
		i.callMetrics[objectID] = metrics
	}
	i.metricsMu.Unlock()

	metrics.mu.Lock()
	metrics.calls = append(metrics.calls, callMetric{
		timestamp: time.Now(),
		duration:  duration,
	})
	metrics.mu.Unlock()

	// Broadcast the call event
	i.pg.BroadcastObjectCall(objectID, objectClass, method, nodeAddress)

	log.Printf("Object call reported: object_id=%s, class=%s, method=%s, node=%s, duration=%dms", objectID, objectClass, method, nodeAddress, duration)
	return &inspector_pb.Empty{}, nil
}
