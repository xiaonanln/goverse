package testutil

import (
	"sync"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/xiaonanln/goverse/util/metrics"
)

// TestLockMetrics_Sequential verifies that LockMetrics allows sequential execution
func TestLockMetrics_Sequential(t *testing.T) {
	// This test verifies that the LockMetrics function can be called without panicking
	// The actual locking behavior is tested in TestLockMetrics_Parallel
	
	// Verify the function doesn't panic when called
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("LockMetrics panicked: %v", r)
		}
	}()
	
	// Call LockMetrics in a sub-test so cleanup works properly
	t.Run("SubTest", func(t *testing.T) {
		LockMetrics(t)
		// If we get here without panic, the test passes
	})
}

// TestLockMetrics_Parallel verifies that LockMetrics prevents parallel execution
func TestLockMetrics_Parallel(t *testing.T) {
	// Track execution order
	var executionOrder []int
	var mu sync.Mutex
	
	// This test verifies that when multiple test goroutines call LockMetrics,
	// they execute sequentially, not in parallel
	
	for i := 0; i < 3; i++ {
		i := i // capture loop variable
		t.Run("", func(t *testing.T) {
			t.Parallel() // Try to run in parallel
			
			LockMetrics(t) // This should serialize execution
			
			// Record that we started
			mu.Lock()
			executionOrder = append(executionOrder, i)
			startCount := len(executionOrder)
			mu.Unlock()
			
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
			
			// Verify no other test has started while we're running
			mu.Lock()
			endCount := len(executionOrder)
			mu.Unlock()
			
			// If tests are truly sequential, endCount should equal startCount
			// (no other test should have started while we were sleeping)
			if endCount != startCount {
				t.Errorf("Test %d: Expected %d tests to have started, but %d started (tests are not properly serialized)", i, startCount, endCount)
			}
		})
	}
}

// TestLockMetrics_Cleanup verifies that the lock is released on test completion
func TestLockMetrics_Cleanup(t *testing.T) {
	// Create a channel to signal when the lock is released
	done := make(chan bool, 1)
	
	// Start a goroutine that tries to acquire the lock after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		metricsTestMutex.Lock()
		metricsTestMutex.Unlock()
		done <- true
	}()
	
	// Run a sub-test that acquires the lock
	t.Run("SubTest", func(t *testing.T) {
		LockMetrics(t)
		// Lock is held during this test
		time.Sleep(20 * time.Millisecond)
		// Lock will be released by t.Cleanup when this test ends
	})
	
	// Wait for the goroutine to acquire the lock (meaning cleanup happened)
	select {
	case <-done:
		// Success - cleanup released the lock
	case <-time.After(200 * time.Millisecond):
		t.Error("Lock was not released by cleanup within expected time")
	}
}

// TestLockMetrics_ResetsMetrics verifies that LockMetrics resets all metrics
func TestLockMetrics_ResetsMetrics(t *testing.T) {
	// Set up some metric values
	metrics.AssignedShardsTotal.WithLabelValues("test-node-1").Set(10)
	metrics.ObjectCount.WithLabelValues("test-node-2", "TestObject", "5").Set(20)
	metrics.ClientsConnected.WithLabelValues("test-node-3", "grpc").Set(5)
	
	// Verify metrics are set
	if promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("test-node-1")) != 10 {
		t.Fatal("Test setup failed: AssignedShardsTotal not set correctly")
	}
	if promtestutil.ToFloat64(metrics.ObjectCount.WithLabelValues("test-node-2", "TestObject", "5")) != 20 {
		t.Fatal("Test setup failed: ObjectCount not set correctly")
	}
	if promtestutil.ToFloat64(metrics.ClientsConnected.WithLabelValues("test-node-3", "grpc")) != 5 {
		t.Fatal("Test setup failed: ClientsConnected not set correctly")
	}
	
	// Run a sub-test that calls LockMetrics
	t.Run("SubTest", func(t *testing.T) {
		LockMetrics(t)
		
		// Verify all metrics are reset to 0
		if promtestutil.ToFloat64(metrics.AssignedShardsTotal.WithLabelValues("test-node-1")) != 0 {
			t.Error("AssignedShardsTotal was not reset by LockMetrics")
		}
		if promtestutil.ToFloat64(metrics.ObjectCount.WithLabelValues("test-node-2", "TestObject", "5")) != 0 {
			t.Error("ObjectCount was not reset by LockMetrics")
		}
		if promtestutil.ToFloat64(metrics.ClientsConnected.WithLabelValues("test-node-3", "grpc")) != 0 {
			t.Error("ClientsConnected was not reset by LockMetrics")
		}
	})
}
