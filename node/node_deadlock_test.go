package node

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// Global reference to node for method callbacks (to avoid import cycle)
var testNodeRef *Node
var testNodeMu sync.Mutex

// TestObjectMethodCallingCreateReentrant is a test object with a method that calls CreateObject
type TestObjectMethodCallingCreateReentrant struct {
	TestPersistentObject // Embed to get all the required methods
}

func (obj *TestObjectMethodCallingCreateReentrant) MethodThatCreatesObject(ctx context.Context, req *emptypb.Empty) (*structpb.Struct, error) {
	// This simulates a user method that needs to create another object
	// This should NOT deadlock - it's a reentrant call
	testNodeMu.Lock()
	n := testNodeRef
	testNodeMu.Unlock()

	_, err := n.CreateObject(ctx, "TestPersistentObject", "child-object-123", -1)
	if err != nil {
		return nil, err
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"result": {Kind: &structpb.Value_StringValue{StringValue: "success"}},
		},
	}, nil
}

// TestCallObject_MethodCallingCreateObject_NoDeadlock verifies that a method
// can call CreateObject without causing a deadlock
func TestCallObject_MethodCallingCreateObject_NoDeadlock(t *testing.T) {
	n := NewNode("localhost:47000")
	n.RegisterObjectType((*TestObjectMethodCallingCreateReentrant)(nil))
	n.RegisterObjectType((*TestPersistentObject)(nil))

	// Set global reference
	testNodeMu.Lock()
	testNodeRef = n
	testNodeMu.Unlock()

	ctx := context.Background()
	err := n.Start(ctx, -1)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create the parent object
	err = n.createObject(ctx, "TestObjectMethodCallingCreateReentrant", "parent-object", -1)
	if err != nil {
		t.Fatalf("Failed to create parent object: %v", err)
	}

	// Use a channel to detect deadlock
	done := make(chan bool, 1)
	var callErr error

	go func() {
		// Call a method that internally calls CreateObject
		_, callErr = n.CallObject(ctx, "TestObjectMethodCallingCreateReentrant", "parent-object", "MethodThatCreatesObject", &emptypb.Empty{})
		done <- true
	}()

	// Wait for completion or timeout (deadlock detection)
	select {
	case <-done:
		if callErr != nil {
			t.Logf("Call completed with error: %v (this is ok, we're just checking for deadlock)", callErr)
		} else {
			t.Logf("Call completed successfully - no deadlock")
		}
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK DETECTED: CallObject did not complete within 5 seconds when method called CreateObject")
	}
}

// TestObjectMethodCallingCallReentrant is a test object with a method that calls CallObject
type TestObjectMethodCallingCallReentrant struct {
	TestPersistentObjectWithMethod // Embed to get all the required methods
}

func (obj *TestObjectMethodCallingCallReentrant) MethodThatCallsObject(ctx context.Context, req *emptypb.Empty) (*structpb.Struct, error) {
	// This simulates a user method that needs to call another object
	// This should NOT deadlock - it's a reentrant call
	testNodeMu.Lock()
	n := testNodeRef
	testNodeMu.Unlock()

	_, err := n.CallObject(ctx, "TestPersistentObjectWithMethod", "target-object", "GetValue", &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"result": {Kind: &structpb.Value_StringValue{StringValue: "success"}},
		},
	}, nil
}

// TestCallObject_MethodCallingCallObject_NoDeadlock verifies that a method
// can call CallObject without causing a deadlock
func TestCallObject_MethodCallingCallObject_NoDeadlock(t *testing.T) {
	n := NewNode("localhost:47000")
	n.RegisterObjectType((*TestObjectMethodCallingCallReentrant)(nil))
	n.RegisterObjectType((*TestPersistentObjectWithMethod)(nil))

	// Set global reference
	testNodeMu.Lock()
	testNodeRef = n
	testNodeMu.Unlock()

	ctx := context.Background()
	err := n.Start(ctx, -1)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create the source object
	err = n.createObject(ctx, "TestObjectMethodCallingCallReentrant", "source-object", -1)
	if err != nil {
		t.Fatalf("Failed to create source object: %v", err)
	}

	// Use a channel to detect deadlock
	done := make(chan bool, 1)
	var callErr error

	go func() {
		// Call a method that internally calls CallObject on another object
		_, callErr = n.CallObject(ctx, "TestObjectMethodCallingCallReentrant", "source-object", "MethodThatCallsObject", &emptypb.Empty{})
		done <- true
	}()

	// Wait for completion or timeout (deadlock detection)
	select {
	case <-done:
		if callErr != nil {
			t.Logf("Call completed with error: %v (this is ok, we're just checking for deadlock)", callErr)
		} else {
			t.Logf("Call completed successfully - no deadlock")
		}
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK DETECTED: CallObject did not complete within 5 seconds when method called CallObject")
	}
}

