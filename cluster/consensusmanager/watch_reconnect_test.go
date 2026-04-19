package consensusmanager

import (
	"context"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/xiaonanln/goverse/cluster/etcdmanager"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/testutil"
	"github.com/xiaonanln/goverse/util/uniqueid"
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

// TestWatchPrefixOnceRecordsReconnectMetric verifies that watchPrefixOnce
// records the "reconnected" metric exactly when isReconnect=true and a
// healthy watch response arrives.
func TestWatchPrefixOnceRecordsReconnectMetric(t *testing.T) {
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

	// Unique localNodeAddress so the metric label is isolated per test run.
	localAddr := "test-reconnect-metric-" + uniqueid.UniqueId()
	cm := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, localAddr, testNumShards)

	ctx := context.Background()
	if err := cm.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Run watchPrefixOnce in a goroutine with isReconnect=true. We write a key
	// to force etcd to emit a watch event, which triggers the first-healthy-
	// response path in watchPrefixOnce that bumps the metric.
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	done := make(chan struct{})
	go func() {
		_ = cm.watchPrefixOnce(watchCtx, testPrefix, true)
		close(done)
	}()

	// Give the watch time to open, then write a key to trigger an event.
	time.Sleep(200 * time.Millisecond)
	client := mgr.GetClient()
	if _, err := client.Put(ctx, testPrefix+"/nodes/test-metric-node", "test-metric-addr:1"); err != nil {
		t.Fatalf("Failed to put node key: %v", err)
	}

	// Wait for the event to be processed so the metric is bumped.
	time.Sleep(200 * time.Millisecond)
	watchCancel()
	<-done

	got := promtestutil.ToFloat64(metrics.WatchReconnectionsTotal.WithLabelValues(localAddr, "reconnected"))
	if got != 1 {
		t.Fatalf("Expected 1 reconnected metric for %s, got %v", localAddr, got)
	}

	// Sanity: with isReconnect=false, no metric should be bumped.
	localAddr2 := "test-reconnect-metric-off-" + uniqueid.UniqueId()
	cm2 := NewConsensusManager(mgr, shardlock.NewShardLock(testNumShards), 0, localAddr2, testNumShards)
	if err := cm2.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize cm2: %v", err)
	}
	watchCtx2, watchCancel2 := context.WithCancel(ctx)
	defer watchCancel2()
	done2 := make(chan struct{})
	go func() {
		_ = cm2.watchPrefixOnce(watchCtx2, testPrefix, false)
		close(done2)
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := client.Put(ctx, testPrefix+"/nodes/test-metric-node-2", "test-metric-addr:2"); err != nil {
		t.Fatalf("Failed to put node key: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	watchCancel2()
	<-done2

	got2 := promtestutil.ToFloat64(metrics.WatchReconnectionsTotal.WithLabelValues(localAddr2, "reconnected"))
	if got2 != 0 {
		t.Fatalf("Expected 0 reconnected metric for %s (isReconnect=false), got %v", localAddr2, got2)
	}
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

	result := cm.watchPrefixOnce(watchCtx, testPrefix, false)
	if result != nil {
		t.Fatalf("Expected nil (clean shutdown) but got error: %v", result)
	}
}
