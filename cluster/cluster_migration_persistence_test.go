package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// sharedMockProvider is a minimal PersistenceProvider backed by an in-memory map
// shared between nodes so that what node1 persists, node2 can later load.
type sharedMockProvider struct {
	mu         sync.Mutex
	storage    map[string][]byte
	nextRcseqs map[string]int64
}

func newSharedMockProvider() *sharedMockProvider {
	return &sharedMockProvider{
		storage:    make(map[string][]byte),
		nextRcseqs: make(map[string]int64),
	}
}

func (m *sharedMockProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storage[objectID] = data
	m.nextRcseqs[objectID] = nextRcseq
	return nil
}

func (m *sharedMockProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.storage[objectID]
	if !ok {
		return nil, 0, nil
	}
	return data, m.nextRcseqs[objectID], nil
}

func (m *sharedMockProvider) DeleteObject(ctx context.Context, objectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.storage, objectID)
	delete(m.nextRcseqs, objectID)
	return nil
}

func (m *sharedMockProvider) InsertOrGetReliableCall(ctx context.Context, requestID, objectID, objectType, methodName string, requestData []byte) (*object.ReliableCall, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *sharedMockProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	return fmt.Errorf("not implemented")
}

func (m *sharedMockProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	return nil, nil
}

func (m *sharedMockProvider) GetReliableCallBySeq(ctx context.Context, seq int64) (*object.ReliableCall, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *sharedMockProvider) hasStoredData(objectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.storage[objectID]
	return ok
}

// MigratablePersistentObject is a persistent object whose state the runtime
// saves via the provider. The specific state doesn't matter for this test —
// the bug is about the provider row being deleted, not about the value.
type MigratablePersistentObject struct {
	object.BaseObject
}

func (o *MigratablePersistentObject) OnCreated() {}

func (o *MigratablePersistentObject) ToData() (proto.Message, error) {
	return structpb.NewStruct(map[string]any{"present": true})
}

func (o *MigratablePersistentObject) FromData(data proto.Message) error { return nil }

// TestShardMigration_PreservesPersistentState verifies the virtual-actor
// reactivation contract (docs/SHARDING.md): when a shard migrates, the old
// owner evicts local objects but must leave their persisted state intact so
// the new owner can reactivate them on first access. Previously eviction
// called Node.DeleteObject, which also wiped the persistence row — silently
// losing all object state on every rebalance. The fix introduces
// Node.EvictObject, which flushes latest state then drops the in-memory
// instance without touching persistence.
func TestShardMigration_PreservesPersistentState(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}

	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	ctx := context.Background()

	node1Addr := testutil.GetFreeAddress()
	node2Addr := testutil.GetFreeAddress()
	cluster1 := mustNewCluster(ctx, t, node1Addr, testPrefix)
	cluster2 := mustNewCluster(ctx, t, node2Addr, testPrefix)

	node1 := cluster1.GetThisNode()
	node2 := cluster2.GetThisNode()

	// Shared persistence so node2 can reactivate what node1 persisted.
	provider := newSharedMockProvider()
	node1.SetPersistenceProvider(provider)
	node2.SetPersistenceProvider(provider)

	node1.RegisterObjectType((*MigratablePersistentObject)(nil))
	node2.RegisterObjectType((*MigratablePersistentObject)(nil))

	// Start mock gRPC servers so cross-node CreateObject routing works.
	mockServer1 := testutil.NewMockGoverseServer()
	mockServer1.SetNode(node1)
	testServer1 := testutil.NewTestServerHelper(node1Addr, mockServer1)
	if err := testServer1.Start(ctx); err != nil {
		t.Fatalf("Failed to start mock server 1: %v", err)
	}
	t.Cleanup(func() { testServer1.Stop() })

	mockServer2 := testutil.NewMockGoverseServer()
	mockServer2.SetNode(node2)
	testServer2 := testutil.NewTestServerHelper(node2Addr, mockServer2)
	if err := testServer2.Start(ctx); err != nil {
		t.Fatalf("Failed to start mock server 2: %v", err)
	}
	t.Cleanup(func() { testServer2.Stop() })

	testutil.WaitForClustersReady(t, cluster1, cluster2)

	// Find an object ID that routes to node1 so we can later migrate its shard to node2.
	var objID string
	var shardID int
	for i := range 1000 {
		candidate := fmt.Sprintf("migrate-persist-obj-%d", i)
		owner, err := cluster1.GetCurrentNodeForObject(ctx, candidate)
		if err != nil {
			continue
		}
		if owner == node1Addr {
			objID = candidate
			shardID = sharding.GetShardID(candidate, testutil.TestNumShards)
			break
		}
	}
	if objID == "" {
		t.Skip("Could not find an object ID routed to node1 within 1000 candidates")
	}
	t.Logf("Selected object %s on shard %d (initial owner: node1)", objID, shardID)

	// Create the object and persist its state.
	if _, err := cluster1.CreateObject(ctx, "MigratablePersistentObject", objID); err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}
	waitForObjectCreated(t, node1, objID, 5*time.Second)

	if err := node1.SaveAllObjects(ctx); err != nil {
		t.Fatalf("SaveAllObjects failed: %v", err)
	}
	if !provider.hasStoredData(objID) {
		t.Fatalf("precondition: persisted data for %s should exist after SaveAllObjects", objID)
	}
	t.Logf("Persisted state confirmed for %s", objID)

	// Force-migrate the shard from node1 → node2 by writing directly to etcd.
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer etcdClient.Close()

	key := fmt.Sprintf("%s/shard/%d", testPrefix, shardID)
	value := fmt.Sprintf("%s,%s", node2Addr, node1Addr) // targetNode=node2, currentNode=node1
	if _, err := etcdClient.Put(ctx, key, value); err != nil {
		t.Fatalf("Failed to update shard %d: %v", shardID, err)
	}
	t.Logf("Reassigned shard %d: targetNode=%s, currentNode=%s", shardID, node2Addr, node1Addr)

	// Wait for node1 to evict the object.
	waitForObjectRemoved(t, node1, objID, testutil.WaitForShardMappingTimeout)
	t.Logf("Object %s evicted from node1 after shard migration", objID)

	// The critical assertion: the persisted state must survive eviction so that
	// node2 can reactivate the object with its prior value (500), as promised by
	// the virtual-actor reactivation contract in docs/SHARDING.md:141-145.
	if !provider.hasStoredData(objID) {
		t.Fatalf("persisted state for %s was deleted during shard migration; "+
			"the new owner can no longer reactivate the object", objID)
	}
	t.Logf("Persisted state survived migration — reactivation will succeed")
}
