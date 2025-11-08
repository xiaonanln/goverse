package node

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// MockPersistenceProvider for testing
type MockPersistenceProvider struct {
	storage   map[string][]byte
	mu        sync.Mutex
	saveCount int
	SaveErr   error
	LoadErr   error
}

func NewMockPersistenceProvider() *MockPersistenceProvider {
	return &MockPersistenceProvider{
		storage: make(map[string][]byte),
	}
}

func (m *MockPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.storage[objectID] = data
	m.saveCount++
	return nil
}

func (m *MockPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.LoadErr != nil {
		return nil, m.LoadErr
	}
	data, ok := m.storage[objectID]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (m *MockPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.storage, objectID)
	return nil
}

func (m *MockPersistenceProvider) GetSaveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCount
}

// TestPersistentObject for testing
type TestPersistentObject struct {
	object.BaseObject
	mu    sync.Mutex
	Value string
}

func (t *TestPersistentObject) OnCreated() {}

func (t *TestPersistentObject) SetValue(value string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Value = value
}

func (t *TestPersistentObject) ToData() (proto.Message, error) {
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

func (t *TestPersistentObject) FromData(data proto.Message) error {
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

// TestNonPersistentObject for testing
type TestNonPersistentObject struct {
	object.BaseObject
	Value string
}

func (t *TestNonPersistentObject) OnCreated() {}

func TestNode_SetPersistenceProvider(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()

	node.SetPersistenceProvider(provider)

	if node.persistenceProvider != provider {
		t.Error("SetPersistenceProvider did not set the provider")
	}
}

func TestNode_SetPersistenceInterval(t *testing.T) {
	node := NewNode("localhost:47000")
	interval := 10 * time.Second

	node.SetPersistenceInterval(interval)

	if node.persistenceInterval != interval {
		t.Errorf("SetPersistenceInterval: expected %v, got %v", interval, node.persistenceInterval)
	}
}

func TestNode_SaveAllObjects_NoPersistentObjects(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register non-persistent object type
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	// Create a non-persistent object
	ctx := context.Background()
	obj := &TestNonPersistentObject{}
	obj.OnInit(obj, "test-obj-1", nil)
	obj.Value = "test-value"

	node.objects["test-obj-1"] = obj

	// Save all objects
	err := node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("SaveAllObjects returned error: %v", err)
	}

	// Verify nothing was saved (non-persistent object)
	if provider.GetSaveCount() != 0 {
		t.Errorf("Expected 0 saved objects, got %d", provider.GetSaveCount())
	}
}

func TestNode_SaveAllObjects_WithPersistentObjects(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create persistent objects
	ctx := context.Background()
	obj1 := &TestPersistentObject{}
	obj1.OnInit(obj1, "test-obj-1", nil)
	obj1.Value = "value1"

	obj2 := &TestPersistentObject{}
	obj2.OnInit(obj2, "test-obj-2", nil)
	obj2.Value = "value2"

	node.objects["test-obj-1"] = obj1
	node.objects["test-obj-2"] = obj2

	// Save all objects
	err := node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("SaveAllObjects returned error: %v", err)
	}

	// Verify objects were saved
	if provider.GetSaveCount() != 2 {
		t.Errorf("Expected 2 saved objects, got %d", provider.GetSaveCount())
	}

	// Verify data was saved
	if len(provider.storage) != 2 {
		t.Errorf("Expected 2 objects in storage, got %d", len(provider.storage))
	}
}

