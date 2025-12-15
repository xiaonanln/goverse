package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/testutil"
)

func TestClusterReadyChannel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:47000", testutil.TestNumShards)
	c := newClusterForTesting(testNode, "TestClusterReady")

	// Test 1: Channel should block before cluster is ready
	select {
	case <-c.ClusterReady():
		t.Fatal("Channel should not be closed before cluster is ready")
	case <-time.After(100 * time.Millisecond):
		// Expected: channel is still open
	}

	// Mark cluster as ready
	c.markClusterReady()

	// Channel should now be closed
	select {
	case <-c.ClusterReady():
		// Expected: channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel should be closed after cluster is ready")
	}
}

func TestMultipleGoroutinesWaitingOnClusterReady(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:47000", testutil.TestNumShards)
	c := newClusterForTesting(testNode, "TestMultipleGoroutines")

	done := make(chan bool, 5)

	// Start multiple goroutines waiting on cluster ready
	for i := 0; i < 5; i++ {
		go func() {
			<-c.ClusterReady()
			done <- true
		}()
	}

	// Give goroutines a moment to start waiting
	time.Sleep(50 * time.Millisecond)

	// Mark cluster as ready
	c.markClusterReady()

	// All goroutines should complete
	timeout := time.After(1 * time.Second)
	for i := 0; i < 5; i++ {
		select {
		case <-done:
			// Expected: goroutine completed
		case <-timeout:
			t.Fatalf("Timed out waiting for goroutine %d to complete", i+1)
		}
	}
}

func TestMarkClusterReadyIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	testNode := node.MustNewNode(ctx, t, "localhost:47000", testutil.TestNumShards)
	c := newClusterForTesting(testNode, "TestIdempotent")

	// Mark cluster as ready multiple times
	c.markClusterReady()
	c.markClusterReady()
	c.markClusterReady()

	// Channel should be closed
	select {
	case <-c.ClusterReady():
		// Expected: channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Channel should be closed after markClusterReady")
	}
}
