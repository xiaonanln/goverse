package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/anypb"
)

// SlowTestObject is a test object that has a method that takes a long time to execute
type SlowTestObject struct {
	object.BaseObject
}

func (o *SlowTestObject) OnCreated() {
	// No special initialization needed
}

// SlowMethod is a method that sleeps for a long time
func (o *SlowTestObject) SlowMethod(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.Empty, error) {
	// Sleep for longer than the default timeout
	select {
	case <-time.After(60 * time.Second):
		return &goverse_pb.Empty{}, nil
	case <-ctx.Done():
		// Respect context cancellation
		return nil, ctx.Err()
	}
}

// TestCallObject_DefaultTimeout tests that CallObject applies default timeout when context has no deadline
func TestCallObject_DefaultTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a test cluster with a node
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register the slow test object
	n.RegisterObjectType((*SlowTestObject)(nil))

	err := n.Start(ctx)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer n.Stop(ctx)

	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	cluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster.Stop(ctx)

	err = cluster.Start(ctx, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	// Create a slow object
	objID, err := cluster.CreateObject(ctx, "SlowTestObject", "")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait a bit for object creation to complete
	time.Sleep(200 * time.Millisecond)

	// Call the slow method with context.Background() (no deadline)
	// This should timeout after DefaultCallTimeout (30 seconds)
	start := time.Now()
	_, err = cluster.CallObject(ctx, "SlowTestObject", objID, "SlowMethod", &goverse_pb.Empty{})
	elapsed := time.Since(start)

	// Expect an error (timeout or context deadline exceeded)
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}

	// Verify the call timed out around the default timeout (30s)
	// Allow some tolerance (28-32 seconds)
	if elapsed < 28*time.Second || elapsed > 32*time.Second {
		t.Errorf("Expected timeout around 30 seconds, got %v", elapsed)
	}

	t.Logf("CallObject timed out after %v as expected", elapsed)
}

// TestCallObject_ExistingDeadlinePreserved tests that CallObject preserves existing context deadline
func TestCallObject_ExistingDeadlinePreserved(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a test cluster with a node
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register the slow test object
	n.RegisterObjectType((*SlowTestObject)(nil))

	err := n.Start(ctx)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer n.Stop(ctx)

	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	cluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer cluster.Stop(ctx)

	err = cluster.Start(ctx, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}

	// Wait for cluster to be ready
	testutil.WaitForClustersReady(t, cluster)

	// Create a slow object
	objID, err := cluster.CreateObject(ctx, "SlowTestObject", "")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait a bit for object creation to complete
	time.Sleep(200 * time.Millisecond)

	// Create a context with a custom deadline (2 seconds)
	customCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Call the slow method with custom deadline
	// This should timeout after 2 seconds (not the default 30 seconds)
	start := time.Now()
	_, err = cluster.CallObject(customCtx, "SlowTestObject", objID, "SlowMethod", &goverse_pb.Empty{})
	elapsed := time.Since(start)

	// Expect an error (timeout or context deadline exceeded)
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}

	// Verify the call timed out around the custom deadline (2s)
	// Allow some tolerance (1.8-2.5 seconds)
	if elapsed < 1800*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Errorf("Expected timeout around 2 seconds, got %v", elapsed)
	}

	t.Logf("CallObject timed out after %v as expected (custom deadline preserved)", elapsed)
}

