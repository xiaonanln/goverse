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
	
	_, err := n.CreateObject(ctx, "TestPersistentObject", "child-object-123")
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
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create the parent object
	_, err = n.createObject(ctx, "TestObjectMethodCallingCreateReentrant", "parent-object")
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
	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer n.Stop(ctx)

	// Create the source object
	_, err = n.createObject(ctx, "TestObjectMethodCallingCallReentrant", "source-object")
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
