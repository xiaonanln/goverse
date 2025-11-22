package testutil

import (
	"testing"
	"time"
)

// mockClusterReady is a mock implementation of ClusterReadyWaiter for testing
type mockClusterReady struct {
	readyChan chan bool
}

func (m *mockClusterReady) ClusterReady() <-chan bool {
	return m.readyChan
}

func newMockClusterReady() *mockClusterReady {
	return &mockClusterReady{
		readyChan: make(chan bool),
	}
}

func newMockClusterAlreadyReady() *mockClusterReady {
	m := &mockClusterReady{
		readyChan: make(chan bool),
	}
	close(m.readyChan)
	return m
}

func TestWaitForClusterReady_AlreadyReady(t *testing.T) {
	// Create a mock cluster that is already ready
	cluster := newMockClusterAlreadyReady()

	// This should return immediately without timeout
	start := time.Now()
	WaitForClusterReady(t, cluster)
	elapsed := time.Since(start)

	// Should complete in well under 1 second (typically microseconds)
	if elapsed > 1*time.Second {
		t.Fatalf("WaitForClusterReady took too long for already-ready cluster: %v", elapsed)
	}
}

func TestWaitForClusterReady_BecomesReady(t *testing.T) {
	cluster := newMockClusterReady()

	// Start a goroutine that marks cluster ready after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(cluster.readyChan)
	}()

	// This should wait for the cluster to become ready
	start := time.Now()
	WaitForClusterReady(t, cluster)
	elapsed := time.Since(start)

	// Should complete in around 100ms
	if elapsed < 50*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("WaitForClusterReady took unexpected time: %v (expected ~100ms)", elapsed)
	}
}

// Note: We cannot easily test the timeout case because:
// 1. WaitForShardMappingTimeout is a const and cannot be modified
// 2. Waiting for the full 10 seconds would make tests too slow
// 3. The timeout behavior is straightforward (select with time.After)
// The timeout case will be implicitly tested when used in actual cluster tests.
