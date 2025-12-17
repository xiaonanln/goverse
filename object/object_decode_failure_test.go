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

// TestProcessReliableCall_AdvancesNextRcseqOnDecodeFailure tests that nextRcseq
// is advanced even when request data decoding fails.
//
// Bug: Previously, decode failures would return early without advancing nextRcseq,
// causing the object to retain a stale progress marker.
//
// Fix: nextRcseq should advance for every processed call, regardless of success/failure.
func TestProcessReliableCall_AdvancesNextRcseqOnDecodeFailure(t *testing.T) {
	// Create test object
	obj := &testDecodeObject{}
	obj.OnInit(obj, "decode-test")
	obj.value = 0
	obj.SetNextRcseq(10)

	// Create mock provider that tracks status updates
	provider := &mockDecodeProvider{
		statusUpdates: make(map[int64]string),
	}

	// Create a reliable call with INVALID request data (cannot be decoded)
	call := &ReliableCall{
		Seq:         10,
		CallID:      "call-decode-fail",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: []byte("invalid-protobuf-data"), // This will fail to decode
		Status:      "pending",
	}

	// Initial nextRcseq should be 10
	if obj.GetNextRcseq() != 10 {
		t.Fatalf("Expected initial nextRcseq=10, got %d", obj.GetNextRcseq())
	}

	// Process the call with invalid data
	obj.processReliableCall(provider, call)

	// CRITICAL: nextRcseq MUST advance to 11 even though decode failed
	if obj.GetNextRcseq() != 11 {
		t.Errorf("nextRcseq not advanced on decode failure: expected 11, got %d", obj.GetNextRcseq())
	}

	// Verify the call was marked as skipped (decode failures are marked as skipped, not failed)
	status, ok := provider.statusUpdates[10]
	if !ok {
		t.Errorf("Status update not recorded for seq=10")
	}
	if status != "skipped" {
		t.Errorf("Expected status='skipped', got '%s'", status)
	}

	// Verify call status was updated to skipped
	if call.Status != "skipped" {
		t.Errorf("Expected call.Status='skipped', got '%s'", call.Status)
	}

	// Verify error message was set
	if call.Error == "" {
		t.Errorf("Expected error message to be set, got empty string")
	}

	// === Verify subsequent valid call can be processed ===
	// This ensures decode failure doesn't break the object's ability to process future calls

	requestMsg := wrapperspb.Int32(100)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	successCall := &ReliableCall{
		Seq:         11, // Next seq after the failed one
		CallID:      "call-after-decode-fail",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the valid call
	obj.processReliableCall(provider, successCall)

	// nextRcseq should advance to 12
	if obj.GetNextRcseq() != 12 {
		t.Errorf("nextRcseq not advanced after recovery: expected 12, got %d", obj.GetNextRcseq())
	}

	// Verify the call was marked as success
	status, ok = provider.statusUpdates[11]
	if !ok {
		t.Errorf("Status update not recorded for seq=11")
	}
	if status != "success" {
		t.Errorf("Expected status='success' for follow-up call, got '%s'", status)
	}

	// Verify the method actually executed and updated the value
	if obj.value != 100 {
		t.Errorf("Expected value=100 after follow-up call, got %d", obj.value)
	}
}

// TestProcessReliableCall_AdvancesNextRcseqOnMethodFailure tests that nextRcseq
// is advanced when method invocation fails (existing behavior verification).
func TestProcessReliableCall_AdvancesNextRcseqOnMethodFailure(t *testing.T) {
	// Create test object
	obj := &testDecodeObject{}
	obj.OnInit(obj, "method-fail-test")
	obj.value = 0
	obj.SetNextRcseq(5)

	// Create mock provider
	provider := &mockDecodeProvider{
		statusUpdates: make(map[int64]string),
	}

	// Create a reliable call with VALID request data but will cause method error
	// Calling SetValueWithError method which always returns an error
	requestMsg := wrapperspb.Int32(-999)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	call := &ReliableCall{
		Seq:         5,
		CallID:      "call-method-fail",
		ObjectID:    obj.Id(),
		MethodName:  "SetValueWithError",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the call
	obj.processReliableCall(provider, call)

	// nextRcseq MUST advance even though method failed
	if obj.GetNextRcseq() != 6 {
		t.Errorf("nextRcseq not advanced on method failure: expected 6, got %d", obj.GetNextRcseq())
	}

	// Verify the call was marked as failed
	status, ok := provider.statusUpdates[5]
	if !ok {
		t.Errorf("Status update not recorded for seq=5")
	}
	if status != "failed" {
		t.Errorf("Expected status='failed', got '%s'", status)
	}

	// === Verify subsequent valid call can be processed ===
	// This ensures method failure doesn't break the object's ability to process future calls

	successRequestMsg := wrapperspb.Int32(200)
	successRequestData, err := protohelper.MsgToBytes(successRequestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	successCall := &ReliableCall{
		Seq:         6, // Next seq after the failed one
		CallID:      "call-after-method-fail",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: successRequestData,
		Status:      "pending",
	}

	// Process the valid call
	obj.processReliableCall(provider, successCall)

	// nextRcseq should advance to 7
	if obj.GetNextRcseq() != 7 {
		t.Errorf("nextRcseq not advanced after recovery: expected 7, got %d", obj.GetNextRcseq())
	}

	// Verify the call was marked as success
	status, ok = provider.statusUpdates[6]
	if !ok {
		t.Errorf("Status update not recorded for seq=6")
	}
	if status != "success" {
		t.Errorf("Expected status='success' for follow-up call, got '%s'", status)
	}

	// Verify the method actually executed and updated the value
	if obj.value != 200 {
		t.Errorf("Expected value=200 after follow-up call, got %d", obj.value)
	}
}

// TestProcessReliableCall_AdvancesNextRcseqOnSuccess tests that nextRcseq
// is advanced on successful execution (existing behavior verification).
func TestProcessReliableCall_AdvancesNextRcseqOnSuccess(t *testing.T) {
	// Create test object
	obj := &testDecodeObject{}
	obj.OnInit(obj, "success-test")
	obj.value = 0
	obj.SetNextRcseq(20)

	// Create mock provider
	provider := &mockDecodeProvider{
		statusUpdates: make(map[int64]string),
	}

	// Create a reliable call with valid request data
	requestMsg := wrapperspb.Int32(42)
	requestData, err := protohelper.MsgToBytes(requestMsg)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	call := &ReliableCall{
		Seq:         20,
		CallID:      "call-success",
		ObjectID:    obj.Id(),
		MethodName:  "SetValue",
		RequestData: requestData,
		Status:      "pending",
	}

	// Process the call
	obj.processReliableCall(provider, call)

	// nextRcseq MUST advance on success
	if obj.GetNextRcseq() != 21 {
		t.Errorf("nextRcseq not advanced on success: expected 21, got %d", obj.GetNextRcseq())
	}

	// Verify the call was marked as success
	status, ok := provider.statusUpdates[20]
	if !ok {
		t.Errorf("Status update not recorded for seq=20")
	}
	if status != "success" {
		t.Errorf("Expected status='success', got '%s'", status)
	}

	// Verify value was updated
	if obj.value != 42 {
		t.Errorf("Expected value=42, got %d", obj.value)
	}
}

// testDecodeObject is a test object for decode failure testing
type testDecodeObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testDecodeObject) OnCreated() {}

func (obj *testDecodeObject) ToData() (proto.Message, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testDecodeObject) FromData(data proto.Message) error {
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

func (obj *testDecodeObject) SetValue(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	obj.value = req.Value
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testDecodeObject) SetValueWithError(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	return nil, fmt.Errorf("simulated method failure")
}

// mockDecodeProvider is a minimal mock for testing decode failures
type mockDecodeProvider struct {
	statusUpdates map[int64]string
	mu            sync.Mutex
}

func (m *mockDecodeProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	return nil
}

func (m *mockDecodeProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return nil, 0, nil
}

func (m *mockDecodeProvider) DeleteObject(ctx context.Context, objectID string) error {
	return nil
}

func (m *mockDecodeProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*ReliableCall, error) {
	return nil, nil
}

func (m *mockDecodeProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates[seq] = status
	return nil
}

func (m *mockDecodeProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*ReliableCall, error) {
	return nil, nil
}

func (m *mockDecodeProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*ReliableCall, error) {
	return nil, nil
}