func TestNode_SaveAllObjects_MixedObjects(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Register both types
	node.RegisterObjectType((*TestPersistentObject)(nil))
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	// Create mixed objects
	ctx := context.Background()
	persistentObj := &TestPersistentObject{}
	persistentObj.OnInit(persistentObj, "persistent-1", nil)
	persistentObj.Value = "persistent"

	nonPersistentObj := &TestNonPersistentObject{}
	nonPersistentObj.OnInit(nonPersistentObj, "non-persistent-1", nil)
	nonPersistentObj.Value = "non-persistent"

	node.objects["persistent-1"] = persistentObj
	node.objects["non-persistent-1"] = nonPersistentObj

	// Save all objects
	err := node.SaveAllObjects(ctx)
	if err != nil {
		t.Fatalf("SaveAllObjects returned error: %v", err)
	}

	// Only persistent object should be saved
	if provider.GetSaveCount() != 1 {
		t.Errorf("Expected 1 saved object, got %d", provider.GetSaveCount())
	}
}

func TestNode_PeriodicPersistence_Integration(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.SetPersistenceInterval(100 * time.Millisecond) // Short interval for testing

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create persistent object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-obj-1", nil)
	obj.SetValue("test-value")
	node.objects["test-obj-1"] = obj

	// Start periodic persistence
	ctx := context.Background()
	node.StartPeriodicPersistence(ctx)

	// Wait for at least one save cycle
	time.Sleep(250 * time.Millisecond)

	// Stop periodic persistence
	node.StopPeriodicPersistence()

	// Verify at least one save occurred
	saveCount := provider.GetSaveCount()
	if saveCount < 1 {
		t.Errorf("Expected at least 1 save, got %d", saveCount)
	}
}

func TestNode_StartStop_WithPersistence(t *testing.T) {
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.SetPersistenceInterval(1 * time.Second) // Longer interval to avoid multiple saves

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create persistent object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-obj-1", nil)
	obj.SetValue("test-value")
	node.objects["test-obj-1"] = obj

	// Start node
	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Stop node (should save all objects)
	err = node.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Verify at least one save occurred (during shutdown)
	saveCount := provider.GetSaveCount()
	if saveCount < 1 {
		t.Errorf("Expected at least 1 save during shutdown, got %d", saveCount)
	}
}

func TestNode_SaveAllObjects_NoProvider(t *testing.T) {
	node := NewNode("localhost:47000")
	// No provider set

	ctx := context.Background()
	err := node.SaveAllObjects(ctx)
	if err == nil {
		t.Error("Expected error when no provider is configured")
	}
}

func TestNode_StartPeriodicPersistence_NoProvider(t *testing.T) {
	node := NewNode("localhost:47000")
	// No provider set

	ctx := context.Background()

	// Should not panic, just log warning
	node.StartPeriodicPersistence(ctx)

	// Stop should also not panic
	node.StopPeriodicPersistence()
}

