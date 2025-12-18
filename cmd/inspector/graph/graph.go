package graph

import (
	"sync"

	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
	"github.com/xiaonanln/goverse/util/logger"
)

var log = logger.NewLogger("graph")

// EventType represents the type of graph change event
type EventType string

const (
	EventNodeAdded     EventType = "node_added"
	EventNodeUpdated   EventType = "node_updated"
	EventNodeRemoved   EventType = "node_removed"
	EventGateAdded     EventType = "gate_added"
	EventGateUpdated   EventType = "gate_updated"
	EventGateRemoved   EventType = "gate_removed"
	EventObjectAdded   EventType = "object_added"
	EventObjectUpdated EventType = "object_updated"
	EventObjectRemoved EventType = "object_removed"
	EventObjectCall    EventType = "object_call"
)

// GraphEvent represents a change event in the graph
type GraphEvent struct {
	Type        EventType             `json:"type"`
	Node        *models.GoverseNode   `json:"node,omitempty"`
	Gate        *models.GoverseGate   `json:"gate,omitempty"`
	Object      *models.GoverseObject `json:"object,omitempty"`
	ObjectID    string                `json:"object_id,omitempty"`
	NodeID      string                `json:"node_id,omitempty"`
	GateID      string                `json:"gate_id,omitempty"`
	Method      string                `json:"method,omitempty"`       // For object_call events
	ObjectClass string                `json:"object_class,omitempty"` // For object_call events
	NodeAddress string                `json:"node_address,omitempty"` // For object_call events
}

// Observer is an interface for receiving graph change events
type Observer interface {
	OnGraphEvent(event GraphEvent)
}

type GoverseGraph struct {
	mu        sync.RWMutex
	objects   map[string]models.GoverseObject
	nodes     map[string]models.GoverseNode
	gates     map[string]models.GoverseGate
	observers map[Observer]struct{}
}

func NewGoverseGraph() *GoverseGraph {
	return &GoverseGraph{
		objects:   make(map[string]models.GoverseObject),
		nodes:     make(map[string]models.GoverseNode),
		gates:     make(map[string]models.GoverseGate),
		observers: make(map[Observer]struct{}),
	}
}

// AddObserver registers an observer to receive graph change events
func (pg *GoverseGraph) AddObserver(observer Observer) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.observers[observer] = struct{}{}
}

// RemoveObserver unregisters an observer
func (pg *GoverseGraph) RemoveObserver(observer Observer) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	delete(pg.observers, observer)
}

// notifyObservers sends an event to all registered observers
// Must be called without holding the lock
func (pg *GoverseGraph) notifyObservers(event GraphEvent) {
	pg.mu.RLock()
	observers := make([]Observer, 0, len(pg.observers))
	for obs := range pg.observers {
		observers = append(observers, obs)
	}
	pg.mu.RUnlock()

	log.Debugf("Graph event: %s (node=%s, gate=%s, object=%s)", event.Type, event.NodeID, event.GateID, event.ObjectID)

	for _, obs := range observers {
		obs.OnGraphEvent(event)
	}
}

// GetNodes returns a copy of all registered nodes with their object counts.
func (pg *GoverseGraph) GetNodes() []models.GoverseNode {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	// Count objects per node
	objectCounts := make(map[string]int)
	for _, obj := range pg.objects {
		objectCounts[obj.GoverseNodeID]++
	}

	nodes := make([]models.GoverseNode, 0, len(pg.nodes))
	for _, n := range pg.nodes {
		nodeCopy := n
		nodeCopy.ObjectCount = objectCounts[n.ID]
		nodes = append(nodes, nodeCopy)
	}
	return nodes
}

// GetObjects returns a copy of all registered objects.
func (pg *GoverseGraph) GetObjects() []models.GoverseObject {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	objects := make([]models.GoverseObject, 0, len(pg.objects))
	for _, o := range pg.objects {
		objects = append(objects, o)
	}
	return objects
}

