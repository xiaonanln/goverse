package graph

import (
	"sync"

	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

type PulseGraph struct {
	mu      sync.RWMutex
	objects map[string]models.PulseObject
	nodes   map[string]models.PulseNode
}

func NewPulseGraph() *PulseGraph {
	return &PulseGraph{
		objects: make(map[string]models.PulseObject),
		nodes:   make(map[string]models.PulseNode),
	}
}

// GetNodes returns a copy of all registered nodes.
func (pg *PulseGraph) GetNodes() []models.PulseNode {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	nodes := make([]models.PulseNode, 0, len(pg.nodes))
	for _, n := range pg.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

// GetObjects returns a copy of all registered objects.
func (pg *PulseGraph) GetObjects() []models.PulseObject {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	objects := make([]models.PulseObject, 0, len(pg.objects))
	for _, o := range pg.objects {
		objects = append(objects, o)
	}
	return objects
}

// AddObject adds an object if it does not already exist.
func (pg *PulseGraph) AddObject(obj models.PulseObject) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	if _, exists := pg.objects[obj.ID]; !exists {
		pg.objects[obj.ID] = obj
	}
}

// AddOrUpdateNode registers or updates a node.
func (pg *PulseGraph) AddOrUpdateNode(node models.PulseNode) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.nodes[node.ID] = node
}

func (pg *PulseGraph) RemoveNode(pulseNodeID string) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	delete(pg.nodes, pulseNodeID)
	// Also remove all objects associated with this node
	for id, obj := range pg.objects {
		if obj.PulseNodeID == pulseNodeID {
			delete(pg.objects, id)
		}
	}
}

// RemoveStaleObjects removes objects associated with the given node ID that are not in the current object ID set.
func (pg *PulseGraph) RemoveStaleObjects(pulseNodeID string, currentObjs []*inspector_pb.Object) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	currentIDs := make(map[string]struct{})
	for _, o := range currentObjs {
		if o != nil && o.Id != "" {
			currentIDs[o.Id] = struct{}{}
		}
	}

	for id, obj := range pg.objects {
		if obj.PulseNodeID == pulseNodeID {
			if _, ok := currentIDs[id]; !ok {
				delete(pg.objects, id)
			}
		}
	}
}