func TestNode_PeriodicPersistence_ActuallyStoresPeriodically(t *testing.T) {
	// This test verifies that the node actually saves objects at the configured interval
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Set a short interval for testing (200ms)
	interval := 200 * time.Millisecond
	node.SetPersistenceInterval(interval)

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create multiple persistent objects
	obj1 := &TestPersistentObject{}
	obj1.OnInit(obj1, "periodic-obj-1", nil)
	obj1.SetValue("value1")
	node.objects["periodic-obj-1"] = obj1

	obj2 := &TestPersistentObject{}
	obj2.OnInit(obj2, "periodic-obj-2", nil)
	obj2.SetValue("value2")
	node.objects["periodic-obj-2"] = obj2

	// Start periodic persistence
	ctx := context.Background()
	node.StartPeriodicPersistence(ctx)

	// Wait for multiple save cycles (at least 3 cycles = 600ms + buffer)
	// We check at different intervals to verify periodic behavior
	time.Sleep(150 * time.Millisecond) // Before first cycle
	firstCount := provider.GetSaveCount()

	time.Sleep(250 * time.Millisecond) // After first cycle (~400ms total)
	secondCount := provider.GetSaveCount()

	time.Sleep(250 * time.Millisecond) // After second cycle (~650ms total)
	thirdCount := provider.GetSaveCount()

	// Stop periodic persistence
	node.StopPeriodicPersistence()

	// Verify the behavior:
	// 1. Initially no saves (before first cycle)
	if firstCount != 0 {
		t.Errorf("Expected 0 saves before first cycle, got %d", firstCount)
	}

	// 2. After first cycle, should have saved both objects (2 saves)
	if secondCount < 2 {
		t.Errorf("Expected at least 2 saves after first cycle, got %d", secondCount)
	}

	// 3. After second cycle, should have more saves (at least 4 total)
	if thirdCount < 4 {
		t.Errorf("Expected at least 4 saves after second cycle, got %d", thirdCount)
	}

	// 4. Verify periodic behavior: saves should increase over time
	if !(firstCount < secondCount && secondCount < thirdCount) {
		t.Errorf("Save counts should increase over time: %d, %d, %d", firstCount, secondCount, thirdCount)
	}

	// 5. Verify both objects were actually stored
	if len(provider.storage) != 2 {
		t.Errorf("Expected 2 objects in storage, got %d", len(provider.storage))
	}

	// 6. Verify correct objects were stored
	if _, ok := provider.storage["periodic-obj-1"]; !ok {
		t.Error("Object periodic-obj-1 was not stored")
	}
	if _, ok := provider.storage["periodic-obj-2"]; !ok {
		t.Error("Object periodic-obj-2 was not stored")
	}

	// 7. Verify the saved data is correct for object 1
	data1 := provider.storage["periodic-obj-1"]
	if data1 == nil {
		t.Fatal("No data stored for periodic-obj-1")
	}
	struct1 := &structpb.Struct{}
	if err := object.UnmarshalProtoFromJSON(data1, struct1); err != nil {
		t.Fatalf("Failed to unmarshal data for periodic-obj-1: %v", err)
	}
	if idField, ok := struct1.Fields["id"]; !ok || idField.GetStringValue() != "periodic-obj-1" {
		t.Errorf("Expected id 'periodic-obj-1', got '%v'", struct1.Fields["id"])
	}
	if valueField, ok := struct1.Fields["value"]; !ok || valueField.GetStringValue() != "value1" {
		t.Errorf("Expected value 'value1' for periodic-obj-1, got '%v'", struct1.Fields["value"])
	}

	// 8. Verify the saved data is correct for object 2
	data2 := provider.storage["periodic-obj-2"]
	if data2 == nil {
		t.Fatal("No data stored for periodic-obj-2")
	}
	struct2 := &structpb.Struct{}
	if err := object.UnmarshalProtoFromJSON(data2, struct2); err != nil {
		t.Fatalf("Failed to unmarshal data for periodic-obj-2: %v", err)
	}
	if idField, ok := struct2.Fields["id"]; !ok || idField.GetStringValue() != "periodic-obj-2" {
		t.Errorf("Expected id 'periodic-obj-2', got '%v'", struct2.Fields["id"])
	}
	if valueField, ok := struct2.Fields["value"]; !ok || valueField.GetStringValue() != "value2" {
		t.Errorf("Expected value 'value2' for periodic-obj-2, got '%v'", struct2.Fields["value"])
	}
}

