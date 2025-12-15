package object

import (
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// testPersistentObject is a test object that supports persistence
type testPersistentObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testPersistentObject) OnCreated() {}

func (obj *testPersistentObject) ToData() (proto.Message, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testPersistentObject) FromData(data proto.Message) error {
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

func (obj *testPersistentObject) SetValue(v int32) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	obj.value = v
}

func (obj *testPersistentObject) GetValue() int32 {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return obj.value
}

// TestBaseObject_ToDataWithSeq tests that ToDataWithSeq atomically captures state and nextRcseq
func TestBaseObject_ToDataWithSeq(t *testing.T) {
	obj := &testPersistentObject{}
	obj.OnInit(obj, "test-todata-seq")
	obj.SetValue(42)
	obj.SetNextRcseq(5)

	// Call ToDataWithSeq
	data, nextRcseq, err := obj.ToDataWithSeq()
	if err != nil {
		t.Fatalf("ToDataWithSeq failed: %v", err)
	}

	// Verify nextRcseq
	if nextRcseq != 5 {
		t.Errorf("Expected nextRcseq=5, got %d", nextRcseq)
	}

	// Verify data
	msg, ok := data.(*wrapperspb.Int32Value)
	if !ok {
		t.Fatalf("Expected *wrapperspb.Int32Value, got %T", data)
	}
	if msg.Value != 42 {
		t.Errorf("Expected value=42, got %d", msg.Value)
	}
}

// TestBaseObject_ToDataWithSeq_NonPersistent tests that ToDataWithSeq returns ErrNotPersistent
func TestBaseObject_ToDataWithSeq_NonPersistent(t *testing.T) {
	obj := &testObjectForLock{}
	obj.OnInit(obj, "test-non-persistent")

	_, _, err := obj.ToDataWithSeq()
	if err != ErrNotPersistent {
		t.Errorf("Expected ErrNotPersistent, got %v", err)
	}
}

// TestBaseObject_ToDataWithSeq_Consistency tests atomic snapshot during concurrent modifications
func TestBaseObject_ToDataWithSeq_Consistency(t *testing.T) {
	obj := &testPersistentObject{}
	obj.OnInit(obj, "test-consistency")

	// Run concurrent operations that modify state and nextRcseq
	var wg sync.WaitGroup
	const numGoroutines = 10
	const numIterations = 100

	// Writer goroutines that simulate RC execution (acquire stateMu write lock)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				// Simulate processReliableCall behavior
				obj.stateMu.Lock()
				obj.SetValue(int32(id*1000 + j))
				obj.SetNextRcseq(int64(id*1000 + j + 1))
				obj.stateMu.Unlock()
			}
		}(i)
	}

	// Reader goroutines that call ToDataWithSeq (acquire stateMu read lock)
	inconsistencies := 0
	var inconsistencyMu sync.Mutex
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				data, nextRcseq, err := obj.ToDataWithSeq()
				if err != nil {
					t.Errorf("ToDataWithSeq failed: %v", err)
					return
				}

				// Verify consistency: nextRcseq should be value + 1
				msg, ok := data.(*wrapperspb.Int32Value)
				if !ok {
					t.Errorf("Expected *wrapperspb.Int32Value, got %T", data)
					return
				}

				expectedSeq := int64(msg.Value + 1)
				if nextRcseq != expectedSeq {
					inconsistencyMu.Lock()
					inconsistencies++
					inconsistencyMu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// With proper locking, there should be NO inconsistencies
	if inconsistencies > 0 {
		t.Errorf("Found %d inconsistent (data, nextRcseq) pairs - race condition detected!", inconsistencies)
	}
}

// TestBaseObject_ToDataWithSeq_ConcurrentReads tests that multiple concurrent ToDataWithSeq calls work
func TestBaseObject_ToDataWithSeq_ConcurrentReads(t *testing.T) {
	obj := &testPersistentObject{}
	obj.OnInit(obj, "test-concurrent-reads")
	obj.SetValue(123)
	obj.SetNextRcseq(10)

	var wg sync.WaitGroup
	const numReaders = 20

	// Multiple concurrent readers should all see consistent data
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, nextRcseq, err := obj.ToDataWithSeq()
			if err != nil {
				t.Errorf("ToDataWithSeq failed: %v", err)
				return
			}

			// All readers should see the same values
			if nextRcseq != 10 {
				t.Errorf("Expected nextRcseq=10, got %d", nextRcseq)
			}

			msg, ok := data.(*wrapperspb.Int32Value)
			if !ok {
				t.Errorf("Expected *wrapperspb.Int32Value, got %T", data)
				return
			}
			if msg.Value != 123 {
				t.Errorf("Expected value=123, got %d", msg.Value)
			}
		}()
	}

	wg.Wait()
}
