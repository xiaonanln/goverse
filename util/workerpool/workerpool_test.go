package workerpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		numWorkers int
		want       int
	}{
		{"positive number", 5, 5},
		{"zero defaults to 1", 0, 1},
		{"negative defaults to 1", -5, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wp := New(context.Background(), tt.numWorkers)
			if wp.numWorkers != tt.want {
				t.Fatalf("New(%d).numWorkers = %d, want %d", tt.numWorkers, wp.numWorkers, tt.want)
			}
			wp.Stop()
		})
	}
}

func TestWorkerPool_BasicExecution(t *testing.T) {
	wp := New(context.Background(), 3)
	wp.Start()
	defer wp.Stop()

	var counter int32
	task := func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	}

	// Submit 10 tasks and collect results
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resultChan := wp.Submit(task)
			<-resultChan // Wait for completion
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&counter) != 10 {
		t.Fatalf("Expected 10 tasks to execute, got %d", atomic.LoadInt32(&counter))
	}
}

func TestWorkerPool_ErrorHandling(t *testing.T) {
	wp := New(context.Background(), 2)
	wp.Start()
	defer wp.Stop()

	expectedErr := errors.New("test error")
	task := func(ctx context.Context) error {
		return expectedErr
	}

	resultChan := wp.Submit(task)

	// Wait for result
	select {
	case err := <-resultChan:
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !errors.Is(err, expectedErr) {
			t.Fatalf("Expected error %v, got %v", expectedErr, err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

func TestWorkerPool_SubmitAndWait(t *testing.T) {
	wp := New(context.Background(), 5)
	wp.Start()
	defer wp.Stop()

	ctx := context.Background()
	var counter int32

	// Create 20 tasks
	tasks := make([]Task, 20)
	for i := 0; i < 20; i++ {
		tasks[i] = func(ctx context.Context) error {
			atomic.AddInt32(&counter, 1)
			time.Sleep(10 * time.Millisecond)
			return nil
		}
	}

	results := wp.SubmitAndWait(ctx, tasks)

	if len(results) != 20 {
		t.Fatalf("Expected 20 results, got %d", len(results))
	}

	if atomic.LoadInt32(&counter) != 20 {
		t.Fatalf("Expected 20 tasks to execute, got %d", atomic.LoadInt32(&counter))
	}

	// Check all results are successful
	for i, result := range results {
		if result.Err != nil {
			t.Fatalf("Result %d has error: %v", i, result.Err)
		}
	}
}

func TestWorkerPool_SubmitAndWaitWithErrors(t *testing.T) {
	wp := New(context.Background(), 3)
	wp.Start()
	defer wp.Stop()

	ctx := context.Background()
	expectedErr := errors.New("task error")

	// Create tasks, some with errors
	tasks := make([]Task, 10)
	for i := 0; i < 10; i++ {
		idx := i
		tasks[i] = func(ctx context.Context) error {
			if idx%2 == 0 {
				return expectedErr
			}
			return nil
		}
	}

	results := wp.SubmitAndWait(ctx, tasks)

	if len(results) != 10 {
		t.Fatalf("Expected 10 results, got %d", len(results))
	}

	// Count errors
	errorCount := 0
	successCount := 0
	for _, result := range results {
		if result.Err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	if errorCount != 5 {
		t.Fatalf("Expected 5 errors, got %d", errorCount)
	}
	if successCount != 5 {
		t.Fatalf("Expected 5 successes, got %d", successCount)
	}
}

func TestWorkerPool_ContextCancellation(t *testing.T) {
	wp := New(context.Background(), 2)
	wp.Start()
	defer wp.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	var started int32
	var completed int32

	tasks := make([]Task, 10)
	for i := 0; i < 10; i++ {
		tasks[i] = func(ctx context.Context) error {
			atomic.AddInt32(&started, 1)
			// Simulate long-running task
			select {
			case <-time.After(200 * time.Millisecond):
				atomic.AddInt32(&completed, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Cancel context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	results := wp.SubmitAndWait(ctx, tasks)

	// All tasks will be submitted because SubmitAndWait spawns goroutines
	// But tasks themselves will respect context cancellation
	if len(results) != 10 {
		t.Fatalf("Expected 10 results, got %d", len(results))
	}

	// Count cancelled tasks
	cancelledCount := 0
	for _, result := range results {
		if result.Err != nil && errors.Is(result.Err, context.Canceled) {
			cancelledCount++
		}
	}

	// We expect some tasks to be cancelled (those that were running when context was cancelled)
	// or those that checked context before completing
	t.Logf("Started: %d, Completed: %d, Results: %d, Cancelled: %d",
		atomic.LoadInt32(&started),
		atomic.LoadInt32(&completed),
		len(results),
		cancelledCount)
}

func TestWorkerPool_Stop(t *testing.T) {
	wp := New(context.Background(), 3)
	wp.Start()

	// Submit a task
	resultChan := wp.Submit(func(ctx context.Context) error {
		return nil
	})

	// Wait for result
	select {
	case <-resultChan:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Task didn't complete before stop")
	}

	// Stop the pool
	wp.Stop()

	// Try to submit after stop - should fail with context error
	resultChan = wp.Submit(func(ctx context.Context) error {
		return nil
	})

	select {
	case err := <-resultChan:
		if err == nil {
			t.Fatal("Should get error when submitting after stop")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Should get immediate error when submitting after stop")
	}
}

func TestWorkerPool_StopNow(t *testing.T) {
	wp := New(context.Background(), 2)
	wp.Start()

	var started int32
	var completed int32

	// Submit long-running tasks
	for i := 0; i < 5; i++ {
		wp.Submit(func(ctx context.Context) error {
			atomic.AddInt32(&started, 1)
			select {
			case <-time.After(1 * time.Second):
				atomic.AddInt32(&completed, 1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}

	// Give tasks a moment to start
	time.Sleep(50 * time.Millisecond)

	// Force stop
	wp.StopNow()

	// Should have started some but not completed all
	startedCount := atomic.LoadInt32(&started)
	completedCount := atomic.LoadInt32(&completed)

	if startedCount == 0 {
		t.Fatal("No tasks started")
	}

	if completedCount >= startedCount {
		t.Fatalf("All tasks completed despite StopNow, started=%d completed=%d",
			startedCount, completedCount)
	}

	t.Logf("Started: %d, Completed: %d", startedCount, completedCount)
}

func TestWorkerPool_Concurrency(t *testing.T) {
	numWorkers := 5
	wp := New(context.Background(), numWorkers)
	wp.Start()
	defer wp.Stop()

	var activeWorkers int32
	var maxConcurrent int32
	var mu sync.Mutex

	task := func(ctx context.Context) error {
		current := atomic.AddInt32(&activeWorkers, 1)

		mu.Lock()
		if current > atomic.LoadInt32(&maxConcurrent) {
			atomic.StoreInt32(&maxConcurrent, current)
		}
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&activeWorkers, -1)
		return nil
	}

	// Submit many tasks and wait for all
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resultChan := wp.Submit(task)
			<-resultChan
		}()
	}

	wg.Wait()

	maxConcurrentValue := atomic.LoadInt32(&maxConcurrent)
	if maxConcurrentValue > int32(numWorkers) {
		t.Fatalf("Max concurrent workers %d exceeded pool size %d",
			maxConcurrentValue, numWorkers)
	}

	if maxConcurrentValue < int32(numWorkers) {
		t.Logf("Max concurrent workers %d was less than pool size %d (may be expected with timing)",
			maxConcurrentValue, numWorkers)
	}
}

func TestWorkerPool_EmptyTaskList(t *testing.T) {
	wp := New(context.Background(), 3)
	wp.Start()
	defer wp.Stop()

	ctx := context.Background()
	results := wp.SubmitAndWait(ctx, nil)

	if results != nil {
		t.Fatalf("Expected nil results for empty task list, got %v", results)
	}

	results = wp.SubmitAndWait(ctx, []Task{})
	if results != nil {
		t.Fatalf("Expected nil results for empty task list, got %v", results)
	}
}

func BenchmarkWorkerPool_SubmitAndWait(b *testing.B) {
	wp := New(context.Background(), 10)
	wp.Start()
	defer wp.Stop()

	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		tasks := make([]Task, 100)
		for j := 0; j < 100; j++ {
			tasks[j] = func(ctx context.Context) error {
				// Simulate some work
				time.Sleep(1 * time.Millisecond)
				return nil
			}
		}
		wp.SubmitAndWait(ctx, tasks)
	}
}

func BenchmarkWorkerPool_Submit(b *testing.B) {
	wp := New(context.Background(), 10)
	wp.Start()
	defer wp.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resultChan := wp.Submit(func(ctx context.Context) error {
			return nil
		})
		<-resultChan
	}
}
