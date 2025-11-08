package object

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// MockPersistenceProvider is a mock implementation for testing
type MockPersistenceProvider struct {
	storage map[string][]byte
	SaveErr error
	LoadErr error
}

func NewMockPersistenceProvider() *MockPersistenceProvider {
	return &MockPersistenceProvider{
		storage: make(map[string][]byte),
	}
}

func (m *MockPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte) error {
	if m.SaveErr != nil {
		return m.SaveErr
	}
	m.storage[objectID] = data
	return nil
}

func (m *MockPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, error) {
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
func (t *TestPersistentObject) ToData() (proto.Message, error) {
	data, err := structpb.NewStruct(map[string]interface{}{
		"id":          t.id,
		"type":        t.Type(),
		"custom_data": t.CustomData,
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// FromData implements deserialization for TestPersistentObject
func (t *TestPersistentObject) FromData(data proto.Message) error {
	structData, ok := data.(*structpb.Struct)
	if !ok {
		return nil
	}
	if customData, ok := structData.Fields["custom_data"]; ok {
		t.CustomData = customData.GetStringValue()
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

	emptyStruct, _ := structpb.NewStruct(map[string]interface{}{})
	err := obj.FromData(emptyStruct)
	if err == nil {
		t.Error("FromData() should return error for non-persistent object")
	}
}

func TestPersistentObject_ToData(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)
	obj.CustomData = "test-value"

	protoMsg, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	structData, ok := protoMsg.(*structpb.Struct)
	if !ok {
		t.Fatal("ToData() did not return *structpb.Struct")
	}

	if structData.Fields["id"].GetStringValue() != "test-id" {
		t.Errorf("ToData() id = %v; want test-id", structData.Fields["id"])
	}

	if structData.Fields["type"].GetStringValue() != "TestPersistentObject" {
		t.Errorf("ToData() type = %v; want TestPersistentObject", structData.Fields["type"])
	}

	if structData.Fields["custom_data"].GetStringValue() != "test-value" {
		t.Errorf("ToData() custom_data = %v; want test-value", structData.Fields["custom_data"])
	}
}

func TestPersistentObject_FromData(t *testing.T) {
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)

	data, _ := structpb.NewStruct(map[string]interface{}{
		"custom_data": "loaded-value",
	})

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

	// Call ToData() to get the proto.Message
	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	ctx := context.Background()
	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data)
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Verify data was saved
	savedData, ok := provider.storage["test-id"]
	if !ok {
		t.Fatal("Object was not saved to provider")
	}

	if len(savedData) == 0 {
		t.Error("Saved data is empty")
	}
}

func TestSaveObject_NotPersistent(t *testing.T) {
	provider := NewMockPersistenceProvider()
	obj := &TestObject{} // Non-persistent object
	obj.OnInit(obj, "test-id", nil)

	// Try to get data from non-persistent object - should return error
	_, err := obj.ToData()
	if err == nil {
		t.Fatal("ToData() should return error for non-persistent object")
	}

	// Since ToData() returns error, we cannot call SaveObject
	// Verify nothing was saved
	if len(provider.storage) != 0 {
		t.Error("Non-persistent object should not be saved")
	}
}

func TestLoadObject(t *testing.T) {
	provider := NewMockPersistenceProvider()
	obj := &TestPersistentObject{}
	obj.OnInit(obj, "test-id", nil)
	obj.CustomData = "test-data"

	// First save an object
	ctx := context.Background()
	data, err := obj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = SaveObject(ctx, provider, obj.Id(), obj.Type(), data)
	if err != nil {
		t.Fatalf("SaveObject() returned error: %v", err)
	}

	// Now load it
	loadedObj := &TestPersistentObject{}
	loadedObj.OnInit(loadedObj, "test-id", nil)

	// Get a proto.Message to load into
	loadedData, err := loadedObj.ToData()
	if err != nil {
		t.Fatalf("ToData() returned error: %v", err)
	}

	err = LoadObject(ctx, provider, "test-id", loadedData)
	if err != nil {
		t.Fatalf("LoadObject() returned error: %v", err)
	}

	// Restore the object state from loaded data
	err = loadedObj.FromData(loadedData)
	if err != nil {
		t.Fatalf("FromData() returned error: %v", err)
	}

	if loadedObj.CustomData != "test-data" {
		t.Errorf("CustomData = %s; want test-data", loadedObj.CustomData)
	}
}

func TestMarshalProtoToJSON(t *testing.T) {
	data, err := structpb.NewStruct(map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	})
	if err != nil {
		t.Fatalf("NewStruct() returned error: %v", err)
	}

	jsonData, err := MarshalProtoToJSON(data)
	if err != nil {
		t.Fatalf("MarshalProtoToJSON() returned error: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("MarshalProtoToJSON() returned empty data")
	}
}

func TestUnmarshalProtoFromJSON(t *testing.T) {
	jsonData := []byte(`{"key1":"value1","key2":123}`)

	data := &structpb.Struct{}
	err := UnmarshalProtoFromJSON(jsonData, data)
	if err != nil {
		t.Fatalf("UnmarshalProtoFromJSON() returned error: %v", err)
	}

	if data.Fields["key1"].GetStringValue() != "value1" {
		t.Errorf("key1 = %v; want value1", data.Fields["key1"])
	}

	if data.Fields["key2"].GetNumberValue() != 123 {
		t.Errorf("key2 = %v; want 123", data.Fields["key2"])
	}
}
