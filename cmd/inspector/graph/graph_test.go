package graph

import (
	"fmt"
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/cmd/inspector/proto"
)

// TestNewGoverseGraph tests the constructor
func TestNewGoverseGraph(t *testing.T) {
	pg := NewGoverseGraph(0)

	if pg == nil {
		t.Fatal("NewGoverseGraph(0) returned nil")
	}

	if pg.objects == nil {
		t.Fatal("objects map should be initialized")
	}

	if pg.nodes == nil {
		t.Fatal("nodes map should be initialized")
	}

	if pg.gates == nil {
		t.Fatal("gates map should be initialized")
	}

	if len(pg.objects) != 0 {
		t.Fatalf("objects map should be empty, got %d items", len(pg.objects))
	}

	if len(pg.nodes) != 0 {
		t.Fatalf("nodes map should be empty, got %d items", len(pg.nodes))
	}

	if len(pg.gates) != 0 {
		t.Fatalf("gates map should be empty, got %d items", len(pg.gates))
	}

	// Test default numShards (when 0 is passed, default to 8192)
	if pg.GetNumShards() != 8192 {
		t.Fatalf("Expected default numShards 8192, got %d", pg.GetNumShards())
	}

	// Test with custom numShards
	pg2 := NewGoverseGraph(64)
	if pg2.GetNumShards() != 64 {
		t.Fatalf("Expected numShards 64, got %d", pg2.GetNumShards())
	}
}

// TestGetNodes tests retrieving all nodes
func TestGetNodes(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Test with empty graph
	nodes := pg.GetNodes()
	if nodes == nil {
		t.Fatal("GetNodes() should return non-nil slice")
	}
	if len(nodes) != 0 {
		t.Fatalf("GetNodes() should return empty slice for empty graph, got %d items", len(nodes))
	}

	// Add some nodes
	node1 := models.GoverseNode{ID: "node1", Label: "Node 1"}
	node2 := models.GoverseNode{ID: "node2", Label: "Node 2"}
	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2)

	nodes = pg.GetNodes()
	if len(nodes) != 2 {
		t.Fatalf("GetNodes() should return 2 nodes, got %d", len(nodes))
	}

	// Verify it returns a copy (modifying returned slice shouldn't affect internal state)
	nodes[0].Label = "Modified"
	nodes = pg.GetNodes()
	found := false
	for _, n := range nodes {
		if n.ID == "node1" && n.Label == "Node 1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("GetNodes() should return a copy, original data was modified")
	}
}

// TestGetObjects tests retrieving all objects
func TestGetObjects(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Test with empty graph
	objects := pg.GetObjects()
	if objects == nil {
		t.Fatal("GetObjects() should return non-nil slice")
	}
	if len(objects) != 0 {
		t.Fatalf("GetObjects() should return empty slice for empty graph, got %d items", len(objects))
	}

	// Add some objects
	obj1 := models.GoverseObject{ID: "obj1", Label: "Object 1"}
	obj2 := models.GoverseObject{ID: "obj2", Label: "Object 2"}
	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)

	objects = pg.GetObjects()
	if len(objects) != 2 {
		t.Fatalf("GetObjects() should return 2 objects, got %d", len(objects))
	}

	// Verify it returns a copy (modifying returned slice shouldn't affect internal state)
	objects[0].Label = "Modified"
	objects = pg.GetObjects()
	found := false
	for _, o := range objects {
		if o.ID == "obj1" && o.Label == "Object 1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("GetObjects() should return a copy, original data was modified")
	}
}

