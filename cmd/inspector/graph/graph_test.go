package graph

import (
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/cmd/inspector/models"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

// TestNewGoverseGraph tests the constructor
func TestNewGoverseGraph(t *testing.T) {
	pg := NewGoverseGraph()

	if pg == nil {
		t.Fatal("NewGoverseGraph() returned nil")
	}

	if pg.objects == nil {
		t.Error("objects map should be initialized")
	}

	if pg.nodes == nil {
		t.Error("nodes map should be initialized")
	}

	if len(pg.objects) != 0 {
		t.Errorf("objects map should be empty, got %d items", len(pg.objects))
	}

	if len(pg.nodes) != 0 {
		t.Errorf("nodes map should be empty, got %d items", len(pg.nodes))
	}
}

// TestGetNodes tests retrieving all nodes
func TestGetNodes(t *testing.T) {
	pg := NewGoverseGraph()

	// Test with empty graph
	nodes := pg.GetNodes()
	if nodes == nil {
		t.Error("GetNodes() should return non-nil slice")
	}
	if len(nodes) != 0 {
		t.Errorf("GetNodes() should return empty slice for empty graph, got %d items", len(nodes))
	}

	// Add some nodes
	node1 := models.GoverseNode{ID: "node1", Label: "Node 1"}
	node2 := models.GoverseNode{ID: "node2", Label: "Node 2"}
	pg.AddOrUpdateNode(node1)
	pg.AddOrUpdateNode(node2)

	nodes = pg.GetNodes()
	if len(nodes) != 2 {
		t.Errorf("GetNodes() should return 2 nodes, got %d", len(nodes))
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
		t.Error("GetNodes() should return a copy, original data was modified")
	}
}

// TestGetObjects tests retrieving all objects
func TestGetObjects(t *testing.T) {
	pg := NewGoverseGraph()

	// Test with empty graph
	objects := pg.GetObjects()
	if objects == nil {
		t.Error("GetObjects() should return non-nil slice")
	}
	if len(objects) != 0 {
		t.Errorf("GetObjects() should return empty slice for empty graph, got %d items", len(objects))
	}

	// Add some objects
	obj1 := models.GoverseObject{ID: "obj1", Label: "Object 1"}
	obj2 := models.GoverseObject{ID: "obj2", Label: "Object 2"}
	pg.AddObject(obj1)
	pg.AddObject(obj2)

	objects = pg.GetObjects()
	if len(objects) != 2 {
		t.Errorf("GetObjects() should return 2 objects, got %d", len(objects))
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
		t.Error("GetObjects() should return a copy, original data was modified")
	}
}

// TestAddObject tests adding objects
func TestAddObject(t *testing.T) {
	pg := NewGoverseGraph()

	obj := models.GoverseObject{
		ID:            "test-obj-1",
		Label:         "Test Object 1",
		GoverseNodeID: "node1",
	}

	pg.AddObject(obj)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object, got %d", len(objects))
	}

	if objects[0].ID != "test-obj-1" {
		t.Errorf("Expected object ID 'test-obj-1', got '%s'", objects[0].ID)
	}

	if objects[0].Label != "Test Object 1" {
		t.Errorf("Expected object label 'Test Object 1', got '%s'", objects[0].Label)
	}

	if objects[0].GoverseNodeID != "node1" {
		t.Errorf("Expected GoverseNodeID 'node1', got '%s'", objects[0].GoverseNodeID)
	}
}

// TestAddObject_Duplicate tests that duplicate objects are not added
func TestAddObject_Duplicate(t *testing.T) {
	pg := NewGoverseGraph()

	obj1 := models.GoverseObject{
		ID:    "test-obj-1",
		Label: "First Version",
	}

	obj2 := models.GoverseObject{
		ID:    "test-obj-1",
		Label: "Second Version",
	}

	pg.AddObject(obj1)
	pg.AddObject(obj2) // Should not replace the first one

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object (duplicates should not be added), got %d", len(objects))
	}

	// Verify the original object is preserved
	if objects[0].Label != "First Version" {
		t.Errorf("Expected label 'First Version', got '%s' - duplicate should not replace existing", objects[0].Label)
	}
}

// TestAddOrUpdateNode tests adding and updating nodes
func TestAddOrUpdateNode(t *testing.T) {
	pg := NewGoverseGraph()

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
		t.Errorf("Expected node ID 'node1', got '%s'", nodes[0].ID)
	}

	if nodes[0].Label != "Node 1" {
		t.Errorf("Expected node label 'Node 1', got '%s'", nodes[0].Label)
	}
}

// TestAddOrUpdateNode_Update tests that existing nodes are updated
func TestAddOrUpdateNode_Update(t *testing.T) {
	pg := NewGoverseGraph()

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
		t.Errorf("Expected label 'Updated Label', got '%s'", nodes[0].Label)
	}

	if nodes[0].AdvertiseAddr != "localhost:47001" {
		t.Errorf("Expected address 'localhost:47001', got '%s'", nodes[0].AdvertiseAddr)
	}
}

// TestRemoveNode tests removing a node
func TestRemoveNode(t *testing.T) {
	pg := NewGoverseGraph()

	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	pg.RemoveNode("node1")

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes after removal, got %d", len(nodes))
	}
}

// TestRemoveNode_NonExistent tests removing a non-existent node
func TestRemoveNode_NonExistent(t *testing.T) {
	pg := NewGoverseGraph()

	// Should not panic when removing non-existent node
	pg.RemoveNode("non-existent-node")

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(nodes))
	}
}