// TestCallObjectAnyRequest_DefaultTimeout tests that CallObjectAnyRequest applies default timeout
func TestCallObjectAnyRequest_DefaultTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node
	nodeAddr := testutil.GetFreeAddress()
	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	// Register the slow test object
	n.RegisterObjectType((*SlowTestObject)(nil))

	err := n.Start(ctx)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer n.Stop(ctx)

	cfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	nodeCluster, err := NewClusterWithNode(cfg, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}
	defer nodeCluster.Stop(ctx)

	err = nodeCluster.Start(ctx, n)
	if err != nil {
		t.Skipf("Skipping - etcd unavailable: %v", err)
		return
	}

	// Create and start a gate
	gateAddr := testutil.GetFreeAddress()
	gateConfig := &gate.GateConfig{
		AdvertiseAddress: gateAddr,
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       etcdPrefix,
	}
	gw, err := gate.NewGate(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	defer gw.Stop()

	err = gw.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start gate: %v", err)
	}

	gateClusterCfg := Config{
		EtcdAddress:                   "localhost:2379",
		EtcdPrefix:                    etcdPrefix,
		MinQuorum:                     1,
		ClusterStateStabilityDuration: 50 * time.Millisecond,
		ShardMappingCheckInterval:     100 * time.Millisecond,
		NumShards:                     testutil.TestNumShards,
	}

	gateCluster, err := NewClusterWithGate(gateClusterCfg, gw)
	if err != nil {
		t.Fatalf("Failed to create gate cluster: %v", err)
	}
	defer gateCluster.Stop(ctx)

	err = gateCluster.Start(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to start gate cluster: %v", err)
	}

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, nodeCluster, gateCluster)

	// Create a slow object via the node cluster
	objID, err := nodeCluster.CreateObject(ctx, "SlowTestObject", "")
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Wait a bit for object creation to complete
	time.Sleep(200 * time.Millisecond)

	// Prepare request as Any
	req := &goverse_pb.Empty{}
	reqAny, err := anypb.New(req)
	if err != nil {
		t.Fatalf("Failed to marshal request to Any: %v", err)
	}

	// Call the slow method via gate using CallObjectAnyRequest
	start := time.Now()
	_, err = gateCluster.CallObjectAnyRequest(ctx, "SlowTestObject", objID, "SlowMethod", reqAny)
	elapsed := time.Since(start)

	// Expect an error (timeout or context deadline exceeded)
	if err == nil {
		t.Fatalf("Expected timeout error, got nil")
	}

	// Verify the call timed out around the default timeout (30s)
	// Allow some tolerance (28-32 seconds)
	if elapsed < 28*time.Second || elapsed > 32*time.Second {
		t.Errorf("Expected timeout around 30 seconds, got %v", elapsed)
	}

	t.Logf("CallObjectAnyRequest timed out after %v as expected", elapsed)
}

// TestCallObject_ConstantValue tests that DefaultCallTimeout constant has the correct value
func TestCallObject_ConstantValue(t *testing.T) {
	expectedTimeout := 30 * time.Second
	if DefaultCallTimeout != expectedTimeout {
		t.Errorf("Expected DefaultCallTimeout to be %v, got %v", expectedTimeout, DefaultCallTimeout)
	}
}

// TestCallObject_NoDeadlineAppliesDefault tests the deadline application logic
func TestCallObject_NoDeadlineAppliesDefault(t *testing.T) {
	// Test with context.Background() (no deadline)
	ctx := context.Background()
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Fatal("Background context should not have a deadline")
	}

	// Apply timeout (similar to what CallObject does)
	ctxWithTimeout, cancel := context.WithTimeout(ctx, DefaultCallTimeout)
	defer cancel()

	// Verify deadline was set
	deadline, hasDeadline := ctxWithTimeout.Deadline()
	if !hasDeadline {
		t.Fatal("Context should have a deadline after WithTimeout")
	}

	// Verify deadline is approximately correct (within 100ms)
	expectedDeadline := time.Now().Add(DefaultCallTimeout)
	diff := deadline.Sub(expectedDeadline)
	if diff < -100*time.Millisecond || diff > 100*time.Millisecond {
		t.Errorf("Deadline is not close to expected time. Diff: %v", diff)
	}
}

// TestCallObject_ExistingDeadlineNotModified tests that existing deadlines are preserved
func TestCallObject_ExistingDeadlineNotModified(t *testing.T) {
	// Create context with existing deadline
	existingTimeout := 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), existingTimeout)
	defer cancel()

	existingDeadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Fatal("Context should have a deadline")
	}

	// Check if context has deadline (what CallObject does)
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		// Don't apply new timeout - verify the check works
		newDeadline, stillHasDeadline := ctx.Deadline()
		if !stillHasDeadline {
			t.Fatal("Deadline should still exist")
		}
		if !newDeadline.Equal(existingDeadline) {
			t.Error("Deadline should not have changed")
		}
	}
}
