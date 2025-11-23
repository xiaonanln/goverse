package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestObjectLifecycleSerialization verifies that objectsMu properly serializes
// all object lifecycle operations (create, call, delete, save)
func TestObjectLifecycleSerialization(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	node := NewNode(addr)
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()

	// Start the node
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Launch multiple concurrent operations that should be serialized
	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 4) // 4 types of operations

	// Track operations
	createCount := 0
	callCount := 0
	deleteCount := 0
	saveCount := 0
	var countMu sync.Mutex

	// Concurrent CreateObject operations
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			objID := "serial-test-obj"
			_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
			if err == nil {
				countMu.Lock()
				createCount++
				countMu.Unlock()
			}
		}(i)
	}

	// Concurrent CallObject operations
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Let some creates happen first
			objID := "serial-call-obj"
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", objID, "GetValue", &emptypb.Empty{})
			if err == nil {
				countMu.Lock()
				callCount++
				countMu.Unlock()
			}
		}(i)
	}

	// Concurrent DeleteObject operations
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			time.Sleep(20 * time.Millisecond) // Let creates finish first
			objID := "serial-delete-obj"
			// Create the object first
			node.createObject(ctx, "TestPersistentObject", objID)
			err := node.DeleteObject(ctx, objID)
			if err == nil {
				countMu.Lock()
				deleteCount++
				countMu.Unlock()
			}
		}(i)
	}

	// Concurrent SaveAllObjects operations
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			time.Sleep(15 * time.Millisecond) // Let some creates happen first
			err := node.SaveAllObjects(ctx)
			if err == nil {
				countMu.Lock()
				saveCount++
				countMu.Unlock()
			}
		}(i)
	}

	// Wait for all operations to complete
	wg.Wait()

	t.Logf("Operation counts: create=%d, call=%d, delete=%d, save=%d",
		createCount, callCount, deleteCount, saveCount)

	// Verify that operations completed successfully
	// Due to serialization, we should have consistent results
	if createCount == 0 {
		t.Fatal("Expected at least some create operations to succeed")
	}
	if callCount == 0 {
		t.Fatal("Expected at least some call operations to succeed")
	}
	if deleteCount == 0 {
		t.Fatal("Expected at least some delete operations to succeed")
	}
	if saveCount == 0 {
		t.Fatal("Expected at least some save operations to succeed")
	}
}

// TestConcurrentCreateObjectSerialization verifies that concurrent CreateObject
// calls for the same object ID are properly serialized and only one succeeds
func TestConcurrentCreateObjectSerialization(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	node := NewNode(addr)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	const numGoroutines = 100
	objID := "concurrent-create-test"
	results := make(chan error, numGoroutines)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch many concurrent creates for the same object ID
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, err := node.CreateObject(ctx, "TestPersistentObject", objID)
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	// Count successful creates
	successCount := 0
	for err := range results {
		if err == nil {
			successCount++
		}
	}

	// With proper serialization, all creates should succeed
	// (createObject returns existing object if already created)
	t.Logf("Successful creates: %d/%d", successCount, numGoroutines)
	if successCount != numGoroutines {
		t.Fatalf("Expected all creates to succeed with proper serialization, got %d/%d",
			successCount, numGoroutines)
	}

	// Wait for object to be fully created (CreateObject is async)
	waitForObjectCreated(t, node, objID, 5*time.Second)

	// Verify only one object was actually created
	if node.NumObjects() != 1 {
		t.Fatalf("Expected exactly 1 object, got %d", node.NumObjects())
	}
}

// TestSaveAllObjectsWhileCreating verifies that SaveAllObjects properly
// serializes with concurrent CreateObject operations
func TestSaveAllObjectsWhileCreating(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	node := NewNode(addr)
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObject)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	var wg sync.WaitGroup
	const numCreates = 10
	const numSaves = 10

	// Start creating objects
	wg.Add(numCreates)
	for i := 0; i < numCreates; i++ {
		go func(index int) {
			defer wg.Done()
			objID := "save-create-test"
			node.createObject(ctx, "TestPersistentObject", objID)
			time.Sleep(5 * time.Millisecond) // Small delay
		}(i)
	}

	// Concurrently save all objects
	wg.Add(numSaves)
	for i := 0; i < numSaves; i++ {
		go func() {
			defer wg.Done()
			time.Sleep(2 * time.Millisecond)
			node.SaveAllObjects(ctx)
		}()
	}

	wg.Wait()

	// Verify persistence completed without errors
	// The save count should be at least as many as successful saves
	if provider.GetSaveCount() == 0 {
		t.Fatal("Expected at least some objects to be saved")
	}
	t.Logf("Total save operations: %d", provider.GetSaveCount())
}

// TestDeleteObjectSerializesWithCallObject verifies that DeleteObject
// properly serializes with CallObject operations
func TestDeleteObjectSerializesWithCallObject(t *testing.T) {	addr := testutil.GetFreeAddress()
	

	node := NewNode(addr)
	provider := NewMockPersistenceProvider()
	node.SetPersistenceProvider(provider)
	node.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	ctx := context.Background()
	err := node.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object
	objID := "delete-call-test"
	err = node.createObject(ctx, "TestPersistentObjectWithMethod", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	var wg sync.WaitGroup
	const numCalls = 20
	const numDeletes = 5

	callSuccessCount := 0
	deleteSuccessCount := 0
	var countMu sync.Mutex

	// Concurrent CallObject operations
	wg.Add(numCalls)
	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			_, err := node.CallObject(ctx, "TestPersistentObjectWithMethod", objID, "GetValue", &emptypb.Empty{})
			if err == nil {
				countMu.Lock()
				callSuccessCount++
				countMu.Unlock()
			}
		}()
	}

	// Concurrent DeleteObject operations (all should succeed due to idempotency)
	wg.Add(numDeletes)
	for i := 0; i < numDeletes; i++ {
		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			err := node.DeleteObject(ctx, objID)
			if err == nil {
				countMu.Lock()
				deleteSuccessCount++
				countMu.Unlock()
			}
		}()
	}

	wg.Wait()

	t.Logf("Results: calls=%d, deletes=%d", callSuccessCount, deleteSuccessCount)

	// All deletes should succeed (idempotent behavior)
	if deleteSuccessCount != numDeletes {
		t.Fatalf("Expected all %d deletes to succeed due to idempotency, got %d", numDeletes, deleteSuccessCount)
	}

	// Some calls should have succeeded before delete
	if callSuccessCount == 0 {
		t.Fatal("Expected at least some calls to succeed")
	}
}
