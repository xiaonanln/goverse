package node

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// PersistentTestObject is a test object that supports persistence
type PersistentTestObject struct {
	TestObject
}

// Override ToData to make it persistent
func (t *PersistentTestObject) ToData() (proto.Message, error) {
	data, err := structpb.NewStruct(map[string]interface{}{
		"id":   t.Id(),
		"type": t.Type(),
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Override FromData to restore state
func (t *PersistentTestObject) FromData(data proto.Message) error {
	// No fields to restore for this simple test object
	return nil
}

func TestNode_NextRcid_DefaultValue(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)

	// Register object type
	node.RegisterObjectType((*PersistentTestObject)(nil))

	// Create object
	_, err := node.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get object
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object was not created")
	}

	// Verify default next_rcid is 0
	if obj.GetNextRcid() != 0 {
		t.Fatalf("GetNextRcid() = %d; want 0", obj.GetNextRcid())
	}
}

func TestNode_NextRcid_PersistenceNewObject(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*PersistentTestObject)(nil))

	// Create object
	_, err := node.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get object and set next_rcid
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object was not created")
	}

	obj.SetNextRcid(42)

	// Save all objects
	if err := node.SaveAllObjects(ctx); err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Verify next_rcid was saved
	savedNextRcid, ok := provider.nextRcids["test-obj-1"]
	if !ok {
		t.Fatal("next_rcid was not saved")
	}
	if savedNextRcid != 42 {
		t.Fatalf("Saved next_rcid = %d; want 42", savedNextRcid)
	}
}

func TestNode_NextRcid_LoadFromPersistence(t *testing.T) {
	ctx := context.Background()
	provider := NewMockPersistenceProvider()

	// Pre-populate storage with an object
	// Create a proper test object and serialize it
	testObj := &PersistentTestObject{}
	testObj.OnInit(testObj, "test-obj-1")
	objData, err := testObj.ToData()
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}

	// Save using the SaveObject helper which creates proper JSON
	err = object.SaveObject(ctx, provider, "test-obj-1", "PersistentTestObject", objData, 123)
	if err != nil {
		t.Fatalf("Failed to save test data: %v", err)
	}

	// Create node and set persistence provider
	node := NewNode("localhost:47000", testNumShards)
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*PersistentTestObject)(nil))

	// Create object (should load from persistence)
	_, err = node.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get object
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object was not created")
	}

	// Verify next_rcid was loaded
	if obj.GetNextRcid() != 123 {
		t.Fatalf("GetNextRcid() = %d; want 123", obj.GetNextRcid())
	}
}

func TestNode_NextRcid_UpdateAndReload(t *testing.T) {
	ctx := context.Background()
	provider := NewMockPersistenceProvider()

	// Create and save object with initial next_rcid
	node1 := NewNode("localhost:47000", testNumShards)
	node1.SetPersistenceProvider(provider)

	node1.RegisterObjectType((*PersistentTestObject)(nil))

	_, err := node1.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node1.objectsMu.RLock()
	obj1, _ := node1.objects["test-obj-1"]
	node1.objectsMu.RUnlock()

	obj1.SetNextRcid(100)

	if err := node1.SaveAllObjects(ctx); err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Create a new node and load the object
	node2 := NewNode("localhost:47001", testNumShards)
	node2.SetPersistenceProvider(provider)

	node2.RegisterObjectType((*PersistentTestObject)(nil))

	_, err = node2.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node2.objectsMu.RLock()
	obj2, _ := node2.objects["test-obj-1"]
	node2.objectsMu.RUnlock()

	// Verify next_rcid was restored
	if obj2.GetNextRcid() != 100 {
		t.Fatalf("GetNextRcid() = %d; want 100", obj2.GetNextRcid())
	}

	// Update next_rcid on node2 and save
	obj2.SetNextRcid(200)

	if err := node2.SaveAllObjects(ctx); err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Create a third node and verify the update was persisted
	node3 := NewNode("localhost:47002", testNumShards)
	node3.SetPersistenceProvider(provider)

	node3.RegisterObjectType((*PersistentTestObject)(nil))

	_, err = node3.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node3.objectsMu.RLock()
	obj3, _ := node3.objects["test-obj-1"]
	node3.objectsMu.RUnlock()

	if obj3.GetNextRcid() != 200 {
		t.Fatalf("GetNextRcid() = %d; want 200", obj3.GetNextRcid())
	}
}

func TestNode_NextRcid_PeriodicPersistence(t *testing.T) {
	ctx := context.Background()
	provider := NewMockPersistenceProvider()

	node := NewNode("localhost:47000", testNumShards)
	node.SetPersistenceProvider(provider)
	node.SetPersistenceInterval(100 * time.Millisecond)

	node.RegisterObjectType((*PersistentTestObject)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create object
	_, err := node.CreateObject(ctx, "PersistentTestObject", "test-obj-1")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Get object and set next_rcid
	node.objectsMu.RLock()
	obj, _ := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	obj.SetNextRcid(50)

	// Wait for periodic persistence to run
	time.Sleep(300 * time.Millisecond)

	// Verify next_rcid was saved by periodic persistence
	savedNextRcid, ok := provider.nextRcids["test-obj-1"]
	if !ok {
		t.Fatal("next_rcid was not saved by periodic persistence")
	}
	if savedNextRcid != 50 {
		t.Fatalf("Saved next_rcid = %d; want 50", savedNextRcid)
	}

	// Update next_rcid
	obj.SetNextRcid(75)

	// Wait for another periodic persistence cycle
	time.Sleep(300 * time.Millisecond)

	// Verify updated next_rcid was saved
	savedNextRcid, ok = provider.nextRcids["test-obj-1"]
	if !ok {
		t.Fatal("next_rcid was not saved by periodic persistence")
	}
	if savedNextRcid != 75 {
		t.Fatalf("Saved next_rcid = %d; want 75", savedNextRcid)
	}
}
