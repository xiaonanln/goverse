package graph

import (
	"sync"

	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

type GoverseGraph struct {
	mu      sync.RWMutex
	objects map[string]models.GoverseObject
	nodes   map[string]models.GoverseNode
}

func NewGoverseGraph() *GoverseGraph {
	return &GoverseGraph{
		objects: make(map[string]models.GoverseObject),
		nodes:   make(map[string]models.GoverseNode),
	}
}

// GetNodes returns a copy of all registered nodes.
func (pg *GoverseGraph) GetNodes() []models.GoverseNode {
	pg.mu.RLock()
	defer pg.mu.RUnlock()

	nodes := make([]models.GoverseNode, 0, len(pg.nodes))
	for _, n := range pg.nodes {
		nodes = append(nodes, n)
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
	defer pg.mu.Unlock()

	pg.objects[obj.ID] = obj
}

// AddOrUpdateNode registers or updates a node.
func (pg *GoverseGraph) AddOrUpdateNode(node models.GoverseNode) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	pg.nodes[node.ID] = node
}

// IsNodeRegistered checks if a node with the given address is registered.
func (pg *GoverseGraph) IsNodeRegistered(nodeAddress string) bool {
	pg.mu.RLock()
	defer pg.mu.RUnlock()
	_, exists := pg.nodes[nodeAddress]
	return exists
}

func (pg *GoverseGraph) RemoveNode(goverseNodeID string) {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	delete(pg.nodes, goverseNodeID)
	// Also remove all objects associated with this node
	for id, obj := range pg.objects {
		if obj.GoverseNodeID == goverseNodeID {
			delete(pg.objects, id)
		}
	}
}

// RemoveStaleObjects removes objects associated with the given node ID that are not in the current object ID set.
func (pg *GoverseGraph) RemoveStaleObjects(goverseNodeID string, currentObjs []*inspector_pb.Object) {
	pg.mu.Lock()
	defer pg.mu.Unlock()

	currentIDs := make(map[string]struct{})
	for _, o := range currentObjs {
		if o != nil && o.Id != "" {
			currentIDs[o.Id] = struct{}{}
		}
	}

	for id, obj := range pg.objects {
		if obj.GoverseNodeID == goverseNodeID {
			if _, ok := currentIDs[id]; !ok {
				delete(pg.objects, id)
			}
		}
	}
}
