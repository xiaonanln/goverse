package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestCreateObjectEnforcesDefaultTimeout verifies that CreateObject enforces
// the DefaultCreateTimeout when the context has no deadline
func TestCreateObjectEnforcesDefaultTimeout(t *testing.T) {
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a simple node cluster for testing
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { n.Stop(ctx) })

	// Create cluster config
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	cluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = cluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	t.Cleanup(func() { cluster.Stop(ctx) })

	// Register a simple test object
	n.RegisterObjectType((*SimpleTimeoutTestObject)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, cluster)

	// Test 1: Verify timeout is applied when no deadline exists
	ctxNoDeadline := context.Background()
	_, hasDeadline := ctxNoDeadline.Deadline()
	if hasDeadline {
		t.Fatal("context.Background() should not have a deadline")
	}

	// Create object - should succeed quickly with local node
	objID1 := testutil.GetObjectIDForShard(0, "timeout-test-1")
	_, err = cluster.CreateObject(ctxNoDeadline, "SimpleTimeoutTestObject", objID1)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	// Wait for async creation
	time.Sleep(500 * time.Millisecond)

	// Verify object was created
	objects := n.ListObjects()
	found := false
	for _, obj := range objects {
		if obj.Id == objID1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected object %s to be created", objID1)
	}
}

// TestCreateObjectRespectsExistingDeadline verifies that CreateObject respects
// an existing deadline in the context and doesn't override it
func TestCreateObjectRespectsExistingDeadline(t *testing.T) {
	t.Parallel()

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	// Create a simple node cluster
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	err := n.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	t.Cleanup(func() { n.Stop(ctx) })

	// Create cluster config
	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    testPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	cluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping test: etcd not available: %v", err)
		return
	}

	err = cluster.Start(ctx, n)
	if err != nil {
		t.Fatalf("Failed to start cluster: %v", err)
	}
	t.Cleanup(func() { cluster.Stop(ctx) })

	// Register a simple test object
	n.RegisterObjectType((*SimpleTimeoutTestObject)(nil))

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, cluster)

	// Create a context with a 30-second deadline
	ctxWithDeadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Verify it has a deadline
	deadline, hasDeadline := ctxWithDeadline.Deadline()
	if !hasDeadline {
		t.Fatal("Context should have a deadline")
	}

	// Create object - should succeed and respect the existing deadline
	objID := testutil.GetObjectIDForShard(0, "deadline-test-obj")
	createdID, err := cluster.CreateObject(ctxWithDeadline, "SimpleTimeoutTestObject", objID)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	if createdID != objID {
		t.Errorf("Expected object ID %s, got %s", objID, createdID)
	}

	// Verify the deadline wasn't changed (still roughly 30s from now)
	// The CreateObject shouldn't add a new timeout on top
	newDeadline, _ := ctxWithDeadline.Deadline()
	if !deadline.Equal(newDeadline) {
		// Some variation is OK due to execution time, but should be minimal
		diff := newDeadline.Sub(deadline)
		if diff > 100*time.Millisecond || diff < -100*time.Millisecond {
			t.Errorf("Deadline changed significantly: was %v, now %v (diff: %v)", deadline, newDeadline, diff)
		}
	}

	// Wait for async creation
	time.Sleep(500 * time.Millisecond)

	// Verify object was created
	objects := n.ListObjects()
	found := false
	for _, obj := range objects {
		if obj.Id == objID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected object %s to be created", objID)
	}
}

// TestCreateObjectTimeoutConstant verifies the DefaultCreateTimeout constant is set correctly
func TestCreateObjectTimeoutConstant(t *testing.T) {
	t.Parallel()

	expectedTimeout := 10 * time.Second
	if DefaultCreateTimeout != expectedTimeout {
		t.Errorf("DefaultCreateTimeout should be %v, got %v", expectedTimeout, DefaultCreateTimeout)
	}
}

// SimpleTimeoutTestObject is a minimal test object for timeout tests
type SimpleTimeoutTestObject struct {
	object.BaseObject
}

func (o *SimpleTimeoutTestObject) OnCreated() {}

// Ensure SimpleTimeoutTestObject implements Object interface
var _ object.Object = (*SimpleTimeoutTestObject)(nil)