// TestAddOrUpdateObject tests adding objects
func TestAddOrUpdateObject(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj := models.GoverseObject{
		ID:            "test-obj-1",
		Label:         "Test Object 1",
		GoverseNodeID: "node1",
	}

	pg.AddOrUpdateObject(obj)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object, got %d", len(objects))
	}

	if objects[0].ID != "test-obj-1" {
		t.Fatalf("Expected object ID 'test-obj-1', got '%s'", objects[0].ID)
	}

	if objects[0].Label != "Test Object 1" {
		t.Fatalf("Expected object label 'Test Object 1', got '%s'", objects[0].Label)
	}

	if objects[0].GoverseNodeID != "node1" {
		t.Fatalf("Expected GoverseNodeID 'node1', got '%s'", objects[0].GoverseNodeID)
	}
}

// TestAddOrUpdateObject_Duplicate tests that duplicate objects are replaced
func TestAddOrUpdateObject_Duplicate(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{
		ID:    "test-obj-1",
		Label: "First Version",
	}

	obj2 := models.GoverseObject{
		ID:    "test-obj-1",
		Label: "Second Version",
	}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2) // Should replace the first one

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object, got %d", len(objects))
	}

	// Verify the object was replaced with the newer version
	if objects[0].Label != "Second Version" {
		t.Fatalf("Expected label 'Second Version', got '%s' - duplicate should replace existing", objects[0].Label)
	}
}

// TestAddOrUpdateNode tests adding and updating nodes
func TestAddOrUpdateNode(t *testing.T) {
	pg := NewGoverseGraph(0)

	node := models.GoverseNode{
		ID:            "node1",
		Label:         "Node 1",
		AdvertiseAddr: "localhost:47000",
	}

	pg.AddOrUpdateNode(node)

	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	if nodes[0].ID != "node1" {
		t.Fatalf("Expected node ID 'node1', got '%s'", nodes[0].ID)
	}

	if nodes[0].Label != "Node 1" {
		t.Fatalf("Expected node label 'Node 1', got '%s'", nodes[0].Label)
	}
}

// TestAddOrUpdateNode_Update tests that existing nodes are updated
func TestAddOrUpdateNode_Update(t *testing.T) {
	pg := NewGoverseGraph(0)

	node1 := models.GoverseNode{
		ID:            "node1",
		Label:         "Original Label",
		AdvertiseAddr: "localhost:47000",
	}

	node2 := models.GoverseNode{
		ID:            "node1",
		Label:         "Updated Label",
		AdvertiseAddr: "localhost:47001",
	}

	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2) // Should update the existing node

	nodes := pg.GetNodes()
	if len(nodes) != 1 {
		t.Fatalf("Expected 1 node, got %d", len(nodes))
	}

	// Verify the node was updated
	if nodes[0].Label != "Updated Label" {
		t.Fatalf("Expected label 'Updated Label', got '%s'", nodes[0].Label)
	}

	if nodes[0].AdvertiseAddr != "localhost:47001" {
		t.Fatalf("Expected address 'localhost:47001', got '%s'", nodes[0].AdvertiseAddr)
	}
}

// TestRemoveObject tests removing a specific object
func TestRemoveObject(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj := models.GoverseObject{ID: "obj1", Label: "Object 1", GoverseNodeID: "node1"}
	pg.AddOrUpdateObject(obj)

	// Verify object exists
	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object before removal, got %d", len(objects))
	}

	// Remove the object
	pg.RemoveObject("obj1")

	// Verify object was removed
	objects = pg.GetObjects()
	if len(objects) != 0 {
		t.Fatalf("Expected 0 objects after removal, got %d", len(objects))
	}
}

// TestRemoveObject_NonExistent tests removing a non-existent object
func TestRemoveObject_NonExistent(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Should not panic when removing non-existent object
	pg.RemoveObject("non-existent-object")

	objects := pg.GetObjects()
	if len(objects) != 0 {
		t.Fatalf("Expected 0 objects, got %d", len(objects))
	}
}

