package cluster

import (
	"context"
	"testing"
	"time"
)

// TestClusterLiveCtxSeededByConstructor verifies the cluster constructor path
// seeds liveCtx so fire-and-forget goroutines have a non-nil parent even when
// Start() has not been called.
func TestClusterLiveCtxSeededByConstructor(t *testing.T) {
	c := newClusterForTesting(nil, "test-livectx-seeded")
	if c.liveCtx == nil {
		t.Fatal("liveCtx should be seeded by the constructor")
	}
	if err := c.liveCtx.Err(); err != nil {
		t.Fatalf("liveCtx should not be cancelled initially: %v", err)
	}
	if c.liveCancel == nil {
		t.Fatal("liveCancel should be seeded by the constructor")
	}
}

// TestClusterLiveCtxBoundsAsyncWork exercises the pattern used by the async
// Create/DeleteObject sites: capture c.liveCtx locally before launching a
// goroutine, then derive a WithTimeout child. Cancelling liveCtx (as Stop
// does) must unblock the goroutine promptly.
func TestClusterLiveCtxBoundsAsyncWork(t *testing.T) {
	c := newClusterForTesting(nil, "test-livectx-bounds")

	// Capture liveCtx on the caller's goroutine, exactly like CreateObject does.
	liveCtx := c.liveCtx

	done := make(chan struct{})
	go func() {
		// Simulate the async goroutine pattern: wrap with a long default timeout
		// and block until the ctx is cancelled.
		asyncCtx, asyncCancel := context.WithTimeout(liveCtx, 30*time.Second)
		defer asyncCancel()
		<-asyncCtx.Done()
		close(done)
	}()

	// Cancel liveCtx — this is what Cluster.Stop does.
	c.liveCancel()

	select {
	case <-done:
		// Async goroutine exited promptly via ctx cancellation.
	case <-time.After(2 * time.Second):
		t.Fatal("async goroutine did not exit after liveCancel")
	}

	if c.liveCtx.Err() != context.Canceled {
		t.Fatalf("liveCtx should be cancelled, got: %v", c.liveCtx.Err())
	}
}

// TestClusterLiveCtxCapturePinsLifecycle verifies that goroutines capture
// liveCtx at call time. If a future lifecycle swap ever reassigned c.liveCtx
// (it currently doesn't, but the capture pattern defends against it), the
// already-launched goroutine must still bind to the original ctx.
func TestClusterLiveCtxCapturePinsLifecycle(t *testing.T) {
	c := newClusterForTesting(nil, "test-livectx-capture")

	// Launch a goroutine that captures liveCtx locally, as the async sites do.
	liveCtx := c.liveCtx
	started := make(chan struct{})
	observed := make(chan error, 1)
	go func() {
		close(started)
		<-liveCtx.Done()
		observed <- liveCtx.Err()
	}()
	<-started

	// Simulate a lifecycle swap: rebind c.liveCtx to a fresh ctx. The running
	// goroutine must NOT latch onto this new ctx — it must see the original
	// one cancel.
	oldCancel := c.liveCancel
	c.liveCtx, c.liveCancel = context.WithCancel(context.Background())

	// Cancel the original ctx. The goroutine should observe this, not the new one.
	oldCancel()

	select {
	case err := <-observed:
		if err != context.Canceled {
			t.Fatalf("goroutine observed unexpected err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not observe cancellation of the pinned liveCtx")
	}

	// The new liveCtx should still be live.
	if err := c.liveCtx.Err(); err != nil {
		t.Fatalf("new liveCtx should not be cancelled: %v", err)
	}

	// Clean up.
	c.liveCancel()
}
