package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
)

// MockPersistenceProviderWithReliableCalls extends MockPersistenceProvider to support reliable calls
type MockPersistenceProviderWithReliableCalls struct {
	*MockPersistenceProvider
	mu                sync.Mutex
	reliableCalls     map[string]*object.ReliableCall
	seq               int64
	getPendingCalled  int
	getPendingDelayMs int // Optional delay to simulate slow DB query
}

func NewMockPersistenceProviderWithReliableCalls() *MockPersistenceProviderWithReliableCalls {
	return &MockPersistenceProviderWithReliableCalls{
		MockPersistenceProvider: NewMockPersistenceProvider(),
		reliableCalls:           make(map[string]*object.ReliableCall),
	}
}

func (m *MockPersistenceProviderWithReliableCalls) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*object.ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rc, exists := m.reliableCalls[requestID]; exists {
		return rc, nil
	}

	m.seq++
	rc := &object.ReliableCall{
		Seq:         m.seq,
		CallID:      requestID,
		ObjectID:    objectID,
		ObjectType:  objectType,
		MethodName:  methodName,
		RequestData: requestData,
		Status:      "pending",
	}
	m.reliableCalls[requestID] = rc
	return rc, nil
}

func (m *MockPersistenceProviderWithReliableCalls) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, rc := range m.reliableCalls {
		if rc.Seq == seq {
			rc.Status = status
			rc.ResultData = resultData
			rc.Error = errorMessage
			break
		}
	}
	return nil
}

func (m *MockPersistenceProviderWithReliableCalls) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	m.mu.Lock()
	m.getPendingCalled++
	delay := m.getPendingDelayMs
	m.mu.Unlock()

	// Simulate slow DB query if configured
	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var pending []*object.ReliableCall
	for _, rc := range m.reliableCalls {
		if rc.ObjectID == objectID && rc.Status == "pending" && rc.Seq >= nextRcseq {
			pending = append(pending, rc)
		}
	}
	return pending, nil
}

func (m *MockPersistenceProviderWithReliableCalls) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rc, exists := m.reliableCalls[requestID]; exists {
		return rc, nil
	}
	return nil, object.ErrObjectNotFound
}

func (m *MockPersistenceProviderWithReliableCalls) GetPendingCalledCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getPendingCalled
}

// TestNode_ProcessPendingReliableCalls_SingleGoroutine verifies that multiple calls
// to processPendingReliableCalls for the same object reuse the same goroutine/channel
func TestNode_ProcessPendingReliableCalls_SingleGoroutine(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)
	provider := NewMockPersistenceProviderWithReliableCalls()
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*TestObject)(nil))

	objectType := "TestObject"
	objectID := "test-obj-1"

	// Create the object first
	_, err := node.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Add a delay to GetPendingReliableCalls to ensure the goroutine is still running
	// when we make the second call
	provider.getPendingDelayMs = 100

	// Make first call to processPendingReliableCalls
	chan1 := node.processPendingReliableCalls(ctx, objectType, objectID)

	// Small delay to ensure the goroutine starts
	time.Sleep(10 * time.Millisecond)

	// Make second call with same object - should return the same channel
	chan2 := node.processPendingReliableCalls(ctx, objectType, objectID)

	// Verify that both calls returned the same channel
	if chan1 != chan2 {
		t.Errorf("Expected same channel for same object, got different channels")
	}

	// Drain the channels
	for range chan1 {
		// Consume all items
	}

	// Verify that GetPendingReliableCalls was only called once (not twice)
	calledCount := provider.GetPendingCalledCount()
	if calledCount != 1 {
		t.Errorf("GetPendingReliableCalls called %d times, expected 1", calledCount)
	}
}