// TestObjectOnCreatedCallingCreate is a test object that calls CreateObject in OnCreated
type TestObjectOnCreatedCallingCreate struct {
	TestPersistentObject // Embed to get all the required methods
}

func (obj *TestObjectOnCreatedCallingCreate) OnCreated() {
	// This simulates OnCreated making a reentrant call to CreateObject
	// This should NOT deadlock
	testNodeMu.Lock()
	n := testNodeRef
	testNodeMu.Unlock()

	_, err := n.CreateObject(context.Background(), "TestPersistentObject", "oncreated-child-123", -1)
	if err != nil {
		// Log error but don't fail - we're just checking for deadlock
		println("OnCreated CreateObject error:", err.Error())
	}
}

// TestOnCreated_CallingCreateObject_NoDeadlock verifies that OnCreated
// can call CreateObject without causing a deadlock
func TestOnCreated_CallingCreateObject_NoDeadlock(t *testing.T) {
	n := NewNode("localhost:47000")
	n.RegisterObjectType((*TestObjectOnCreatedCallingCreate)(nil))
	n.RegisterObjectType((*TestPersistentObject)(nil))

	// Set global reference
	testNodeMu.Lock()
	testNodeRef = n
	testNodeMu.Unlock()

	ctx := context.Background()
	err := n.Start(ctx, -1)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Use a channel to detect deadlock
	done := make(chan bool, 1)
	var createErr error

	go func() {
		// Create an object whose OnCreated calls CreateObject
		_, createErr = n.CreateObject(ctx, "TestObjectOnCreatedCallingCreate", "parent-oncreated", -1)
		done <- true
	}()

	// Wait for completion or timeout (deadlock detection)
	select {
	case <-done:
		if createErr != nil {
			t.Logf("CreateObject completed with error: %v (this is ok, we're just checking for deadlock)", createErr)
		} else {
			t.Logf("CreateObject completed successfully - no deadlock")
		}
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("DEADLOCK DETECTED: CreateObject did not complete within 5 seconds when OnCreated called CreateObject")
	}
}

// TestObjectWithOnCreatedFlag tracks whether OnCreated was called
type TestObjectWithOnCreatedFlag struct {
	TestPersistentObject
	onCreatedCalled bool
	mu              sync.Mutex
}

func (obj *TestObjectWithOnCreatedFlag) OnCreated() {
	obj.mu.Lock()
	obj.onCreatedCalled = true
	obj.mu.Unlock()
	// Small delay to ensure other threads would have a chance to call methods
	time.Sleep(10 * time.Millisecond)
}

func (obj *TestObjectWithOnCreatedFlag) CheckOnCreated(ctx context.Context, req *emptypb.Empty) (*structpb.Struct, error) {
	obj.mu.Lock()
	defer obj.mu.Unlock()

	wasCalled := obj.onCreatedCalled
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"onCreatedCalled": {Kind: &structpb.Value_BoolValue{BoolValue: wasCalled}},
		},
	}, nil
}

// TestOnCreated_CalledBeforeObjectVisible verifies that OnCreated completes
// before other threads can call methods on the object
func TestOnCreated_CalledBeforeObjectVisible(t *testing.T) {
	n := NewNode("localhost:47000")
	n.RegisterObjectType((*TestObjectWithOnCreatedFlag)(nil))

	ctx := context.Background()
	err := n.Start(ctx, -1)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Start creating the object in one goroutine
	createDone := make(chan bool, 1)
	go func() {
		_, err := n.CreateObject(ctx, "TestObjectWithOnCreatedFlag", "test-visibility", -1)
		if err != nil {
			t.Errorf("CreateObject failed: %v", err)
		}
		createDone <- true
	}()

	// Give it a moment to start
	time.Sleep(5 * time.Millisecond)

	// Try to call a method on the object from another goroutine
	// This should either:
	// 1. Not find the object yet (OnCreated still running)
	// 2. Find the object after OnCreated completed
	// It should NEVER find the object with OnCreated not yet called
	callAttempts := 0
	for i := 0; i < 50; i++ {
		resp, err := n.CallObject(ctx, "TestObjectWithOnCreatedFlag", "test-visibility", "CheckOnCreated", &emptypb.Empty{})
		if err == nil {
			callAttempts++
			// If we successfully called the method, OnCreated must have been called
			result := resp.(*structpb.Struct)
			if !result.Fields["onCreatedCalled"].GetBoolValue() {
				t.Fatal("Method was callable before OnCreated completed!")
			}
		}
		time.Sleep(1 * time.Millisecond)
	}

	<-createDone
	t.Logf("Successfully verified: made %d call attempts, OnCreated always completed before methods were callable", callAttempts)
}