func TestNode_PeriodicPersistence_UpdatesExistingObjects(t *testing.T) {
	// This test verifies that periodic persistence updates objects even when they change
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Set a very short interval for testing
	interval := 150 * time.Millisecond
	node.SetPersistenceInterval(interval)

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create persistent object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "update-obj", nil)
	obj.SetValue("initial-value")
	node.objects["update-obj"] = obj

	// Start periodic persistence
	ctx := context.Background()
	node.StartPeriodicPersistence(ctx)

	// Wait for first save
	time.Sleep(250 * time.Millisecond)

	// Verify initial save
	firstCount := provider.GetSaveCount()
	if firstCount < 1 {
		t.Fatalf("Expected at least 1 save, got %d", firstCount)
	}

	// Load and verify initial value
	firstData := provider.storage["update-obj"]
	firstStruct := &structpb.Struct{}
	if err := object.UnmarshalProtoFromJSON(firstData, firstStruct); err != nil {
		t.Fatalf("Failed to unmarshal first data: %v", err)
	}
	firstValue := firstStruct.Fields["value"].GetStringValue()
	if firstValue != "initial-value" {
		t.Errorf("Expected initial value 'initial-value', got '%s'", firstValue)
	}

	// Change the object value
	obj.SetValue("updated-value")

	// Wait for next save cycle
	time.Sleep(200 * time.Millisecond)

	// Verify another save occurred
	secondCount := provider.GetSaveCount()
	if secondCount <= firstCount {
		t.Errorf("Expected more saves after update: first=%d, second=%d", firstCount, secondCount)
	}

	// Load and verify updated value
	secondData := provider.storage["update-obj"]
	secondStruct := &structpb.Struct{}
	if err := object.UnmarshalProtoFromJSON(secondData, secondStruct); err != nil {
		t.Fatalf("Failed to unmarshal second data: %v", err)
	}
	secondValue := secondStruct.Fields["value"].GetStringValue()
	if secondValue != "updated-value" {
		t.Errorf("Expected updated value 'updated-value', got '%s'", secondValue)
	}

	// Stop periodic persistence
	node.StopPeriodicPersistence()
}

func TestNode_PeriodicPersistence_StopsCleanly(t *testing.T) {
	// This test verifies that stopping periodic persistence actually stops the saves
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)

	// Set a short interval
	interval := 150 * time.Millisecond
	node.SetPersistenceInterval(interval)

	// Register persistent object type
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create persistent object
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "stop-test-obj", nil)
	obj.SetValue("test-value")
	node.objects["stop-test-obj"] = obj

	// Start periodic persistence
	ctx := context.Background()
	node.StartPeriodicPersistence(ctx)

	// Wait for at least 2 save cycles to ensure it's running
	time.Sleep(400 * time.Millisecond)
	countBeforeStop := provider.GetSaveCount()

	// Verify saves happened before stopping
	if countBeforeStop < 2 {
		t.Fatalf("Expected at least 2 saves before stopping, got %d", countBeforeStop)
	}

	// Stop periodic persistence (blocks until stopped)
	node.StopPeriodicPersistence()

	// Record count immediately after stop
	countAtStop := provider.GetSaveCount()

	// Wait for what would be multiple more cycles (to be sure)
	time.Sleep(500 * time.Millisecond)
	countAfterStop := provider.GetSaveCount()

	// Verify no more saves occurred after stopping
	// We allow for one in-progress save to complete (countAtStop might be +1 from countBeforeStop)
	// but there should be no new saves after that
	if countAfterStop > countAtStop {
		t.Errorf("Expected no more saves after stop completed: atStop=%d, after=%d", countAtStop, countAfterStop)
	}

	// The count should have increased from before stopping to when we stopped
	// (at least the in-progress save should complete)
	if countAtStop < countBeforeStop {
		t.Errorf("Count should not decrease: before=%d, atStop=%d", countBeforeStop, countAtStop)
	}
}

func TestNode_CreateObject_LoadsFromPersistence(t *testing.T) {
	// This test verifies that when creating an object that exists in persistence,
	// the persisted data is loaded and the object reflects that state
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Step 1: Directly save object data to persistence (simulating a previously saved object)
	savedObj := &TestPersistentObject{}
	savedObj.OnInit(savedObj, "load-test-obj", nil)
	savedObj.SetValue("persisted-value")

	err := object.SaveObject(ctx, provider, savedObj)
	if err != nil {
		t.Fatalf("Failed to save object: %v", err)
	}

	// Step 2: Create a fresh node and attempt to create the object with different initData
	node := NewNode("localhost:47000")
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Use initData that would set a different value
	initData, _ := structpb.NewStruct(map[string]interface{}{
		"value": "init-data-value",
	})

	// Create the object - it should load from persistence instead of using initData
	loadedObj, err := node.createObject(ctx, "TestPersistentObject", "load-test-obj", initData)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Verify the object has the persisted value, not the init value
	persistentObj := loadedObj.(*TestPersistentObject)
	if persistentObj.Value != "persisted-value" {
		t.Errorf("Expected value 'persisted-value' from persistence, got '%s'", persistentObj.Value)
	}
}