// TestRemoveObject_MultipleObjects tests removing one object from many
func TestRemoveObject_MultipleObjects(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{ID: "obj1", Label: "Object 1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", Label: "Object 2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", Label: "Object 3", GoverseNodeID: "node2"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	// Remove obj2
	pg.RemoveObject("obj2")

	// Verify only obj2 was removed
	objects := pg.GetObjects()
	if len(objects) != 2 {
		t.Fatalf("Expected 2 objects after removal, got %d", len(objects))
	}

	objIDs := make(map[string]bool)
	for _, obj := range objects {
		objIDs[obj.ID] = true
	}

	if !objIDs["obj1"] {
		t.Fatal("obj1 should still exist")
	}

	if objIDs["obj2"] {
		t.Fatal("obj2 should have been removed")
	}

	if !objIDs["obj3"] {
		t.Fatal("obj3 should still exist")
	}
}

// TestRemoveNode tests removing a node
func TestRemoveNode(t *testing.T) {
	pg := NewGoverseGraph(0)

	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	pg.RemoveNode("node1")

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected 0 nodes after removal, got %d", len(nodes))
	}
}

// TestRemoveNode_NonExistent tests removing a non-existent node
func TestRemoveNode_NonExistent(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Should not panic when removing non-existent node
	pg.RemoveNode("non-existent-node")

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected 0 nodes, got %d", len(nodes))
	}
}

// TestRemoveNode_CascadeObjects tests that removing a node also removes its objects
func TestRemoveNode_CascadeObjects(t *testing.T) {
	pg := NewGoverseGraph(0)

	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	// Remove node1
	pg.RemoveNode("node1")

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object remaining (obj3), got %d", len(objects))
	}

	if len(objects) > 0 && objects[0].ID != "obj3" {
		t.Fatalf("Expected remaining object to be 'obj3', got '%s'", objects[0].ID)
	}

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Fatalf("Expected 0 nodes after removal, got %d", len(nodes))
	}
}

// TestRemoveStaleObjects tests removing stale objects
func TestRemoveStaleObjects(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Add objects for node1
	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	// Current objects only includes obj1
	currentObjs := []*inspector_pb.Object{
		{Id: "obj1"},
	}

	pg.RemoveStaleObjects("node1", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 2 {
		t.Fatalf("Expected 2 objects remaining (obj1 and obj3), got %d", len(objects))
	}

	// Verify obj2 was removed but obj1 and obj3 remain
	objIDs := make(map[string]bool)
	for _, obj := range objects {
		objIDs[obj.ID] = true
	}

	if !objIDs["obj1"] {
		t.Fatal("obj1 should still exist")
	}

	if objIDs["obj2"] {
		t.Fatal("obj2 should have been removed as stale")
	}

	if !objIDs["obj3"] {
		t.Fatal("obj3 should still exist (belongs to different node)")
	}
}

// TestRemoveStaleObjects_EmptyCurrentList tests removing all objects when current list is empty
func TestRemoveStaleObjects_EmptyCurrentList(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	// Empty current objects list
	currentObjs := []*inspector_pb.Object{}

	pg.RemoveStaleObjects("node1", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object remaining (obj3), got %d", len(objects))
	}

	if objects[0].ID != "obj3" {
		t.Fatalf("Expected remaining object to be 'obj3', got '%s'", objects[0].ID)
	}
}

// TestRemoveStaleObjects_NilObjects tests handling nil objects in current list
func TestRemoveStaleObjects_NilObjects(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	pg.AddOrUpdateObject(obj1)

	// Current list contains nil and empty ID objects
	currentObjs := []*inspector_pb.Object{
		nil,
		{Id: ""},
		{Id: "obj1"},
	}

	// Should not panic
	pg.RemoveStaleObjects("node1", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object, got %d", len(objects))
	}

	if objects[0].ID != "obj1" {
		t.Fatalf("Expected object 'obj1', got '%s'", objects[0].ID)
	}
}

// TestRemoveStaleObjects_NonExistentNode tests removing stale objects for non-existent node
func TestRemoveStaleObjects_NonExistentNode(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	pg.AddOrUpdateObject(obj1)

	currentObjs := []*inspector_pb.Object{
		{Id: "obj2"},
	}

	// Should not panic when node doesn't exist
	pg.RemoveStaleObjects("non-existent-node", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object (unchanged), got %d", len(objects))
	}
}

// TestConcurrentAccess tests thread safety with concurrent operations
func TestConcurrentAccess(t *testing.T) {
	pg := NewGoverseGraph(0)

	const goroutines = 5
	const operations = 10

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 types of operations

	// Concurrent AddObject operations
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				obj := models.GoverseObject{
					ID:            fmt.Sprintf("obj-%d-%d", id, j),
					GoverseNodeID: "node1",
				}
				pg.AddOrUpdateObject(obj)
			}
		}(i)
	}

	// Concurrent AddOrUpdateNode operations
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				node := models.GoverseNode{
					ID:    fmt.Sprintf("node-%d-%d", id, j),
					Label: "Node",
				}
				pg.AddOrUpdateNode(node)
			}
		}(i)
	}

	// Concurrent read operations
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				_ = pg.GetObjects()
				_ = pg.GetNodes()
			}
		}()
	}

	wg.Wait()

	// Should not panic and should have some data
	objects := pg.GetObjects()
	nodes := pg.GetNodes()

	if len(objects) == 0 {
		t.Fatal("Expected some objects after concurrent operations")
	}

	if len(nodes) == 0 {
		t.Fatal("Expected some nodes after concurrent operations")
	}
}

