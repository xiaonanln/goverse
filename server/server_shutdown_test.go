package server

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
)

func newShutdownTestServer(t *testing.T) *Server {
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
	return srv
}

// TestServer_ContextCancel_TriggersCleanShutdown verifies that cancelling the
// Run context (the same code path as SIGTERM via ctx.Done) shuts the server
// down cleanly within a bounded timeout.
func TestServer_ContextCancel_TriggersCleanShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	srv := newShutdownTestServer(t)

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

// TestServer_SIGTERM_TriggersCleanShutdown verifies that a real SIGTERM is
// caught by signal.Notify and causes server.Run to return cleanly.
// Must not run in parallel — sends a real signal to the process.
func TestServer_SIGTERM_TriggersCleanShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	srv := newShutdownTestServer(t)

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
