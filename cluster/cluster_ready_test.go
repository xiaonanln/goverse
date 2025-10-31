package cluster

import (
	"sync"
	"testing"
)

func TestClusterReadyCallback(t *testing.T) {
	c := newClusterForTesting("TestClusterReady")

	// Test 1: Register callback before cluster is ready
	var callbackInvoked bool
	var mu sync.Mutex
	
	c.RegisterClusterReadyCallback(func() {
		mu.Lock()
		defer mu.Unlock()
		callbackInvoked = true
	})

	// Verify callback hasn't been invoked yet
	mu.Lock()
	if callbackInvoked {
		t.Error("Callback should not be invoked before cluster is ready")
	}
	mu.Unlock()

	// Mark cluster as ready
	c.markClusterReady()

	// Verify callback was invoked
	mu.Lock()
	if !callbackInvoked {
		t.Error("Callback should have been invoked after cluster is ready")
	}
	mu.Unlock()

	// Test 2: Register callback after cluster is ready
	var secondCallbackInvoked bool
	c.RegisterClusterReadyCallback(func() {
		secondCallbackInvoked = true
	})

	// Verify callback was invoked immediately
	if !secondCallbackInvoked {
		t.Error("Callback should be invoked immediately when cluster is already ready")
	}
}

func TestMultipleClusterReadyCallbacks(t *testing.T) {
	c := newClusterForTesting("TestMultipleCallbacks")

	var count int
	var mu sync.Mutex

	// Register multiple callbacks
	for i := 0; i < 5; i++ {
		c.RegisterClusterReadyCallback(func() {
			mu.Lock()
			defer mu.Unlock()
			count++
		})
	}

	// Mark cluster as ready
	c.markClusterReady()

	// Verify all callbacks were invoked
	mu.Lock()
	if count != 5 {
		t.Errorf("Expected 5 callbacks to be invoked, got %d", count)
	}
	mu.Unlock()

	// Verify callbacks list is cleared
	if len(c.clusterReadyCallbacks) != 0 {
		t.Errorf("Expected callbacks list to be cleared after invocation, got %d callbacks", len(c.clusterReadyCallbacks))
	}
}

func TestIsClusterReady(t *testing.T) {
	c := newClusterForTesting("TestIsClusterReady")

	// Initially, cluster should not be ready
	if c.IsClusterReady() {
		t.Error("Cluster should not be ready initially")
	}

	// Mark cluster as ready
	c.markClusterReady()

	// Now cluster should be ready
	if !c.IsClusterReady() {
		t.Error("Cluster should be ready after markClusterReady")
	}
}

func TestResetForTestingClearsReadyState(t *testing.T) {
	c := Get()

	// Mark cluster as ready
	c.markClusterReady()
	if !c.IsClusterReady() {
		t.Error("Cluster should be ready after markClusterReady")
	}

	// Register a callback
	c.RegisterClusterReadyCallback(func() {})

	// Reset for testing
	c.ResetForTesting()

	// Verify cluster is no longer ready
	if c.IsClusterReady() {
		t.Error("Cluster should not be ready after ResetForTesting")
	}

	// Verify callbacks are cleared
	if len(c.clusterReadyCallbacks) != 0 {
		t.Errorf("Callbacks should be cleared after ResetForTesting, got %d", len(c.clusterReadyCallbacks))
	}
}