// TestNode_ProcessPendingReliableCalls_DifferentObjects verifies that calls
// for different objects create different goroutines/channels
func TestNode_ProcessPendingReliableCalls_DifferentObjects(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)
	provider := NewMockPersistenceProviderWithReliableCalls()
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*TestObject)(nil))

	objectType := "TestObject"
	objectID1 := "test-obj-1"
	objectID2 := "test-obj-2"

	// Create both objects
	_, err := node.CreateObject(ctx, objectType, objectID1)
	if err != nil {
		t.Fatalf("Failed to create object 1: %v", err)
	}
	_, err = node.CreateObject(ctx, objectType, objectID2)
	if err != nil {
		t.Fatalf("Failed to create object 2: %v", err)
	}

	// Make calls for different objects
	chan1 := node.processPendingReliableCalls(ctx, objectType, objectID1)
	chan2 := node.processPendingReliableCalls(ctx, objectType, objectID2)

	// Verify that different objects get different channels
	if chan1 == chan2 {
		t.Errorf("Expected different channels for different objects, got same channel")
	}

	// Drain both channels
	go func() {
		for range chan1 {
		}
	}()
	for range chan2 {
	}
}

// TestNode_ProcessPendingReliableCalls_SequentialCalls verifies that after a goroutine
// completes, a new call creates a new goroutine
func TestNode_ProcessPendingReliableCalls_SequentialCalls(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)
	provider := NewMockPersistenceProviderWithReliableCalls()
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*TestObject)(nil))

	objectType := "TestObject"
	objectID := "test-obj-1"

	// Create the object
	_, err := node.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Make first call and wait for it to complete
	chan1 := node.processPendingReliableCalls(ctx, objectType, objectID)
	for range chan1 {
		// Drain the channel
	}

	// Verify the channel is closed
	_, ok := <-chan1
	if ok {
		t.Error("Expected channel to be closed")
	}

	// Make second call after first completes - should get a new channel
	chan2 := node.processPendingReliableCalls(ctx, objectType, objectID)

	// Verify we got a different channel
	if chan1 == chan2 {
		t.Errorf("Expected new channel after first completed, got same channel")
	}

	// Drain the second channel
	for range chan2 {
	}

	// Verify GetPendingReliableCalls was called twice (once for each goroutine)
	calledCount := provider.GetPendingCalledCount()
	if calledCount != 2 {
		t.Errorf("GetPendingReliableCalls called %d times, expected 2", calledCount)
	}
}

// TestNode_ProcessPendingReliableCalls_ConcurrentCallsSameObject verifies that
// when multiple goroutines call processPendingReliableCalls concurrently for the same object,
// they all get the same channel and only one processing goroutine is created
func TestNode_ProcessPendingReliableCalls_ConcurrentCallsSameObject(t *testing.T) {
	ctx := context.Background()
	node := NewNode("localhost:47000", testNumShards)
	provider := NewMockPersistenceProviderWithReliableCalls()
	node.SetPersistenceProvider(provider)

	// Register object type
	node.RegisterObjectType((*TestObject)(nil))

	objectType := "TestObject"
	objectID := "test-obj-1"

	// Create the object
	_, err := node.CreateObject(ctx, objectType, objectID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Add delay to ensure goroutine is running when concurrent calls are made
	provider.getPendingDelayMs = 100

	// Launch multiple concurrent calls
	numCalls := 10
	channels := make([]<-chan *object.ReliableCall, numCalls)
	var wg sync.WaitGroup
	wg.Add(numCalls)

	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			defer wg.Done()
			channels[idx] = node.processPendingReliableCalls(ctx, objectType, objectID)
		}(i)
	}

	wg.Wait()

	// Verify all calls returned the same channel
	firstChan := channels[0]
	for i := 1; i < numCalls; i++ {
		if channels[i] != firstChan {
			t.Errorf("Call %d returned different channel", i)
		}
	}

	// Drain the channel
	for range firstChan {
	}

	// Verify GetPendingReliableCalls was only called once
	calledCount := provider.GetPendingCalledCount()
	if calledCount != 1 {
		t.Errorf("GetPendingReliableCalls called %d times, expected 1", calledCount)
	}
}
