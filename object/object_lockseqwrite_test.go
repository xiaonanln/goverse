package object

import (
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

// testObjectForLock is a minimal test object for testing LockSeqWrite
type testObjectForLock struct {
	BaseObject
}

func (obj *testObjectForLock) OnCreated() {}

func (obj *testObjectForLock) ToData() (proto.Message, error) {
	return nil, ErrNotPersistent
}

func (obj *testObjectForLock) FromData(data proto.Message) error {
	return nil
}

// TestBaseObject_LockSeqWrite tests that LockSeqWrite correctly serializes access
func TestBaseObject_LockSeqWrite(t *testing.T) {
	obj := &testObjectForLock{}
	obj.OnInit(obj, "test-lock")

	// Track order of goroutine execution
	var order []int
	var orderMu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			unlock := obj.LockSeqWrite()
			defer unlock()

			// Record this goroutine's execution
			orderMu.Lock()
			order = append(order, id)
			orderMu.Unlock()

			// Simulate some work
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// Verify all goroutines executed
	if len(order) != 5 {
		t.Errorf("Expected 5 goroutines to execute, got %d", len(order))
	}

	// The mutex should have serialized access (order may vary but all should complete)
	seen := make(map[int]bool)
	for _, id := range order {
		if seen[id] {
			t.Errorf("Goroutine %d executed multiple times", id)
		}
		seen[id] = true
	}
}

// TestBaseObject_LockSeqWrite_Concurrent tests concurrent access is properly serialized
func TestBaseObject_LockSeqWrite_Concurrent(t *testing.T) {
	obj := &testObjectForLock{}
	obj.OnInit(obj, "test-concurrent")

	counter := 0
	var wg sync.WaitGroup

	// Run 10 goroutines that each increment the counter 100 times
	// If the lock works correctly, all increments will be serialized
	// and the final value will be exactly 1000
	// Use -race flag to detect race conditions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				unlock := obj.LockSeqWrite()
				// The lock should protect this critical section
				// If not protected, race detector (-race flag) will fail
				localCounter := counter
				counter = localCounter + 1
				unlock()
			}
		}()
	}

	wg.Wait()

	// If locking is correct, counter should be exactly 1000
	// If not, it will be less due to lost updates
	if counter != 1000 {
		t.Errorf("Expected counter to be 1000, got %d (lock failed to serialize access)", counter)
	}
}

// TestBaseObject_LockSeqWrite_Unlock tests that unlock can be called safely
func TestBaseObject_LockSeqWrite_Unlock(t *testing.T) {
	obj := &testObjectForLock{}
	obj.OnInit(obj, "test-unlock")

	unlock := obj.LockSeqWrite()
	unlock()

	// Should be able to acquire the lock again
	unlock2 := obj.LockSeqWrite()
	unlock2()
}