// TestRemoveNode_CascadeObjects tests that removing a node also removes its objects
func TestRemoveNode_CascadeObjects(t *testing.T) {
	pg := NewGoverseGraph()

	node := models.GoverseNode{ID: "node1", Label: "Node 1"}
	pg.AddOrUpdateNode(node)

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddObject(obj1)
	pg.AddObject(obj2)
	pg.AddObject(obj3)

	// Remove node1
	pg.RemoveNode("node1")

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Errorf("Expected 1 object remaining (obj3), got %d", len(objects))
	}

	if len(objects) > 0 && objects[0].ID != "obj3" {
		t.Errorf("Expected remaining object to be 'obj3', got '%s'", objects[0].ID)
	}

	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes after removal, got %d", len(nodes))
	}
}

// TestRemoveStaleObjects tests removing stale objects
func TestRemoveStaleObjects(t *testing.T) {
	pg := NewGoverseGraph()

	// Add objects for node1
	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddObject(obj1)
	pg.AddObject(obj2)
	pg.AddObject(obj3)

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
		t.Error("obj1 should still exist")
	}

	if objIDs["obj2"] {
		t.Error("obj2 should have been removed as stale")
	}

	if !objIDs["obj3"] {
		t.Error("obj3 should still exist (belongs to different node)")
	}
}

// TestRemoveStaleObjects_EmptyCurrentList tests removing all objects when current list is empty
func TestRemoveStaleObjects_EmptyCurrentList(t *testing.T) {
	pg := NewGoverseGraph()

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node1"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node2"}

	pg.AddObject(obj1)
	pg.AddObject(obj2)
	pg.AddObject(obj3)

	// Empty current objects list
	currentObjs := []*inspector_pb.Object{}

	pg.RemoveStaleObjects("node1", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Fatalf("Expected 1 object remaining (obj3), got %d", len(objects))
	}

	if objects[0].ID != "obj3" {
		t.Errorf("Expected remaining object to be 'obj3', got '%s'", objects[0].ID)
	}
}

// TestRemoveStaleObjects_NilObjects tests handling nil objects in current list
func TestRemoveStaleObjects_NilObjects(t *testing.T) {
	pg := NewGoverseGraph()

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	pg.AddObject(obj1)

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
		t.Errorf("Expected object 'obj1', got '%s'", objects[0].ID)
	}
}

// TestRemoveStaleObjects_NonExistentNode tests removing stale objects for non-existent node
func TestRemoveStaleObjects_NonExistentNode(t *testing.T) {
	pg := NewGoverseGraph()

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	pg.AddObject(obj1)

	currentObjs := []*inspector_pb.Object{
		{Id: "obj2"},
	}

	// Should not panic when node doesn't exist
	pg.RemoveStaleObjects("non-existent-node", currentObjs)

	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Errorf("Expected 1 object (unchanged), got %d", len(objects))
	}
}

// TestConcurrentAccess tests thread safety with concurrent operations
func TestConcurrentAccess(t *testing.T) {
	pg := NewGoverseGraph()

	const goroutines = 10
	const operations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // 3 types of operations

	// Concurrent AddObject operations
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				obj := models.GoverseObject{
					ID:            string(rune('a'+id)) + string(rune('0'+j%10)),
					GoverseNodeID: "node1",
				}
				pg.AddObject(obj)
			}
		}(i)
	}

	// Concurrent AddOrUpdateNode operations
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				node := models.GoverseNode{
					ID:    string(rune('n'+id)) + string(rune('0'+j%10)),
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
		t.Error("Expected some objects after concurrent operations")
	}

	if len(nodes) == 0 {
		t.Error("Expected some nodes after concurrent operations")
	}
}

// TestConcurrentRemoveAndRead tests concurrent remove and read operations
func TestConcurrentRemoveAndRead(t *testing.T) {
	pg := NewGoverseGraph()

	// Add initial data
	for i := 0; i < 10; i++ {
		node := models.GoverseNode{ID: string(rune('n' + i))}
		pg.AddOrUpdateNode(node)

		obj := models.GoverseObject{ID: string(rune('o' + i)), GoverseNodeID: string(rune('n' + i))}
		pg.AddObject(obj)
	}

	var wg sync.WaitGroup
	wg.Add(3)

	// Concurrent RemoveNode operations
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			pg.RemoveNode(string(rune('n' + i)))
		}
	}()

	// Concurrent RemoveStaleObjects operations
	go func() {
		defer wg.Done()
		for i := 5; i < 10; i++ {
			pg.RemoveStaleObjects(string(rune('n'+i)), []*inspector_pb.Object{})
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
		t.Error("Expected some objects to be removed")
	}

	if len(nodes) >= 10 {
		t.Error("Expected some nodes to be removed")
	}
}

// TestAddObject_MultipleNodes tests objects from different nodes
func TestAddObject_MultipleNodes(t *testing.T) {
	pg := NewGoverseGraph()

	obj1 := models.GoverseObject{ID: "obj1", GoverseNodeID: "node1"}
	obj2 := models.GoverseObject{ID: "obj2", GoverseNodeID: "node2"}
	obj3 := models.GoverseObject{ID: "obj3", GoverseNodeID: "node1"}

	pg.AddObject(obj1)
	pg.AddObject(obj2)
	pg.AddObject(obj3)

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
		t.Errorf("Expected 2 objects for node1, got %d", nodeCount["node1"])
	}

	if nodeCount["node2"] != 1 {
		t.Errorf("Expected 1 object for node2, got %d", nodeCount["node2"])
	}
}
