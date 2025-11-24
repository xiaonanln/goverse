package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestObjectWithMethod is a test object with a callable method
type TestObjectWithMethod struct {
	object.BaseObject
	mu          sync.Mutex
	CallCount   int
	LastMessage string
}

func (t *TestObjectWithMethod) OnCreated() {}

// TestMethod is a simple method that can be called
func (t *TestObjectWithMethod) TestMethod(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.CallCount++
	return req, nil
}

// TestCallObject_AutoCreateNonPersistent tests that CallObject automatically creates non-persistent objects
func TestCallObject_AutoCreateNonPersistent(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestObjectWithMethod)(nil))

	// Verify object doesn't exist yet
	node.objectsMu.RLock()
	_, exists := node.objects["auto-create-1"]
	node.objectsMu.RUnlock()
	if exists {
		t.Fatal("Object should not exist before CallObject")
	}

	// Call method on non-existent object - should auto-create and call
	resp, err := node.CallObject(ctx, "TestObjectWithMethod", "auto-create-1", "TestMethod", &emptypb.Empty{}, 0)
	if err != nil {
		t.Fatalf("CallObject should succeed and auto-create object, got error: %v", err)
	}

	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	// Verify object was created
	node.objectsMu.RLock()
	obj, exists := node.objects["auto-create-1"]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist after CallObject")
	}

	// Verify it's the right type
	if obj.Type() != "TestObjectWithMethod" {
		t.Fatalf("Object type should be TestObjectWithMethod, got %s", obj.Type())
	}

	// Verify method was called
	testObj := obj.(*TestObjectWithMethod)
	testObj.mu.Lock()
	callCount := testObj.CallCount
	testObj.mu.Unlock()
	if callCount != 1 {
		t.Fatalf("Method should have been called once, got %d", callCount)
	}
}

// TestPersistentObjectWithMethod is a persistent test object with methods
type TestPersistentObjectWithMethod struct {
	object.BaseObject
	mu    sync.Mutex
	Value string
}

func (t *TestPersistentObjectWithMethod) OnCreated() {}