// AddOrUpdateObject adds or replaces an object.
func (pg *GoverseGraph) AddOrUpdateObject(obj models.GoverseObject) {
	pg.mu.Lock()
	_, exists := pg.objects[obj.ID]
	pg.objects[obj.ID] = obj
	pg.mu.Unlock()

	eventType := EventObjectAdded
	if exists {
		eventType = EventObjectUpdated
	}
	pg.notifyObservers(GraphEvent{
		Type:   eventType,
		Object: &obj,
	})
}

// AddOrUpdateNode registers or updates a node.
func (pg *GoverseGraph) AddOrUpdateNode(node models.GoverseNode) {
	pg.mu.Lock()
	_, exists := pg.nodes[node.ID]
	pg.nodes[node.ID] = node
	pg.mu.Unlock()

	eventType := EventNodeAdded
	if exists {
		eventType = EventNodeUpdated
	}
	pg.notifyObservers(GraphEvent{
		Type: eventType,
		Node: &node,
	})
}

// IsNodeRegistered checks if a node with the given address is registered.
func (pg *GoverseGraph) IsNodeRegistered(nodeAddress string) bool {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	_, exists := pg.nodes[nodeAddress]
	return exists
}

// RemoveObject removes a specific object by ID.
func (pg *GoverseGraph) RemoveObject(objectID string) {
	pg.mu.Lock()
	_, exists := pg.objects[objectID]
	delete(pg.objects, objectID)
	pg.mu.Unlock()

	if exists {
		pg.notifyObservers(GraphEvent{
			Type:     EventObjectRemoved,
			ObjectID: objectID,
		})
	}
}

func (pg *GoverseGraph) RemoveNode(goverseNodeID string) {
	pg.mu.Lock()
	_, nodeExists := pg.nodes[goverseNodeID]
	delete(pg.nodes, goverseNodeID)
	// Collect objects to remove
	removedObjects := make([]string, 0)
	for id, obj := range pg.objects {
		if obj.GoverseNodeID == goverseNodeID {
			delete(pg.objects, id)
			removedObjects = append(removedObjects, id)
		}
	}
	pg.mu.Unlock()

	// Notify about removed objects
	for _, objID := range removedObjects {
		pg.notifyObservers(GraphEvent{
			Type:     EventObjectRemoved,
			ObjectID: objID,
		})
	}

	// Notify about removed node
	if nodeExists {
		pg.notifyObservers(GraphEvent{
			Type:   EventNodeRemoved,
			NodeID: goverseNodeID,
		})
	}
}

// RemoveStaleObjects removes objects associated with the given node ID that are not in the current object ID set.
func (pg *GoverseGraph) RemoveStaleObjects(goverseNodeID string, currentObjs []*inspector_pb.Object) {
	currentIDs := make(map[string]struct{})
	for _, o := range currentObjs {
		if o != nil && o.Id != "" {
			currentIDs[o.Id] = struct{}{}
		}
	}

	pg.mu.Lock()
	removedObjects := make([]string, 0)
	for id, obj := range pg.objects {
		if obj.GoverseNodeID == goverseNodeID {
			if _, ok := currentIDs[id]; !ok {
				delete(pg.objects, id)
				removedObjects = append(removedObjects, id)
			}
		}
	}
	pg.mu.Unlock()

	// Notify about removed objects
	for _, objID := range removedObjects {
		pg.notifyObservers(GraphEvent{
			Type:     EventObjectRemoved,
			ObjectID: objID,
		})
	}
}

// GetGates returns a copy of all registered gates.
func (pg *GoverseGraph) GetGates() []models.GoverseGate {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	gates := make([]models.GoverseGate, 0, len(pg.gates))
	for _, g := range pg.gates {
		gates = append(gates, g)
	}
	return gates
}

// AddOrUpdateGate registers or updates a gate.
func (pg *GoverseGraph) AddOrUpdateGate(gate models.GoverseGate) {
	pg.mu.Lock()
	_, exists := pg.gates[gate.ID]
	pg.gates[gate.ID] = gate
	pg.mu.Unlock()

	eventType := EventGateAdded
	if exists {
		eventType = EventGateUpdated
	}
	pg.notifyObservers(GraphEvent{
		Type: eventType,
		Gate: &gate,
	})
}

