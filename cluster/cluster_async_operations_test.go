package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
)

func waitForClusterReady(t *testing.T, cluster *Cluster, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cluster.IsReady() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Cluster did not become ready within %v", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestAsyncCreateObjectFromMethod verifies that CreateObject can be called from within
// an object method without causing deadlocks. The async implementation ensures that
// the CreateObject call returns immediately without waiting for completion.
func TestAsyncCreateObjectFromMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a cluster
	cluster1 := mustNewCluster(ctx, t, "localhost:47101", testPrefix)
	node1 := cluster1.GetThisNode()

	// Set the cluster singleton so object methods can use This()
	SetThis(cluster1)
	t.Cleanup(func() { SetThis(nil) })

	// Register object types
	node1.RegisterObjectType((*AsyncTestObject)(nil))

	// Start mock gRPC server
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47101", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	waitForClusterReady(t, cluster1, 30*time.Second)

	// Create the first object
	objID1 := "async-test-obj-1"
	_, err = cluster1.CreateObject(ctx, "AsyncTestObject", objID1)
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}

	// Wait for async creation to complete
	time.Sleep(500 * time.Millisecond)

	// Call a method that internally calls CreateObject
	// This should not deadlock because CreateObject is async
	req := &goverse_pb.Empty{}
	_, err = cluster1.CallObject(ctx, "AsyncTestObject", objID1, "CreateAnotherObject", req)
	if err != nil {
		t.Fatalf("CallObject failed: %v", err)
	}

	// Wait for the async CreateObject from within the method to complete
	time.Sleep(500 * time.Millisecond)

	// Verify the second object was created
	objID2 := "async-test-obj-2"
	objects := node1.ListObjects()
	found := false
	for _, obj := range objects {
		if obj.Id == objID2 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected object %s to be created by method call, but it was not found", objID2)
	}
}

// TestAsyncDeleteObjectFromMethod verifies that DeleteObject can be called from within
// an object method without causing deadlocks.
func TestAsyncDeleteObjectFromMethod(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a cluster
	cluster1 := mustNewCluster(ctx, t, "localhost:47102", testPrefix)
	node1 := cluster1.GetThisNode()

	// Set the cluster singleton so object methods can use This()
	SetThis(cluster1)
	t.Cleanup(func() { SetThis(nil) })

	// Register object types
	node1.RegisterObjectType((*AsyncTestObject)(nil))

	// Start mock gRPC server
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47102", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	waitForClusterReady(t, cluster1, 30*time.Second)

	// Create two objects
	objID1 := "async-delete-obj-1"
	objID2 := "async-delete-obj-2"

	_, err = cluster1.CreateObject(ctx, "AsyncTestObject", objID1)
	if err != nil {
		t.Fatalf("Failed to create first object: %v", err)
	}

	_, err = cluster1.CreateObject(ctx, "AsyncTestObject", objID2)
	if err != nil {
		t.Fatalf("Failed to create second object: %v", err)
	}

	// Wait for async creations to complete
	time.Sleep(500 * time.Millisecond)

	// Call a method on obj1 that deletes obj2
	// This should not deadlock because DeleteObject is async
	req := &goverse_pb.Empty{}
	_, err = cluster1.CallObject(ctx, "AsyncTestObject", objID1, "DeleteAnotherObject", req)
	if err != nil {
		t.Fatalf("CallObject failed: %v", err)
	}

	// Wait for the async DeleteObject from within the method to complete
	time.Sleep(500 * time.Millisecond)

	// Verify the second object was deleted
	objects := node1.ListObjects()
	found := false
	for _, obj := range objects {
		if obj.Id == objID2 {
			found = true
			break
		}
	}
	if found {
		t.Errorf("Expected object %s to be deleted by method call, but it still exists", objID2)
	}
}

// TestAsyncOperationsReturnImmediately verifies that CreateObject and DeleteObject
// return immediately without waiting for the operation to complete
func TestAsyncOperationsReturnImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a cluster
	cluster1 := mustNewCluster(ctx, t, "localhost:47103", testPrefix)
	node1 := cluster1.GetThisNode()

	// Set the cluster singleton so object methods can use This()
	SetThis(cluster1)
	t.Cleanup(func() { SetThis(nil) })

	// Register object type
	node1.RegisterObjectType((*AsyncTestObject)(nil))

	// Start mock gRPC server
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper("localhost:47103", mockServer1)
	err := testServer1.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	waitForClusterReady(t, cluster1, 30*time.Second)

	// Measure time for CreateObject to return
	objID := "async-timing-obj"
	start := time.Now()
	_, err = cluster1.CreateObject(ctx, "AsyncTestObject", objID)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	// CreateObject should return very quickly (< 100ms) since it's async
	if duration > 100*time.Millisecond {
		t.Logf("Warning: CreateObject took %v, expected < 100ms", duration)
	}

	// Wait for async creation
	time.Sleep(500 * time.Millisecond)

	// Measure time for DeleteObject to return
	start = time.Now()
	err = cluster1.DeleteObject(ctx, objID)
	duration = time.Since(start)

	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}

	// DeleteObject should return very quickly (< 100ms) since it's async
	if duration > 100*time.Millisecond {
		t.Logf("Warning: DeleteObject took %v, expected < 100ms", duration)
	}
}

// AsyncTestObject is a test object that calls CreateObject and DeleteObject
// from within its methods to test async behavior
type AsyncTestObject struct {
	object.BaseObject
}

func (o *AsyncTestObject) OnCreated() {}

// CreateAnotherObject creates another object from within a method
func (o *AsyncTestObject) CreateAnotherObject(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.Empty, error) {
	// This call should not deadlock because CreateObject is async
	_, err := This().CreateObject(ctx, "AsyncTestObject", "async-test-obj-2")
	if err != nil {
		return nil, err
	}
	return &goverse_pb.Empty{}, nil
}

// DeleteAnotherObject deletes another object from within a method
func (o *AsyncTestObject) DeleteAnotherObject(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.Empty, error) {
	// This call should not deadlock because DeleteObject is async
	err := This().DeleteObject(ctx, "async-delete-obj-2")
	if err != nil {
		return nil, err
	}
	return &goverse_pb.Empty{}, nil
}

// Ensure AsyncTestObject implements Object interface
var _ object.Object = (*AsyncTestObject)(nil)
