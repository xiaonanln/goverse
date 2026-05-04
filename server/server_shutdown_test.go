package server

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

func newShutdownTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")
	listenAddr := testutil.GetFreeAddress()
	cfg := &ServerConfig{
		ListenAddress:             listenAddr,
		AdvertiseAddress:          listenAddr,
		EtcdAddress:               "localhost:2379",
		EtcdPrefix:                etcdPrefix,
		NodeStabilityDuration:     3 * time.Second,
		ShardMappingCheckInterval: 1 * time.Second,
		NumShards:                 testutil.TestNumShards,
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv, listenAddr
}

// TestServer_ContextCancel_TriggersCleanShutdown verifies that cancelling the
// Run context shuts the server down cleanly within a bounded timeout.
// This is the same code path as SIGTERM (ctx.Done case in the select).
func TestServer_ContextCancel_TriggersCleanShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	srv, _ := newShutdownTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(2 * time.Second)

	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error from Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down within 15s after context cancel")
	}
}

// TestServer_SIGTERM_TriggersCleanShutdown verifies that SIGTERM causes
// server.Run to return cleanly. Do not run in parallel — sends a real signal
// to the process.
func TestServer_SIGTERM_TriggersCleanShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	srv, _ := newShutdownTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(2 * time.Second)

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("failed to send SIGTERM: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error from Run: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down within 15s after SIGTERM")
	}
}

// TestServer_ShutdownOrder_NodeBeforeCluster verifies the critical shutdown
// invariant: node.Stop() (Postgres save) must complete before cluster.Stop()
// (etcd lease revocation). If violated, other nodes claiming our shards would
// load stale object state.
func TestServer_ShutdownOrder_NodeBeforeCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	srv, _ := newShutdownTestServer(t)

	var mu sync.Mutex
	var order []string
	srv.testHookAfterNodeStop = func() {
		mu.Lock()
		order = append(order, "node")
		mu.Unlock()
	}
	srv.testHookAfterClusterStop = func() {
		mu.Lock()
		order = append(order, "cluster")
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	time.Sleep(2 * time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("server did not shut down within 15s")
	}

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("expected 2 shutdown events, got %d: %v", len(got), got)
	}
	if got[0] != "node" || got[1] != "cluster" {
		t.Errorf("shutdown order = %v; want [node cluster]", got)
	}
}
