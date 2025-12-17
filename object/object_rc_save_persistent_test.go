package object

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/util/protohelper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestProcessReliableCall_SavesPersistentObject tests that persistent objects
// are saved after RC execution completes
func TestProcessReliableCall_SavesPersistentObject(t *testing.T) {
	obj := &testPersistentRCObject{}
	obj.OnInit(obj, "persistent-test")
	obj.value = 10
	obj.SetNextRcseq(5)

	provider := &mockSaveTrackingProvider{
		storage: make(map[int64]*ReliableCall),
		saves:   make([]SaveRecord, 0),
	}

	// Create a reliable call using protohelper
	requestMsg := wrapperspb.Int32(100)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	call := &ReliableCall{
		Seq:         5,
		CallID:      "call-1",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the call
	obj.processReliableCall(provider, call)

	// Verify nextRcseq was updated
	if obj.GetNextRcseq() != 6 {
		t.Errorf("Expected nextRcseq=6, got %d", obj.GetNextRcseq())
	}

	// Verify state was updated
	if obj.value != 100 {
		t.Errorf("Expected value=100, got %d", obj.value)
	}

	// Verify SaveObject was called exactly once
	if len(provider.saves) != 1 {
		t.Fatalf("Expected 1 save call, got %d", len(provider.saves))
	}

	// Verify save was called with correct parameters
	save := provider.saves[0]
	if save.objectID != obj.Id() {
		t.Errorf("Expected objectID=%s, got %s", obj.Id(), save.objectID)
	}
	if save.objectType != "testPersistentRCObject" {
		t.Errorf("Expected objectType=testPersistentRCObject, got %s", save.objectType)
	}
	if save.nextRcseq != 6 {
		t.Errorf("Expected nextRcseq=6, got %d", save.nextRcseq)
	}

	// Verify saved data reflects the new state
	savedMsg := &wrapperspb.Int32Value{}
	if err := UnmarshalProtoFromJSON(save.data, savedMsg); err != nil {
		t.Fatalf("Failed to unmarshal saved data: %v", err)
	}
	if savedMsg.Value != 100 {
		t.Errorf("Expected saved value=100, got %d", savedMsg.Value)
	}

	// Verify RC status was updated
	if call.Status != "success" {
		t.Errorf("Expected status=success, got %s", call.Status)
	}
}

// TestProcessReliableCall_SkipsNonPersistentObject tests that non-persistent objects
// skip the save step
func TestProcessReliableCall_SkipsNonPersistentObject(t *testing.T) {
	obj := &testNonPersistentRCObject{}
	obj.OnInit(obj, "non-persistent-test")
	obj.value = 10
	obj.SetNextRcseq(5)

	provider := &mockSaveTrackingProvider{
		storage: make(map[int64]*ReliableCall),
		saves:   make([]SaveRecord, 0),
	}

	// Create a reliable call
	requestMsg := wrapperspb.Int32(100)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	call := &ReliableCall{
		Seq:         5,
		CallID:      "call-2",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the call
	obj.processReliableCall(provider, call)

	// Verify nextRcseq was updated
	if obj.GetNextRcseq() != 6 {
		t.Errorf("Expected nextRcseq=6, got %d", obj.GetNextRcseq())
	}

	// Verify state was updated
	if obj.value != 100 {
		t.Errorf("Expected value=100, got %d", obj.value)
	}

	// Verify SaveObject was NOT called (non-persistent object)
	if len(provider.saves) != 0 {
		t.Fatalf("Expected 0 save calls for non-persistent object, got %d", len(provider.saves))
	}

	// Verify RC status was still updated
	if call.Status != "success" {
		t.Errorf("Expected status=success, got %s", call.Status)
	}
}

// TestProcessReliableCall_SaveFailureDoesNotBlockRCCompletion tests that save failures
// are logged but don't prevent RC completion
func TestProcessReliableCall_SaveFailureDoesNotBlockRCCompletion(t *testing.T) {
	obj := &testPersistentRCObject{}
	obj.OnInit(obj, "save-failure-test")
	obj.value = 10
	obj.SetNextRcseq(5)

	provider := &mockFailingSaveProvider{
		storage:   make(map[int64]*ReliableCall),
		saveError: fmt.Errorf("simulated save failure"),
	}

	// Create a reliable call
	requestMsg := wrapperspb.Int32(100)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	call := &ReliableCall{
		Seq:         5,
		CallID:      "call-3",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the call - should not panic or fail
	obj.processReliableCall(provider, call)

	// Verify nextRcseq was updated despite save failure
	if obj.GetNextRcseq() != 6 {
		t.Errorf("Expected nextRcseq=6, got %d", obj.GetNextRcseq())
	}

	// Verify state was updated despite save failure
	if obj.value != 100 {
		t.Errorf("Expected value=100, got %d", obj.value)
	}

	// Verify RC status was updated despite save failure
	if call.Status != "success" {
		t.Errorf("Expected status=success, got %s", call.Status)
	}
}

// testPersistentRCObject is a test object that supports persistence and RC methods
type testPersistentRCObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testPersistentRCObject) OnCreated() {}

func (obj *testPersistentRCObject) ToData() (proto.Message, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testPersistentRCObject) FromData(data proto.Message) error {
	if data == nil {
		return nil
	}
	obj.mu.Lock()
	defer obj.mu.Unlock()
	if msg, ok := data.(*wrapperspb.Int32Value); ok {
		obj.value = msg.Value
	}
	return nil
}

func (obj *testPersistentRCObject) SetValue(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	obj.value = req.Value
	return wrapperspb.Int32(obj.value), nil
}

// testNonPersistentRCObject is a test object that does NOT support persistence
type testNonPersistentRCObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testNonPersistentRCObject) OnCreated() {}

// ToData returns ErrNotPersistent for non-persistent objects
// Note: We rely on BaseObject's default implementation which returns ErrNotPersistent

func (obj *testNonPersistentRCObject) SetValue(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	obj.value = req.Value
	return wrapperspb.Int32(obj.value), nil
}

// SaveRecord tracks a call to SaveObject
type SaveRecord struct {
	objectID   string
	objectType string
	data       []byte
	nextRcseq  int64
}

// mockSaveTrackingProvider tracks all SaveObject calls
type mockSaveTrackingProvider struct {
	storage map[int64]*ReliableCall
	saves   []SaveRecord
	mu      sync.Mutex
}

func (m *mockSaveTrackingProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves = append(m.saves, SaveRecord{
		objectID:   objectID,
		objectType: objectType,
		data:       data,
		nextRcseq:  nextRcseq,
	})
	return nil
}

func (m *mockSaveTrackingProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return nil, 0, nil
}

func (m *mockSaveTrackingProvider) DeleteObject(ctx context.Context, objectID string) error {
	return nil
}

func (m *mockSaveTrackingProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*ReliableCall, error) {
	return nil, nil
}

func (m *mockSaveTrackingProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		call.Status = status
		call.ResultData = resultData
		call.Error = errorMessage
	}
	return nil
}

func (m *mockSaveTrackingProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*ReliableCall, error) {
	return nil, nil
}

func (m *mockSaveTrackingProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		return call, nil
	}
	return nil, nil
}

// mockFailingSaveProvider simulates SaveObject failures
type mockFailingSaveProvider struct {
	storage   map[int64]*ReliableCall
	saveError error
	mu        sync.Mutex
}

func (m *mockFailingSaveProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	return m.saveError
}

func (m *mockFailingSaveProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return nil, 0, nil
}

func (m *mockFailingSaveProvider) DeleteObject(ctx context.Context, objectID string) error {
	return nil
}

func (m *mockFailingSaveProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*ReliableCall, error) {
	return nil, nil
}

func (m *mockFailingSaveProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		call.Status = status
		call.ResultData = resultData
		call.Error = errorMessage
	}
	return nil
}

func (m *mockFailingSaveProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*ReliableCall, error) {
	return nil, nil
}

func (m *mockFailingSaveProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		return call, nil
	}
	return nil, nil
}