func (t *TestPersistentObjectWithMethod) GetValue(ctx context.Context, req *emptypb.Empty) (*structpb.Struct, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := structpb.NewStruct(map[string]interface{}{
		"value": t.Value,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (t *TestPersistentObjectWithMethod) ToData() (proto.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := structpb.NewStruct(map[string]interface{}{
		"id":    t.Id(),
		"value": t.Value,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (t *TestPersistentObjectWithMethod) FromData(data proto.Message) error {
	if data == nil {
		// New object - initialize with default value
		t.mu.Lock()
		t.Value = "default"
		t.mu.Unlock()
		return nil
	}

	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if value, ok := structData.Fields["value"]; ok {
		t.Value = value.GetStringValue()
	}
	return nil
}

// TestCallObject_AutoCreatePersistent_NotInStorage tests auto-creation of persistent objects not in storage
func TestCallObject_AutoCreatePersistent_NotInStorage(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Set up persistence provider
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	// Call method on non-existent object (not in storage)
	// Should auto-create with FromData(nil)
	resp, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", "persistent-1", "GetValue", &emptypb.Empty{}, 0)
	if err != nil {
		t.Fatalf("CallObject should succeed and auto-create object, got error: %v", err)
	}

	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	// Verify object was created
	node.objectsMu.RLock()
	obj, exists := node.objects["persistent-1"]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist after CallObject")
	}

	// Verify FromData(nil) was called - object should have default value
	testObj := obj.(*TestPersistentObjectWithMethod)
	testObj.mu.Lock()
	value := testObj.Value
	testObj.mu.Unlock()
	if value != "default" {
		t.Fatalf("Object should have default value from FromData(nil), got %s", value)
	}

	// Verify the response contains the default value
	respStruct := resp.(*structpb.Struct)
	if respStruct.Fields["value"].GetStringValue() != "default" {
		t.Fatalf("Response should contain default value, got %s", respStruct.Fields["value"].GetStringValue())
	}
}

// TestCallObject_AutoCreatePersistent_LoadFromStorage tests auto-creation with data from storage
func TestCallObject_AutoCreatePersistent_LoadFromStorage(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Set up persistence provider with existing data
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Pre-populate storage with object data
	existingData, err := structpb.NewStruct(map[string]interface{}{
		"id":    "persistent-2",
		"value": "loaded-from-storage",
	})
	if err != nil {
		t.Fatalf("Failed to create test data: %v", err)
	}
	jsonData, err := object.MarshalProtoToJSON(existingData)
	if err != nil {
		t.Fatalf("Failed to marshal test data: %v", err)
	}
	provider.storage["persistent-2"] = jsonData

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	// Call method on non-existent object (exists in storage)
	// Should auto-create and load from storage
	resp, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", "persistent-2", "GetValue", &emptypb.Empty{}, 0)
	if err != nil {
		t.Fatalf("CallObject should succeed and auto-create object from storage, got error: %v", err)
	}

	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	// Verify object was created
	node.objectsMu.RLock()
	obj, exists := node.objects["persistent-2"]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist after CallObject")
	}

	// Verify object was loaded from storage
	testObj := obj.(*TestPersistentObjectWithMethod)
	testObj.mu.Lock()
	value := testObj.Value
	testObj.mu.Unlock()
	if value != "loaded-from-storage" {
		t.Fatalf("Object should have value loaded from storage, got %s", value)
	}

	// Verify the response contains the loaded value
	respStruct := resp.(*structpb.Struct)
	if respStruct.Fields["value"].GetStringValue() != "loaded-from-storage" {
		t.Fatalf("Response should contain loaded value, got %s", respStruct.Fields["value"].GetStringValue())
	}
}

// TestCallObject_AutoCreate_MultipleCallsIdempotent tests that multiple calls are idempotent
func TestCallObject_AutoCreate_MultipleCallsIdempotent(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestObjectWithMethod)(nil))

	// First call - should auto-create
	_, err := node.CallObject(ctx, "TestObjectWithMethod", "idempotent-1", "TestMethod", &emptypb.Empty{}, 0)
	if err != nil {
		t.Fatalf("First CallObject failed: %v", err)
	}

	// Get the created object
	node.objectsMu.RLock()
	obj1 := node.objects["idempotent-1"]
	node.objectsMu.RUnlock()

	// Second call - should use existing object
	_, err = node.CallObject(ctx, "TestObjectWithMethod", "idempotent-1", "TestMethod", &emptypb.Empty{}, 0)
	if err != nil {
		t.Fatalf("Second CallObject failed: %v", err)
	}

	// Get the object again
	node.objectsMu.RLock()
	obj2 := node.objects["idempotent-1"]
	node.objectsMu.RUnlock()

	// Should be the same object instance
	if obj1 != obj2 {
		t.Fatal("Multiple calls should use the same object instance")
	}

	// Verify method was called twice
	testObj := obj1.(*TestObjectWithMethod)
	testObj.mu.Lock()
	callCount := testObj.CallCount
	testObj.mu.Unlock()
	if callCount != 2 {
		t.Fatalf("Method should have been called twice, got %d", callCount)
	}
}

// TestCallObject_AutoCreate_TypeMismatch tests that type validation still works
func TestCallObject_AutoCreate_TypeMismatch(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Register object types
	node.RegisterObjectType((*TestObjectWithMethod)(nil))
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	// Create object with one type
	_, err := node.CreateObject(ctx, "TestObjectWithMethod", "type-test-1", 0)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait for object to be created (CreateObject is async)
	waitForObjectCreated(t, node, "type-test-1", 5*time.Second)

	// Try to call with different type - should fail with type mismatch
	_, err = node.CallObject(ctx, "TestPersistentObjectWithMethod", "type-test-1", "GetValue", &emptypb.Empty{}, 0)
	if err == nil {
		t.Fatal("CallObject should fail with type mismatch")
	}

	if err.Error() != "failed to auto-create object type-test-1: object with id type-test-1 already exists but with different type: expected TestPersistentObjectWithMethod, got TestObjectWithMethod" {
		t.Fatalf("Expected type mismatch error, got: %v", err)
	}
}

// TestCallObject_AutoCreate_UnknownType tests that unknown types still fail
func TestCallObject_AutoCreate_UnknownType(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Don't register any types

	// Try to call with unknown type - should fail
	_, err := node.CallObject(ctx, "UnknownType", "unknown-1", "SomeMethod", &emptypb.Empty{}, 0)
	if err == nil {
		t.Fatal("CallObject should fail with unknown type")
	}

	// Error should mention unknown type
	if err.Error() != "failed to auto-create object unknown-1: unknown object type: UnknownType" {
		t.Fatalf("Expected unknown type error, got: %v", err)
	}
}

// TestCallObject_AutoCreate_MethodNotFound tests that method validation still works
func TestCallObject_AutoCreate_MethodNotFound(t *testing.T) {
	node := NewNode("test-node:1234")
	ctx := context.Background()

	// Register object type
	node.RegisterObjectType((*TestObjectWithMethod)(nil))

	// Call non-existent method - object should be created but method call should fail
	_, err := node.CallObject(ctx, "TestObjectWithMethod", "method-test-1", "NonExistentMethod", &emptypb.Empty{}, 0)
	if err == nil {
		t.Fatal("CallObject should fail with method not found")
	}

	// Verify error message
	if err.Error() != "method not found in class TestObjectWithMethod: NonExistentMethod" {
		t.Fatalf("Expected method not found error, got: %v", err)
	}

	// Verify object was still created
	node.objectsMu.RLock()
	_, exists := node.objects["method-test-1"]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should exist even though method was not found")
	}
}