// TestConcurrentRemoveAndRead tests concurrent remove and read operations
func TestConcurrentRemoveAndRead(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Add initial data
	for i := 0; i < 10; i++ {
		node := models.GoverseNode{ID: fmt.Sprintf("node-%d", i)}
		pg.AddOrUpdateNode(node)

		obj := models.GoverseObject{ID: fmt.Sprintf("obj-%d", i), GoverseNodeID: fmt.Sprintf("node-%d", i)}
		pg.AddOrUpdateObject(obj)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	// Concurrent RemoveNode operations
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			pg.RemoveNode(fmt.Sprintf("node-%d", i))
		}
	}()

	// Concurrent RemoveStaleObjects operations
	go func() {
		defer wg.Done()
		for i := 5; i < 10; i++ {
			pg.RemoveStaleObjects(fmt.Sprintf("node-%d", i), []*inspector_pb.Object{})
		}
	}()

	// Concurrent read operations
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = pg.GetObjects()
			_ = pg.GetNodes()
		}
	}()

	wg.Wait()

	// Should not panic
	objects := pg.GetObjects()
	nodes := pg.GetNodes()

	// Some data should have been removed
	if len(objects) >= 10 {
		t.Fatal("Expected some objects to be removed")
	}

	if len(nodes) >= 10 {
		t.Fatal("Expected some nodes to be removed")
	}
}

// TestAddObject_MultipleNodes tests objects from different nodes
func TestAddObject_MultipleNodes(t *testing.T) {
	pg := NewGoverseGraph(0)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node2"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node1"}

	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)
	pg.AddOrUpdateObject(obj3)

	objects := pg.GetObjects()
	if len(objects) != 3 {
		t.Fatalf("Expected 3 objects, got %d", len(objects))
	}

	// Count objects per node
	nodeCount := make(map[string]int)
	for _, obj := range objects {
		nodeCount[obj.GoverseNodeID]++
	}

	if nodeCount["node1"] != 2 {
		t.Fatalf("Expected 2 objects for node1, got %d", nodeCount["node1"])
	}

	if nodeCount["node2"] != 1 {
		t.Fatalf("Expected 1 object for node2, got %d", nodeCount["node2"])
	}
}

