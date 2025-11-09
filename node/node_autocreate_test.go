package node

import (
	"context"
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// AutoPersistentObject is a test object that supports persistence and has a Do method
type AutoPersistentObject struct {
	object.BaseObject
	mu    sync.Mutex
	Value string
}

func (a *AutoPersistentObject) OnCreated() {}

func (a *AutoPersistentObject) Do(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return a response with the object's current value
	resp, err := structpb.NewStruct(map[string]interface{}{
		"id":    a.Id(),
		"value": a.Value,
		"echo":  req.Fields["input"].GetStringValue(),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (a *AutoPersistentObject) ToData() (proto.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := structpb.NewStruct(map[string]interface{}{
		"id":    a.Id(),
		"value": a.Value,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (a *AutoPersistentObject) FromData(data proto.Message) error {
	if data == nil {
		return nil
	}

	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if value, ok := structData.Fields["value"]; ok {
		a.Value = value.GetStringValue()
	}
	return nil
}

// AutoNonPersistentObject is a test object that does not support persistence but has a Do method
type AutoNonPersistentObject struct {
	object.BaseObject
	mu    sync.Mutex
	Value string
}

func (a *AutoNonPersistentObject) OnCreated() {}

func (a *AutoNonPersistentObject) Do(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return a response
	resp, err := structpb.NewStruct(map[string]interface{}{
		"id":    a.Id(),
		"value": a.Value,
		"echo":  req.Fields["input"].GetStringValue(),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (a *AutoNonPersistentObject) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (a *AutoNonPersistentObject) FromData(data proto.Message) error {
	// Non-persistent object, just ignore data
	return nil
}

// TestCallObject_AutoCreatePersistentObject tests that CallObject auto-creates a persistent object
// and loads it from the DB when available
func TestCallObject_AutoCreatePersistentObject(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register the persistent object type
	node.RegisterObjectType((*AutoPersistentObject)(nil))

	ctx := context.Background()

	// Pre-save data for the object to simulate it existing in DB
	objectID := "persistent-obj-123"
	savedData, err := structpb.NewStruct(map[string]interface{}{
		"id":    objectID,
		"value": "loaded-from-db",
	})
	if err != nil {
		t.Fatalf("Failed to create saved data: %v", err)
	}
	err = object.SaveObject(ctx, provider, objectID, "AutoPersistentObject", savedData)
	if err != nil {
		t.Fatalf("Failed to pre-save object: %v", err)
	}

	// Verify object doesn't exist yet
	node.objectsMu.RLock()
	_, exists := node.objects[objectID]
	node.objectsMu.RUnlock()
	if exists {
		t.Fatal("Object should not exist before CallObject")
	}

	// Call method on non-existent object - should auto-create and load from DB
	request, err := structpb.NewStruct(map[string]interface{}{
		"input": "test-input",
	})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := node.CallObject(ctx, objectID, "Do", request)
	if err != nil {
		t.Fatalf("CallObject failed: %v", err)
	}

	// Verify response
	respStruct, ok := resp.(*structpb.Struct)
	if !ok {
		t.Fatalf("Expected *structpb.Struct response, got %T", resp)
	}

	// Verify the object was loaded from DB (value should be "loaded-from-db")
	if respStruct.Fields["value"].GetStringValue() != "loaded-from-db" {
		t.Errorf("Expected value 'loaded-from-db', got '%s'", respStruct.Fields["value"].GetStringValue())
	}

	// Verify echo from request
	if respStruct.Fields["echo"].GetStringValue() != "test-input" {
		t.Errorf("Expected echo 'test-input', got '%s'", respStruct.Fields["echo"].GetStringValue())
	}

	// Verify object now exists in node
	node.objectsMu.RLock()
	obj, exists := node.objects[objectID]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist after auto-creation")
	}
	if obj.Id() != objectID {
		t.Errorf("Expected object ID '%s', got '%s'", objectID, obj.Id())
	}
}

// TestCallObject_AutoCreateNonPersistentObject tests that CallObject auto-creates a non-persistent object
func TestCallObject_AutoCreateNonPersistentObject(t *testing.T) {
	node := NewNode("localhost:47000")

	// Register the non-persistent object type
	node.RegisterObjectType((*AutoNonPersistentObject)(nil))

	ctx := context.Background()
	objectID := "non-persistent-obj-456"

	// Verify object doesn't exist yet
	node.objectsMu.RLock()
	_, exists := node.objects[objectID]
	node.objectsMu.RUnlock()
	if exists {
		t.Fatal("Object should not exist before CallObject")
	}

	// Call method on non-existent object - should auto-create
	request, err := structpb.NewStruct(map[string]interface{}{
		"input": "another-test",
	})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := node.CallObject(ctx, objectID, "Do", request)
	if err != nil {
		t.Fatalf("CallObject failed: %v", err)
	}

	// Verify response
	respStruct, ok := resp.(*structpb.Struct)
	if !ok {
		t.Fatalf("Expected *structpb.Struct response, got %T", resp)
	}

	// Verify echo from request
	if respStruct.Fields["echo"].GetStringValue() != "another-test" {
		t.Errorf("Expected echo 'another-test', got '%s'", respStruct.Fields["echo"].GetStringValue())
	}

	// Verify object now exists in node
	node.objectsMu.RLock()
	obj, exists := node.objects[objectID]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist after auto-creation")
	}
	if obj.Id() != objectID {
		t.Errorf("Expected object ID '%s', got '%s'", objectID, obj.Id())
	}
}

// TestCallObject_NoMatchingType tests that CallObject returns error when no matching type is found
func TestCallObject_NoMatchingType(t *testing.T) {
	node := NewNode("localhost:47000")

	// Register a type that doesn't have the method we're calling
	node.RegisterObjectType((*TestObject)(nil))

	ctx := context.Background()
	objectID := "no-match-obj"

	// Try to call a method that doesn't exist on any registered type
	request, err := structpb.NewStruct(map[string]interface{}{
		"input": "test",
	})
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	_, err = node.CallObject(ctx, objectID, "Do", request)
	if err == nil {
		t.Fatal("Expected error when no matching type found, got nil")
	}

	// Should get "object not found" error
	if err.Error() != "object not found: no-match-obj" {
		t.Errorf("Expected 'object not found' error, got '%s'", err.Error())
	}

	// Verify object was not created
	node.objectsMu.RLock()
	_, exists := node.objects[objectID]
	node.objectsMu.RUnlock()
	if exists {
		t.Fatal("Object should not exist when no matching type found")
	}
}