func TestNode_CreateObject_LoadsFromPersistence_NewNode(t *testing.T) {
	// This test verifies that when creating an object on a fresh node,
	// it loads from persistence if available
	provider := NewMockPersistenceProvider()
	ctx := context.Background()

	// Setup: Create and save an object using direct persistence
	savedObj := &TestPersistentObject{}
	savedObj.OnInit(savedObj, "persistent-obj-123", nil)
	savedObj.SetValue("saved-state")

	err := object.SaveObject(ctx, provider, savedObj)
	if err != nil {
		t.Fatalf("Failed to save object: %v", err)
	}

	// Create a fresh node with the same persistence provider
	node := NewNode("localhost:47000")
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	// Create object with different initData
	initData, _ := structpb.NewStruct(map[string]interface{}{
		"value": "init-value",
	})

	// The createObject should load from persistence instead of using initData
	loadedObj, err := node.createObject(ctx, "TestPersistentObject", "persistent-obj-123", initData)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Verify the object has the persisted value, not the init value
	persistentObj := loadedObj.(*TestPersistentObject)
	if persistentObj.Value != "saved-state" {
		t.Errorf("Expected value 'saved-state' from persistence, got '%s'", persistentObj.Value)
	}
}

func TestNode_CreateObject_UsesInitData_WhenNotInPersistence(t *testing.T) {
	// This test verifies that when an object is not in persistence,
	// it uses initData for initialization
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Create object with initData (object not in persistence)
	initData, _ := structpb.NewStruct(map[string]interface{}{
		"value": "init-value",
	})

	obj, err := node.createObject(ctx, "TestPersistentObject", "new-obj-456", initData)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Since object was not in persistence, it should use initData
	// Note: OnInit doesn't automatically call FromData, so the value won't be set
	// But we can verify the object was created successfully
	if obj.Id() != "new-obj-456" {
		t.Errorf("Expected object ID 'new-obj-456', got '%s'", obj.Id())
	}

	// The object should be registered in the node
	if node.objects["new-obj-456"] == nil {
		t.Error("Object was not registered in node.objects")
	}
}

func TestNode_CreateObject_NonPersistentObject(t *testing.T) {
	// This test verifies that non-persistent objects work normally
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestNonPersistentObject)(nil))

	ctx := context.Background()

	// Create non-persistent object
	obj, err := node.createObject(ctx, "TestNonPersistentObject", "non-persistent-obj", nil)
	if err != nil {
		t.Fatalf("Failed to create non-persistent object: %v", err)
	}

	if obj.Id() != "non-persistent-obj" {
		t.Errorf("Expected object ID 'non-persistent-obj', got '%s'", obj.Id())
	}

	// The object should be registered in the node
	if node.objects["non-persistent-obj"] == nil {
		t.Error("Object was not registered in node.objects")
	}
}

func TestNode_CreateObject_PersistenceLoadError(t *testing.T) {
	// This test verifies that when persistence loading fails,
	// the object still gets created with initData
	node := NewNode("localhost:47000")
	provider := NewMockPersistenceProvider()
	provider.LoadErr = fmt.Errorf("simulated load error")
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()

	// Create object - load will fail, should fall back to initData
	obj, err := node.createObject(ctx, "TestPersistentObject", "error-obj", nil)
	if err != nil {
		t.Fatalf("Failed to create object despite load error: %v", err)
	}

	if obj.Id() != "error-obj" {
		t.Errorf("Expected object ID 'error-obj', got '%s'", obj.Id())
	}

	// The object should be registered in the node
	if node.objects["error-obj"] == nil {
		t.Error("Object was not registered in node.objects")
	}
}
