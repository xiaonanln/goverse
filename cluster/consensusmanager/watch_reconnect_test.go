package consensusmanager

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/testutil"
)

// TestWatchReconnectsOnChannelClose verifies that the watch can be stopped
// and restarted, and that it picks up new events after restart.
func TestWatchReconnectsOnChannelClose(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	err = mgr.Connect()
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer mgr.Close()

	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	ctx := context.Background()
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Start the watch
	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to start watch: %v", err)
	}

	// Let watch establish
	time.Sleep(100 * time.Millisecond)

	// Cancel and restart watch to verify it can be stopped and restarted
	cm.StopWatch()
	time.Sleep(100 * time.Millisecond)

	// Restart — this exercises that the state is clean after stop
	err = cm.StartWatch(ctx)
	if err != nil {
		t.Fatalf("Failed to restart watch: %v", err)
	}

	// Verify watch is running by writing a key and checking it's picked up
	client := mgr.GetClient()
	nodeKey := testPrefix + "/nodes/test-reconnect-node"
	_, err = client.Put(ctx, nodeKey, "test-addr:1234")
	if err != nil {
		t.Fatalf("Failed to put node key: %v", err)
	}

	// Wait for the watch event to be processed
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cm.mu.RLock()
		_, exists := cm.state.Nodes["test-addr:1234"]
		cm.mu.RUnlock()
		if exists {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cm.mu.RLock()
	_, exists := cm.state.Nodes["test-addr:1234"]
	cm.mu.RUnlock()
	if !exists {
		t.Fatalf("Watch did not pick up node addition after restart")
	}

	cm.StopWatch()
}

// TestWatchPrefixOnceReturnsNilOnCancel tests that watchPrefixOnce
// returns nil (clean shutdown) when the context is cancelled.
func TestWatchPrefixOnceReturnsNilOnCancel(t *testing.T) {
	testPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	mgr, err := etcdmanager.NewEtcdManager("localhost:2379", testPrefix)
	if err != nil {
		t.Fatalf("Failed to create etcd manager: %v", err)
	}
	err = mgr.Connect()
	if err != nil {
		t.Skipf("etcd not available: %v", err)
		return
	}
	defer mgr.Close()

	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, "", testNumShards)

	ctx := context.Background()
	err = cm.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Set up watch context and cancel it to force watchPrefixOnce to return nil (clean shutdown)
	watchCtx, watchCancel := context.WithCancel(ctx)
	watchCancel() // cancel immediately

	result := cm.watchPrefixOnce(watchCtx, testPrefix)
	if result != nil {
		t.Fatalf("Expected nil (clean shutdown) but got error: %v", result)
	}
}
