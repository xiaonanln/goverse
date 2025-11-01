package object

import (
	"context"
	"testing"
)

// MockPersistenceProvider is a mock implementation for testing
type MockPersistenceProvider struct {
	storage map[string]map[string]interface{}
	SaveErr error
	LoadErr error
}

func NewMockPersistenceProvider() *MockPersistenceProvider {
	return &MockPersistenceProvider{
		storage: make(map[string]map[string]interface{}),
	}
}

func (m *MockPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data map[string]interface{}) error {
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.storage[objectID] = data
	return nil
}

func (m *MockPersistenceProvider) LoadObject(ctx context.Context, objectID string) (map[string]interface{}, error) {
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
	delete(m.storage, objectID)
	return nil
}

// TestPersistentObject is a test implementation of a persistent object
type TestPersistentObject struct {
	BaseObject
	CustomData string
}

func (t *TestPersistentObject) OnCreated() {}

// ToData implements persistence for TestPersistentObject
func (t *TestPersistentObject) ToData() (map[string]interface{}, error) {
	data := map[string]interface{}{
		"id":            t.id,
		"type":          t.Type(),
		"creation_time": t.creationTime.Unix(),
		"custom_data":   t.CustomData,
	}
	return data, nil
}

// FromData implements deserialization for TestPersistentObject
func (t *TestPersistentObject) FromData(data map[string]interface{}) error {
	if customData, ok := data["custom_data"].(string); ok {
		t.CustomData = customData
	}
	return nil
}

func TestBaseObject_ToData_NotPersistent(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	_, err := obj.ToData()
	if err == nil {
		t.Error("ToData() should return error for non-persistent object")
	}
}

func TestBaseObject_FromData_NotPersistent(t *testing.T) {
	obj := &TestObject{}
	obj.OnInit(obj, "test-id", nil)

	err := obj.FromData(map[string]interface{}{})
	if err == nil {
		t.Error("FromData() should return error for non-persistent object")
	}
}

func TestPersistentObject_ToData(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)
	obj.CustomData = "test-value"

	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	if data["id"] != "test-id" {
		t.Errorf("ToData() id = %v; want test-id", data["id"])
	}

	if data["type"] != "TestPersistentObject" {
		t.Errorf("ToData() type = %v; want TestPersistentObject", data["type"])
	}

	if data["custom_data"] != "test-value" {
		t.Errorf("ToData() custom_data = %v; want test-value", data["custom_data"])
	}
}

func TestPersistentObject_FromData(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)

	data := map[string]interface{}{
		"custom_data": "loaded-value",
	}

	err := obj.FromData(data)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	if obj.CustomData != "loaded-value" {
		t.Errorf("CustomData = %s; want loaded-value", obj.CustomData)
	}
}

func TestSaveObject_Persistent(t *testing.T) {
	provider := NewMockPersistenceProvider()
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)
	obj.CustomData = "test-data"

	ctx := context.Background()
	err := SaveObject(ctx, provider, obj)
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Verify data was saved
	savedData, ok := provider.storage["test-id"]
	if !ok {
		t.Fatal("Object was not saved to provider")
	}

	if savedData["custom_data"] != "test-data" {
		t.Errorf("Saved custom_data = %v; want test-data", savedData["custom_data"])
	}
}

func TestSaveObject_NotPersistent(t *testing.T) {
	provider := NewMockPersistenceProvider()
	obj := &TestObject{} // Non-persistent object
	obj.OnInit(obj, "test-id", nil)

	ctx := context.Background()
	err := SaveObject(ctx, provider, obj)
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Verify nothing was saved
	if len(provider.storage) != 0 {
		t.Error("Non-persistent object should not be saved")
	}
}

func TestLoadObject(t *testing.T) {
	provider := NewMockPersistenceProvider()
	
	// Setup saved data
	provider.storage["test-id"] = map[string]interface{}{
		"custom_data": "loaded-value",
	}

	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)

	ctx := context.Background()
	err := LoadObject(ctx, provider, obj, "test-id")
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	if obj.CustomData != "loaded-value" {
		t.Errorf("CustomData = %s; want loaded-value", obj.CustomData)
	}
}

func TestMarshalToJSON(t *testing.T) {
	data := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	}

	jsonData, err := MarshalToJSON(data)
	if err != nil {
		t.Fatalf("MarshalToJSON() returned error: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("MarshalToJSON() returned empty data")
	}
}

func TestUnmarshalFromJSON(t *testing.T) {
	jsonData := []byte(`{"key1":"value1","key2":123}`)

	data, err := UnmarshalFromJSON(jsonData)
	if err != nil {
		t.Fatalf("UnmarshalFromJSON() returned error: %v", err)
	}

	if data["key1"] != "value1" {
		t.Errorf("key1 = %v; want value1", data["key1"])
	}

	if data["key2"] != float64(123) { // JSON numbers are float64
		t.Errorf("key2 = %v; want 123", data["key2"])
	}
}
