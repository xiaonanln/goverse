package taskpool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestIntegration_NotificationPattern tests the pattern used for notifications in the codebase
func TestIntegration_NotificationPattern(t *testing.T) {
	pool := NewTaskPool(4)
	pool.Start()
	defer pool.Stop()

	// Simulate a component (like gate or node) with an address
	componentAddr := "localhost:12345"
	var notificationCount int64

	// Submit 100 notifications using the same key
	// These should execute serially in order
	for i := 0; i < 100; i++ {
		pool.SubmitByKey(componentAddr, func(ctx context.Context) {
			atomic.AddInt64(&notificationCount, 1)
			time.Sleep(time.Millisecond) // Simulate some work
		})
	}

	// Wait for all jobs to complete
	time.Sleep(200 * time.Millisecond)

	// All notifications should have been processed
	count := atomic.LoadInt64(&notificationCount)
	if count != 100 {
		t.Errorf("Expected 100 notifications, got %d", count)
	}
}

// TestIntegration_MetricsPattern tests the pattern used for metrics in the codebase
func TestIntegration_MetricsPattern(t *testing.T) {
	pool := NewTaskPool(4)
	pool.Start()
	defer pool.Stop()

	var counterIncCount int64
	var gaugeSetCount int64

	// Simulate counter increments (fire-and-forget)
	for i := 0; i < 50; i++ {
		pool.Submit(func(ctx context.Context) {
			atomic.AddInt64(&counterIncCount, 1)
		})
	}

	// Simulate gauge sets (keyed by component address)
	componentAddr := "localhost:12345"
	for i := 0; i < 50; i++ {
		pool.SubmitByKey(componentAddr, func(ctx context.Context) {
			atomic.AddInt64(&gaugeSetCount, 1)
		})
	}

	// Wait for all jobs to complete
	time.Sleep(100 * time.Millisecond)

	// All operations should have been processed
	counterCount := atomic.LoadInt64(&counterIncCount)
	gaugeCount := atomic.LoadInt64(&gaugeSetCount)

	if counterCount != 50 {
		t.Errorf("Expected 50 counter increments, got %d", counterCount)
	}
	if gaugeCount != 50 {
		t.Errorf("Expected 50 gauge sets, got %d", gaugeCount)
	}
}

// TestIntegration_MultipleComponents tests that different components execute in parallel
func TestIntegration_MultipleComponents(t *testing.T) {
	pool := NewTaskPool(4)
	pool.Start()
	defer pool.Stop()

	var component1Count, component2Count, component3Count int64

	// Submit jobs for three different components
	// These should execute in parallel since they have different keys
	for i := 0; i < 10; i++ {
		pool.SubmitByKey("component1", func(ctx context.Context) {
			atomic.AddInt64(&component1Count, 1)
			time.Sleep(5 * time.Millisecond)
		})
		pool.SubmitByKey("component2", func(ctx context.Context) {
			atomic.AddInt64(&component2Count, 1)
			time.Sleep(5 * time.Millisecond)
		})
		pool.SubmitByKey("component3", func(ctx context.Context) {
			atomic.AddInt64(&component3Count, 1)
			time.Sleep(5 * time.Millisecond)
		})
	}

	// Wait for all jobs to complete
	time.Sleep(200 * time.Millisecond)

	// All components should have processed their jobs
	if atomic.LoadInt64(&component1Count) != 10 {
		t.Errorf("Component1: expected 10, got %d", component1Count)
	}
	if atomic.LoadInt64(&component2Count) != 10 {
		t.Errorf("Component2: expected 10, got %d", component2Count)
	}
	if atomic.LoadInt64(&component3Count) != 10 {
		t.Errorf("Component3: expected 10, got %d", component3Count)
	}
}
