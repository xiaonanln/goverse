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

func TestNode_NextRcseq_DefaultValue(t *testing.T) {
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

	// Verify default next_rcseq is 0
	if obj.GetNextRcseq() != 0 {
		t.Fatalf("GetNextRcseq() = %d; want 0", obj.GetNextRcseq())
	}
}

func TestNode_NextRcseq_PersistenceNewObject(t *testing.T) {
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

	// Get object and set next_rcseq
	node.objectsMu.RLock()
	obj, exists := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	if !exists {
		t.Fatal("Object was not created")
	}

	obj.SetNextRcseq(42)

	// Save all objects
	if err := node.SaveAllObjects(ctx); err != nil {
		t.Fatalf("Failed to save objects: %v", err)
	}

	// Verify next_rcseq was saved
	savedNextRcseq, ok := provider.GetStoredNextRcseq("test-obj-1")
	if !ok {
		t.Fatal("next_rcseq was not saved")
	}
	if savedNextRcseq != 42 {
		t.Fatalf("Saved next_rcseq = %d; want 42", savedNextRcseq)
	}
}

func TestNode_NextRcseq_LoadFromPersistence(t *testing.T) {
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

	// Verify next_rcseq was loaded
	if obj.GetNextRcseq() != 123 {
		t.Fatalf("GetNextRcseq() = %d; want 123", obj.GetNextRcseq())
	}
}

func TestNode_NextRcseq_UpdateAndReload(t *testing.T) {
	ctx := context.Background()
	provider := NewMockPersistenceProvider()

	// Create and save object with initial next_rcseq
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

	obj1.SetNextRcseq(100)

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

	// Verify next_rcseq was restored
	if obj2.GetNextRcseq() != 100 {
		t.Fatalf("GetNextRcseq() = %d; want 100", obj2.GetNextRcseq())
	}

	// Update next_rcseq on node2 and save
	obj2.SetNextRcseq(200)

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

	if obj3.GetNextRcseq() != 200 {
		t.Fatalf("GetNextRcseq() = %d; want 200", obj3.GetNextRcseq())
	}
}

func TestNode_NextRcseq_PeriodicPersistence(t *testing.T) {
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

	// Get object and set next_rcseq
	node.objectsMu.RLock()
	obj, _ := node.objects["test-obj-1"]
	node.objectsMu.RUnlock()

	obj.SetNextRcseq(50)

	// Wait for periodic persistence to run
	time.Sleep(300 * time.Millisecond)

	// Verify next_rcseq was saved by periodic persistence
	savedNextRcseq, ok := provider.GetStoredNextRcseq("test-obj-1")
	if !ok {
		t.Fatal("next_rcseq was not saved by periodic persistence")
	}
	if savedNextRcseq != 50 {
		t.Fatalf("Saved next_rcseq = %d; want 50", savedNextRcseq)
	}

	// Update next_rcseq
	obj.SetNextRcseq(75)

	// Wait for another periodic persistence cycle
	time.Sleep(300 * time.Millisecond)

	// Verify updated next_rcseq was saved
	savedNextRcseq, ok = provider.GetStoredNextRcseq("test-obj-1")
	if !ok {
		t.Fatal("next_rcseq was not saved by periodic persistence")
	}
	if savedNextRcseq != 75 {
		t.Fatalf("Saved next_rcseq = %d; want 75", savedNextRcseq)
	}
}