// TestIsNodeRegistered tests checking if a node is registered
func TestIsNodeRegistered(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Test with empty graph
	if pg.IsNodeRegistered("node1") {
		t.Fatal("IsNodeRegistered() should return false for non-existent node in empty graph")
	}

	// Add a node
	node := models.GoverseNode{
		ID:            "localhost:47000",
		Label:         "Node 1",
		AdvertiseAddr: "localhost:47000",
	}
	pg.AddOrUpdateNode(node)

	// Test existing node
	if !pg.IsNodeRegistered("localhost:47000") {
		t.Fatal("IsNodeRegistered() should return true for registered node")
	}

	// Test non-existent node
	if pg.IsNodeRegistered("localhost:47001") {
		t.Fatal("IsNodeRegistered() should return false for non-registered node")
	}

	// Remove the node
	pg.RemoveNode("localhost:47000")

	// Test after removal
	if pg.IsNodeRegistered("localhost:47000") {
		t.Fatal("IsNodeRegistered() should return false after node removal")
	}
}

// MockObserver is a mock observer for testing
type MockObserver struct {
	mu     sync.Mutex
	events []GraphEvent
}

func (m *MockObserver) OnGraphEvent(event GraphEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
}

func (m *MockObserver) GetEvents() []GraphEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]GraphEvent, len(m.events))
	copy(result, m.events)
	return result
}

func (m *MockObserver) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = nil
}

// TestObserverPattern tests the observer pattern for graph events
func TestObserverPattern(t *testing.T) {
	pg := NewGoverseGraph(0)
	observer := &MockObserver{}

	pg.AddObserver(observer)

	// Test node added event
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	events := observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventNodeAdded {
		t.Fatalf("Expected EventNodeAdded, got %s", events[0].Type)
	}
	if events[0].Node == nil || events[0].Node.ID != "node1" {
		t.Fatal("Expected node data in event")
	}

	observer.Clear()

	// Test node updated event
	node.Label = "Updated Node 1"
	pg.AddOrUpdateNode(node)

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventNodeUpdated {
		t.Fatalf("Expected EventNodeUpdated, got %s", events[0].Type)
	}

	observer.Clear()

	// Test object added event
	obj := models.GoverseObject{ID: "obj1", Label: "Object 1"}
	pg.AddOrUpdateObject(obj)

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventObjectAdded {
		t.Fatalf("Expected EventObjectAdded, got %s", events[0].Type)
	}

	observer.Clear()

	// Test object updated event
	obj.Label = "Updated Object 1"
	pg.AddOrUpdateObject(obj)

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventObjectUpdated {
		t.Fatalf("Expected EventObjectUpdated, got %s", events[0].Type)
	}

	observer.Clear()

	// Test object removed event
	pg.RemoveObject("obj1")

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventObjectRemoved {
		t.Fatalf("Expected EventObjectRemoved, got %s", events[0].Type)
	}
	if events[0].ObjectID != "obj1" {
		t.Fatalf("Expected ObjectID 'obj1', got '%s'", events[0].ObjectID)
	}

	observer.Clear()

	// Test node removed event
	pg.RemoveNode("node1")

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventNodeRemoved {
		t.Fatalf("Expected EventNodeRemoved, got %s", events[0].Type)
	}
	if events[0].NodeID != "node1" {
		t.Fatalf("Expected NodeID 'node1', got '%s'", events[0].NodeID)
	}
}

// TestRemoveObserver tests removing an observer
func TestRemoveObserver(t *testing.T) {
	pg := NewGoverseGraph(0)
	observer := &MockObserver{}

	pg.AddObserver(observer)

	// Add a node, should trigger event
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	events := observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event before removing observer, got %d", len(events))
	}

	observer.Clear()

	// Remove observer
	pg.RemoveObserver(observer)

	// Add another node, should not trigger event for removed observer
	node2 := models.GoverseNode{ID: "node2", Label: "Node 2"}
	pg.AddOrUpdateNode(node2)

	events = observer.GetEvents()
	if len(events) != 0 {
		t.Fatalf("Expected 0 events after removing observer, got %d", len(events))
	}
}

