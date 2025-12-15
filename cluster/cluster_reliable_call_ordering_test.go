package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/xiaonanln/goverse/object"
	counter_pb "github.com/xiaonanln/goverse/samples/counter/proto"
	"github.com/xiaonanln/goverse/util/postgres"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
)

// testCounterForOrdering is a simple test object for reliable call ordering tests
type testCounterForOrdering struct {
	object.BaseObject
	value int32
}

func (c *testCounterForOrdering) OnCreated() {
	c.Logger.Infof("testCounterForOrdering %s created", c.Id())
	c.value = 0
}

func (c *testCounterForOrdering) ToData() (proto.Message, error) {
	return nil, object.ErrNotPersistent
}

func (c *testCounterForOrdering) FromData(data proto.Message) error {
	return nil
}

func (c *testCounterForOrdering) Increment(ctx context.Context, req *counter_pb.IncrementRequest) (*counter_pb.CounterResponse, error) {
	c.value += req.Amount
	return &counter_pb.CounterResponse{
		Name:  c.Id(),
		Value: c.value,
	}, nil
}

// TestReliableCallObject_ConcurrentOrderingGuarantee tests that concurrent reliable calls
// from multiple caller nodes to the same object get sequential seq values due to
// per-object locking on the owner node.
func TestReliableCallObject_ConcurrentOrderingGuarantee(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database and get connection
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data from previous test runs and reset sequences
	testutil.TruncateReliableCallTables(t, db)

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create THREE clusters simulating multiple caller nodes
	nodeAddr1 := testutil.GetFreeAddress()
	nodeAddr2 := testutil.GetFreeAddress()
	nodeAddr3 := testutil.GetFreeAddress()

	cluster1 := mustNewClusterWithMinDurations(ctx, t, nodeAddr1, testPrefix)
	defer cluster1.Stop(ctx)
	cluster2 := mustNewClusterWithMinDurations(ctx, t, nodeAddr2, testPrefix)
	defer cluster2.Stop(ctx)
	cluster3 := mustNewClusterWithMinDurations(ctx, t, nodeAddr3, testPrefix)
	defer cluster3.Stop(ctx)

	// Set persistence provider on all nodes
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node3 := cluster3.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)
	node3.SetPersistenceProvider(provider)

	// Register the testCounterForOrdering object type on all nodes
	node1.RegisterObjectType((*testCounterForOrdering)(nil))
	node2.RegisterObjectType((*testCounterForOrdering)(nil))
	node3.RegisterObjectType((*testCounterForOrdering)(nil))

	// Wait for all clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2, cluster3)

	// Test object - will be hashed to one of the nodes
	objectType := "testCounterForOrdering"
	objectID := "testCounterForOrdering-ordering-test"
	methodName := "Increment"

	t.Logf("Testing concurrent reliable calls to object: %s", objectID)

	// Determine which cluster owns this object
	ownerAddr, err := cluster1.GetCurrentNodeForObject(ctx, objectID)
	if err != nil {
		t.Fatalf("Failed to determine owner node: %v", err)
	}
	t.Logf("Object %s is owned by node: %s", objectID, ownerAddr)

	// Make 30 concurrent reliable calls from all three clusters
	const numCalls = 30
	var wg sync.WaitGroup
	errors := make(chan error, numCalls)
	seqs := make(chan int64, numCalls)

	for i := 0; i < numCalls; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Rotate between clusters to simulate multiple callers
			var cluster *Cluster
			switch idx % 3 {
			case 0:
				cluster = cluster1
			case 1:
				cluster = cluster2
			case 2:
				cluster = cluster3
			}

			callID := fmt.Sprintf("concurrent-call-%d", idx)
			request := &counter_pb.IncrementRequest{Amount: 1}

			result, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
			if err != nil {
				errors <- fmt.Errorf("call %d failed: %w", idx, err)
				return
			}

			// Verify result
			response, ok := result.(*counter_pb.CounterResponse)
			if !ok {
				errors <- fmt.Errorf("call %d: expected *counter_pb.CounterResponse, got %T", idx, result)
				return
			}

			t.Logf("Call %d completed with value: %d", idx, response.Value)

			// Fetch the seq from the database
			rc, err := db.GetReliableCall(ctx, callID)
			if err != nil {
				errors <- fmt.Errorf("call %d: failed to get reliable call: %w", idx, err)
				return
			}
			seqs <- rc.Seq
		}(i)
	}

	// Wait for all calls to complete
	wg.Wait()
	close(errors)
	close(seqs)

	// Check for errors
	for err := range errors {
		t.Errorf("Error during concurrent calls: %v", err)
	}

	// Collect all seq values
	var allSeqs []int64
	for seq := range seqs {
		allSeqs = append(allSeqs, seq)
	}

	if len(allSeqs) != numCalls {
		t.Fatalf("Expected %d seq values, got %d", numCalls, len(allSeqs))
	}

	// Verify that all seq values are unique and form a contiguous sequence
	seqMap := make(map[int64]bool)
	minSeq := allSeqs[0]
	maxSeq := allSeqs[0]
	for _, seq := range allSeqs {
		if seqMap[seq] {
			t.Errorf("Duplicate seq value found: %d", seq)
		}
		seqMap[seq] = true
		if seq < minSeq {
			minSeq = seq
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}

	t.Logf("Seq values range: %d to %d", minSeq, maxSeq)

	// Verify the range is contiguous (maxSeq - minSeq + 1 should equal numCalls)
	expectedRange := maxSeq - minSeq + 1
	if expectedRange != numCalls {
		t.Errorf("Seq values are not contiguous: range %d-%d (span=%d) but expected %d unique values",
			minSeq, maxSeq, expectedRange, numCalls)
	}

	// Verify all seq values in the range exist
	for seq := minSeq; seq <= maxSeq; seq++ {
		if !seqMap[seq] {
			t.Errorf("Missing seq value in sequence: %d", seq)
		}
	}

	t.Logf("✅ All %d concurrent reliable calls received sequential, contiguous seq values", numCalls)
}