// IsGateRegistered checks if a gate with the given address is registered.
func (pg *GoverseGraph) IsGateRegistered(gateAddress string) bool {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	_, exists := pg.gates[gateAddress]
	return exists
}

// RemoveGate removes a gate by ID.
func (pg *GoverseGraph) RemoveGate(gateID string) {
	pg.mu.Lock()
	_, gateExists := pg.gates[gateID]
	delete(pg.gates, gateID)
	pg.mu.Unlock()

	// Notify about removed gate
	if gateExists {
		pg.notifyObservers(GraphEvent{
			Type:   EventGateRemoved,
			GateID: gateID,
		})
	}
}

// UpdateNodeConnectedNodes updates the connected_nodes field for a specific node.
func (pg *GoverseGraph) UpdateNodeConnectedNodes(nodeID string, connectedNodes []string) {
	pg.mu.Lock()
	node, exists := pg.nodes[nodeID]
	if !exists {
		pg.mu.Unlock()
		return
	}

	node.ConnectedNodes = connectedNodes
	pg.nodes[nodeID] = node

	// Calculate object count for this node
	objectCount := 0
	for _, obj := range pg.objects {
		if obj.GoverseNodeID == nodeID {
			objectCount++
		}
	}
	node.ObjectCount = objectCount
	pg.mu.Unlock()

	pg.notifyObservers(GraphEvent{
		Type: EventNodeUpdated,
		Node: &node,
	})
}

// UpdateGateConnectedNodes updates the connected_nodes field for a specific gate.
func (pg *GoverseGraph) UpdateGateConnectedNodes(gateID string, connectedNodes []string) {
	pg.mu.Lock()
	gate, exists := pg.gates[gateID]
	if !exists {
		pg.mu.Unlock()
		return
	}

	gate.ConnectedNodes = connectedNodes
	pg.gates[gateID] = gate
	pg.mu.Unlock()

	pg.notifyObservers(GraphEvent{
		Type: EventGateUpdated,
		Gate: &gate,
	})
}

// UpdateGateClients updates the clients field for a specific gate.
func (pg *GoverseGraph) UpdateGateClients(gateID string, clients int) {
	pg.mu.Lock()
	gate, exists := pg.gates[gateID]
	if !exists {
		pg.mu.Unlock()
		return
	}

	gate.Clients = clients
	pg.gates[gateID] = gate
	pg.mu.Unlock()

	pg.notifyObservers(GraphEvent{
		Type: EventGateUpdated,
		Gate: &gate,
	})
}

// UpdateNodeRegisteredGates updates the registered_gates field for a specific node.
func (pg *GoverseGraph) UpdateNodeRegisteredGates(nodeID string, registeredGates []string) {
	pg.mu.Lock()
	node, exists := pg.nodes[nodeID]
	if !exists {
		pg.mu.Unlock()
		return
	}

	node.RegisteredGates = registeredGates
	pg.nodes[nodeID] = node

	// Calculate object count for this node
	objectCount := 0
	for _, obj := range pg.objects {
		if obj.GoverseNodeID == nodeID {
			objectCount++
		}
	}
	node.ObjectCount = objectCount
	pg.mu.Unlock()

	pg.notifyObservers(GraphEvent{
		Type: EventNodeUpdated,
		Node: &node,
	})
}

// BroadcastObjectCall broadcasts an object call event to all observers.
// This does not modify any state in the graph, it only sends a notification.
func (pg *GoverseGraph) BroadcastObjectCall(objectID, objectClass, method, nodeAddress string) {
	// No need to acquire lock since we're not modifying state
	// Just broadcast the event to observers
	pg.notifyObservers(GraphEvent{
		Type:        EventObjectCall,
		ObjectID:    objectID,
		ObjectClass: objectClass,
		Method:      method,
		NodeAddress: nodeAddress,
	})
}