// TestMultipleObservers tests multiple observers receiving events
func TestMultipleObservers(t *testing.T) {
	pg := NewGoverseGraph(0)
	observer1 := &MockObserver{}
	observer2 := &MockObserver{}

	pg.AddObserver(observer1)
	pg.AddObserver(observer2)

	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	events1 := observer1.GetEvents()
	events2 := observer2.GetEvents()

	if len(events1) != 1 {
		t.Fatalf("Observer 1: Expected 1 event, got %d", len(events1))
	}
	if len(events2) != 1 {
		t.Fatalf("Observer 2: Expected 1 event, got %d", len(events2))
	}
}

// TestRemoveNodeCascadeEvents tests that removing a node triggers events for removed objects
func TestRemoveNodeCascadeEvents(t *testing.T) {
	pg := NewGoverseGraph(0)
	observer := &MockObserver{}
	pg.AddObserver(observer)

	// Add a node and objects
	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	pg.AddOrUpdateObject(obj1)
	pg.AddOrUpdateObject(obj2)

	observer.Clear()

	// Remove the node
	pg.RemoveNode("node1")

	events := observer.GetEvents()
	// Should have 2 object_removed events + 1 node_removed event
	if len(events) != 3 {
		t.Fatalf("Expected 3 events (2 object_removed + 1 node_removed), got %d", len(events))
	}

	objectRemovedCount := 0
	nodeRemovedCount := 0
	for _, e := range events {
		if e.Type == EventObjectRemoved {
			objectRemovedCount++
		}
		if e.Type == EventNodeRemoved {
			nodeRemovedCount++
		}
	}

	if objectRemovedCount != 2 {
		t.Fatalf("Expected 2 EventObjectRemoved events, got %d", objectRemovedCount)
	}
	if nodeRemovedCount != 1 {
		t.Fatalf("Expected 1 EventNodeRemoved event, got %d", nodeRemovedCount)
	}
}

// TestGetGates tests retrieving all gates
func TestGetGates(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Test with empty graph
	gates := pg.GetGates()
	if gates == nil {
		t.Fatal("GetGates() should return non-nil slice")
	}
	if len(gates) != 0 {
		t.Fatalf("GetGates() should return empty slice for empty graph, got %d items", len(gates))
	}

	// Add a gate
	gate := models.GoverseGate{
		ID:            "localhost:49000",
		Label:         "Gate 1",
		AdvertiseAddr: "localhost:49000",
	}
	pg.AddOrUpdateGate(gate)

	gates = pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	if gates[0].ID != "localhost:49000" {
		t.Fatalf("Expected gate ID 'localhost:49000', got '%s'", gates[0].ID)
	}
}

// TestAddOrUpdateGate tests adding and updating gates
func TestAddOrUpdateGate(t *testing.T) {
	pg := NewGoverseGraph(0)

	gate := models.GoverseGate{
		ID:    "localhost:49000",
		Label: "Gate 1",
	}
	pg.AddOrUpdateGate(gate)

	gates := pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate, got %d", len(gates))
	}

	// Update the gate
	gate.Label = "Updated Gate 1"
	pg.AddOrUpdateGate(gate)

	gates = pg.GetGates()
	if len(gates) != 1 {
		t.Fatalf("Expected 1 gate after update, got %d", len(gates))
	}

	if gates[0].Label != "Updated Gate 1" {
		t.Fatalf("Expected updated label, got '%s'", gates[0].Label)
	}
}

// TestRemoveGate tests removing a gate
func TestRemoveGate(t *testing.T) {
	pg := NewGoverseGraph(0)

	gate := models.GoverseGate{ID: "localhost:49000", Label: "Gate 1"}
	pg.AddOrUpdateGate(gate)

	pg.RemoveGate("localhost:49000")

	gates := pg.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates after removal, got %d", len(gates))
	}
}

// TestRemoveGate_NonExistent tests removing a non-existent gate
func TestRemoveGate_NonExistent(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Should not panic when removing non-existent gate
	pg.RemoveGate("non-existent-gate")

	gates := pg.GetGates()
	if len(gates) != 0 {
		t.Fatalf("Expected 0 gates, got %d", len(gates))
	}
}