// TestReliableCallObject_LocalVsRemoteOrdering tests that both local and remote
// reliable calls to the same object get sequential seq values.
func TestReliableCallObject_LocalVsRemoteOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use PrepareEtcdPrefix for test isolation
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create test database and get connection
	db := testutil.CreateTestDatabase(t)
	if db == nil {
		return
	}
	defer db.Close()

	// Initialize schema
	ctx := context.Background()
	err := db.InitSchema(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize schema: %v", err)
	}

	// Clear all data
	testutil.TruncateReliableCallTables(t, db)

	// Create persistence provider
	provider := postgres.NewPostgresPersistenceProvider(db)

	// Create two clusters
	nodeAddr1 := testutil.GetFreeAddress()
	nodeAddr2 := testutil.GetFreeAddress()

	cluster1 := mustNewClusterWithMinDurations(ctx, t, nodeAddr1, testPrefix)
	defer cluster1.Stop(ctx)
	cluster2 := mustNewClusterWithMinDurations(ctx, t, nodeAddr2, testPrefix)
	defer cluster2.Stop(ctx)

	// Set persistence provider on both nodes
	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)

	// Register object type
	node1.RegisterObjectType((*testCounterForOrdering)(nil))
	node2.RegisterObjectType((*testCounterForOrdering)(nil))

	// Wait for clusters to be ready
	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Find an object that will be on node1
	var objectID string
	for i := 0; i < 1000; i++ {
		testID := fmt.Sprintf("testCounterForOrdering-local-remote-%d", i)
		ownerAddr, err := cluster1.GetCurrentNodeForObject(ctx, testID)
		if err != nil {
			t.Fatalf("Failed to determine owner: %v", err)
		}
		if ownerAddr == nodeAddr1 {
			objectID = testID
			break
		}
	}

	if objectID == "" {
		t.Fatalf("Failed to find an object that would be on node1")
	}

	t.Logf("Testing object %s which is on node1 (%s)", objectID, nodeAddr1)

	objectType := "testCounterForOrdering"
	methodName := "Increment"

	// Make calls alternating between local (cluster1) and remote (cluster2)
	const numCalls = 10
	var seqs []int64

	for i := 0; i < numCalls; i++ {
		callID := fmt.Sprintf("local-remote-call-%d", i)
		request := &counter_pb.IncrementRequest{Amount: 1}

		// Alternate between cluster1 (local) and cluster2 (remote)
		var cluster *Cluster
		var location string
		if i%2 == 0 {
			cluster = cluster1
			location = "LOCAL"
		} else {
			cluster = cluster2
			location = "REMOTE"
		}

		t.Logf("Making call %d from %s cluster", i, location)

		result, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, request)
		if err != nil {
			t.Fatalf("Call %d failed: %v", i, err)
		}

		// Verify result
		response, ok := result.(*counter_pb.CounterResponse)
		if !ok {
			t.Fatalf("Call %d: expected *counter_pb.CounterResponse, got %T", i, result)
		}

		t.Logf("Call %d completed with value: %d", i, response.Value)

		// Fetch the seq from the database
		rc, err := db.GetReliableCall(ctx, callID)
		if err != nil {
			t.Fatalf("Call %d: failed to get reliable call: %w", i, err)
		}
		seqs = append(seqs, rc.Seq)
		t.Logf("Call %d (%s) got seq: %d", i, location, rc.Seq)
	}

	// Verify seqs are sequential
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Errorf("Seq values not sequential: seq[%d]=%d, seq[%d]=%d (expected %d)",
				i-1, seqs[i-1], i, seqs[i], seqs[i-1]+1)
		}
	}

	t.Logf("✅ Local and remote calls both received sequential seq values: %v", seqs)
}
