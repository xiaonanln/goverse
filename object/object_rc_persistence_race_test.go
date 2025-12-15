package object

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/protohelper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestReliableCallPersistenceRaceCondition tests that ToDataWithSeq prevents
// the race condition between RC execution and persistence.
//
// Without the fix, the following race could occur:
// 1. Save goroutine calls ToData() -> gets state at seq=5
// 2. RC goroutine executes call_seq=5, mutates state, updates nextRcseq=6
// 3. Save goroutine calls GetNextRcseq() -> gets 6
// 4. Save goroutine calls SaveObject(state_at_seq_5, nextRcseq=6) -> INCONSISTENT!
//
// With the fix using ToDataWithSeq and stateMu:
// - RC execution holds stateMu.Lock() -> serializes state mutation + nextRcseq update
// - ToDataWithSeq holds stateMu.RLock() -> gets atomic snapshot
// - No race condition possible
func TestReliableCallPersistenceRaceCondition(t *testing.T) {
	obj := &testPersistentObject{}
	obj.OnInit(obj, "race-test")
	obj.SetValue(0)
	obj.SetNextRcseq(0)

	var wg sync.WaitGroup
	const numRCs = 100
	const numSaves = 100

	// Simulate RC execution goroutines
	for i := 0; i < numRCs; i++ {
		wg.Add(1)
		go func(seq int64) {
			defer wg.Done()

			// Simulate RC execution (what processReliableCall does)
			obj.stateMu.Lock()
			obj.SetValue(int32(seq))
			obj.SetNextRcseq(seq + 1)
			obj.stateMu.Unlock()

			// Small delay to increase chance of race
			time.Sleep(1 * time.Microsecond)
		}(int64(i))
	}

	// Simulate persistence goroutines calling ToDataWithSeq
	inconsistencies := 0
	var inconsistencyMu sync.Mutex
	for i := 0; i < numSaves; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Use ToDataWithSeq (the fix)
			data, nextRcseq, err := obj.ToDataWithSeq()
			if err != nil {
				t.Errorf("ToDataWithSeq failed: %v", err)
				return
			}

			// Verify consistency invariant:
			// If nextRcseq = N, then data must reflect state after seq=N-1
			msg, ok := data.(*wrapperspb.Int32Value)
			if !ok {
				t.Errorf("Expected *wrapperspb.Int32Value, got %T", data)
				return
			}

			expectedValue := nextRcseq - 1
			if msg.Value != int32(expectedValue) {
				inconsistencyMu.Lock()
				inconsistencies++
				inconsistencyMu.Unlock()
			}

			// Small delay to increase chance of race
			time.Sleep(1 * time.Microsecond)
		}()
	}

	wg.Wait()

	if inconsistencies > 0 {
		t.Errorf("Found %d inconsistent (data, nextRcseq) pairs - the fix is not working!", inconsistencies)
	}
}

// TestProcessReliableCall_UpdatesNextRcseqUnderLock tests that processReliableCall
// correctly updates nextRcseq inside the stateMu lock
func TestProcessReliableCall_UpdatesNextRcseqUnderLock(t *testing.T) {
	obj := &testRCObject{}
	obj.OnInit(obj, "rc-lock-test")
	obj.value = 0
	obj.SetNextRcseq(5)

	provider := &mockRCProvider{
		storage: make(map[int64]*ReliableCall),
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

	// Now verify ToDataWithSeq sees consistent state
	data, nextRcseq, err := obj.ToDataWithSeq()
	if err != nil {
		t.Fatalf("ToDataWithSeq failed: %v", err)
	}

	if nextRcseq != 6 {
		t.Errorf("Expected nextRcseq=6, got %d", nextRcseq)
	}

	msg, ok := data.(*wrapperspb.Int32Value)
	if !ok {
		t.Fatalf("Expected *wrapperspb.Int32Value, got %T", data)
	}

	if msg.Value != 100 {
		t.Errorf("Expected value=100, got %d", msg.Value)
	}
}

// testRCObject is a test object that supports RC methods
type testRCObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testRCObject) OnCreated() {}

func (obj *testRCObject) ToData() (proto.Message, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testRCObject) FromData(data proto.Message) error {
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

func (obj *testRCObject) SetValue(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	obj.value = req.Value
	return wrapperspb.Int32(obj.value), nil
}

// mockRCProvider is a minimal mock for testing RC processing
type mockRCProvider struct {
	storage map[int64]*ReliableCall
	mu      sync.Mutex
}

func (m *mockRCProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	return nil
}

func (m *mockRCProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return nil, 0, nil
}

func (m *mockRCProvider) DeleteObject(ctx context.Context, objectID string) error {
	return nil
}

func (m *mockRCProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*ReliableCall, error) {
	return nil, nil
}

func (m *mockRCProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		call.Status = status
		call.ResultData = resultData
		call.Error = errorMessage
	}
	return nil
}

func (m *mockRCProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*ReliableCall, error) {
	return nil, nil
}

func (m *mockRCProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if call, ok := m.storage[seq]; ok {
		return call, nil
	}
	return nil, nil
}