// TestIsGateRegistered tests checking if a gate is registered
func TestIsGateRegistered(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Test with empty graph
	if pg.IsGateRegistered("localhost:49000") {
		t.Fatal("IsGateRegistered() should return false for non-existent gate in empty graph")
	}

	// Add a gate
	gate := models.GoverseGate{
		ID:            "localhost:49000",
		Label:         "Gate 1",
		AdvertiseAddr: "localhost:49000",
	}
	pg.AddOrUpdateGate(gate)

	// Test existing gate
	if !pg.IsGateRegistered("localhost:49000") {
		t.Fatal("IsGateRegistered() should return true for registered gate")
	}

	// Test non-existent gate
	if pg.IsGateRegistered("localhost:49001") {
		t.Fatal("IsGateRegistered() should return false for non-registered gate")
	}

	// Remove the gate
	pg.RemoveGate("localhost:49000")

	// Test after removal
	if pg.IsGateRegistered("localhost:49000") {
		t.Fatal("IsGateRegistered() should return false after gate removal")
	}
}

// TestGateObserverEvents tests observer events for gate operations
func TestGateObserverEvents(t *testing.T) {
	pg := NewGoverseGraph(0)
	observer := &MockObserver{}

	pg.AddObserver(observer)

	// Test gate added event
	gate := models.GoverseGate{ID: "gate1", Label: "Gate 1"}
	pg.AddOrUpdateGate(gate)

	events := observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventGateAdded {
		t.Fatalf("Expected EventGateAdded, got %s", events[0].Type)
	}
	if events[0].Gate == nil || events[0].Gate.ID != "gate1" {
		t.Fatal("Expected gate data in event")
	}

	observer.Clear()

	// Test gate updated event
	gate.Label = "Updated Gate 1"
	pg.AddOrUpdateGate(gate)

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventGateUpdated {
		t.Fatalf("Expected EventGateUpdated, got %s", events[0].Type)
	}

	observer.Clear()

	// Test gate removed event
	pg.RemoveGate("gate1")

	events = observer.GetEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventGateRemoved {
		t.Fatalf("Expected EventGateRemoved, got %s", events[0].Type)
	}
	if events[0].GateID != "gate1" {
		t.Fatalf("Expected GateID 'gate1', got '%s'", events[0].GateID)
	}
}

// TestMultipleGatesGraph tests adding multiple gates to graph
func TestMultipleGatesGraph(t *testing.T) {
	pg := NewGoverseGraph(0)

	// Add multiple gates
	gate1 := models.GoverseGate{ID: "localhost:49000", Label: "Gate 1"}
	gate2 := models.GoverseGate{ID: "localhost:49001", Label: "Gate 2"}
	gate3 := models.GoverseGate{ID: "localhost:49002", Label: "Gate 3"}

	pg.AddOrUpdateGate(gate1)
	pg.AddOrUpdateGate(gate2)
	pg.AddOrUpdateGate(gate3)

	gates := pg.GetGates()
	if len(gates) != 3 {
		t.Fatalf("Expected 3 gates, got %d", len(gates))
	}

	// Remove one gate
	pg.RemoveGate("localhost:49001")

	gates = pg.GetGates()
	if len(gates) != 2 {
		t.Fatalf("Expected 2 gates after removal, got %d", len(gates))
	}

	// Verify correct gates remain
	gateIDs := make(map[string]bool)
	for _, g := range gates {
		gateIDs[g.ID] = true
	}

	if !gateIDs["localhost:49000"] {
		t.Fatal("Gate localhost:49000 should still exist")
	}
	if gateIDs["localhost:49001"] {
		t.Fatal("Gate localhost:49001 should have been removed")
	}
	if !gateIDs["localhost:49002"] {
		t.Fatal("Gate localhost:49002 should still exist")
	}
}
