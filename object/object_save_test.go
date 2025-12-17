package object

import (
	"fmt"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestBaseObject_Save_PersistentObject tests that Save() works for persistent objects
func TestBaseObject_Save_PersistentObject(t *testing.T) {
	obj := &testSavePersistentObject{}
	obj.OnInit(obj, "save-test-1")
	obj.value = 42
	obj.SetNextRcseq(10)

	provider := &mockSaveTrackingProvider{
		storage: make(map[int64]*ReliableCall),
		saves:   make([]SaveRecord, 0),
	}

	// Call Save
	err := obj.Save(provider)
	if err != nil {
		t.Fatalf("Save() returned error: %v", err)
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
	if save.objectType != "testSavePersistentObject" {
		t.Errorf("Expected objectType=testSavePersistentObject, got %s", save.objectType)
	}
	if save.nextRcseq != 10 {
		t.Errorf("Expected nextRcseq=10, got %d", save.nextRcseq)
	}

	// Verify saved data reflects the object state
	savedMsg := &wrapperspb.Int32Value{}
	if err := UnmarshalProtoFromJSON(save.data, savedMsg); err != nil {
		t.Fatalf("Failed to unmarshal saved data: %v", err)
	}
	if savedMsg.Value != 42 {
		t.Errorf("Expected saved value=42, got %d", savedMsg.Value)
	}
}

// TestBaseObject_Save_NonPersistentObject tests that Save() returns ErrNotPersistent for non-persistent objects
func TestBaseObject_Save_NonPersistentObject(t *testing.T) {
	obj := &testSaveNonPersistentObject{}
	obj.OnInit(obj, "save-test-2")
	obj.value = 42

	provider := &mockSaveTrackingProvider{
		storage: make(map[int64]*ReliableCall),
		saves:   make([]SaveRecord, 0),
	}

	// Call Save - should return ErrNotPersistent
	err := obj.Save(provider)
	if err != ErrNotPersistent {
		t.Fatalf("Expected ErrNotPersistent, got %v", err)
	}

	// Verify SaveObject was NOT called
	if len(provider.saves) != 0 {
		t.Fatalf("Expected 0 save calls for non-persistent object, got %d", len(provider.saves))
	}
}

// TestBaseObject_Save_Concurrent tests that Save() can be called concurrently
func TestBaseObject_Save_Concurrent(t *testing.T) {
	obj := &testSavePersistentObject{}
	obj.OnInit(obj, "save-test-3")
	obj.value = 100
	obj.SetNextRcseq(5)

	provider := &mockSaveTrackingProvider{
		storage: make(map[int64]*ReliableCall),
		saves:   make([]SaveRecord, 0),
	}

	// Launch multiple concurrent Save calls
	const numGoroutines = 10
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := obj.Save(provider)
			if err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Save() returned error: %v", err)
	}

	// Verify SaveObject was called multiple times
	if len(provider.saves) != numGoroutines {
		t.Errorf("Expected %d save calls, got %d", numGoroutines, len(provider.saves))
	}

	// All saves should have the same value
	for i, save := range provider.saves {
		savedMsg := &wrapperspb.Int32Value{}
		if err := UnmarshalProtoFromJSON(save.data, savedMsg); err != nil {
			t.Fatalf("Failed to unmarshal saved data[%d]: %v", i, err)
		}
		if savedMsg.Value != 100 {
			t.Errorf("Expected saved value=100, got %d at index %d", savedMsg.Value, i)
		}
	}
}

// TestBaseObject_Save_WithSaveFailure tests that Save() returns error when save fails
func TestBaseObject_Save_WithSaveFailure(t *testing.T) {
	obj := &testSavePersistentObject{}
	obj.OnInit(obj, "save-test-4")
	obj.value = 50
	obj.SetNextRcseq(3)

	provider := &mockFailingSaveProvider{
		storage:   make(map[int64]*ReliableCall),
		saveError: fmt.Errorf("simulated save error"),
	}

	// Call Save - should return error
	err := obj.Save(provider)
	if err == nil {
		t.Fatal("Expected error from Save() when SaveObject fails, got nil")
	}
}

// testSavePersistentObject is a test object that supports persistence
type testSavePersistentObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testSavePersistentObject) OnCreated() {}

func (obj *testSavePersistentObject) ToData() (proto.Message, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()
	return wrapperspb.Int32(obj.value), nil
}

func (obj *testSavePersistentObject) FromData(data proto.Message) error {
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

// testSaveNonPersistentObject is a test object that does NOT support persistence
type testSaveNonPersistentObject struct {
	BaseObject
	mu    sync.Mutex
	value int32
}

func (obj *testSaveNonPersistentObject) OnCreated() {}

// ToData returns ErrNotPersistent (uses BaseObject's default implementation)
