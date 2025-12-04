package taskpool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTaskPool_BasicSubmit tests basic job submission and execution
func TestTaskPool_BasicSubmit(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	var executed atomic.Bool
	job := func(ctx context.Context) {
		executed.Store(true)
	}

	pool.SubmitByKey("key1", job)

	// Wait for job to execute
	time.Sleep(50 * time.Millisecond)

	if !executed.Load() {
		t.Fatal("Job was not executed")
	}
}

// TestTaskPool_SerialExecution tests that jobs with the same key run serially
func TestTaskPool_SerialExecution(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	var counter int32
	var order []int32
	var mu sync.Mutex

	const numJobs = 10
	var wg sync.WaitGroup
	wg.Add(numJobs)

	for i := 0; i < numJobs; i++ {
		idx := int32(i)
		job := func(ctx context.Context) {
			defer wg.Done()
			current := atomic.AddInt32(&counter, 1)
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate work

			// Verify serial execution: counter should equal idx + 1
			if current != idx+1 {
				t.Errorf("Expected counter %d, got %d for job %d", idx+1, current, idx)
			}
		}
		pool.SubmitByKey("same-key", job)
	}

	wg.Wait()

	// All jobs should have executed
	if atomic.LoadInt32(&counter) != numJobs {
		t.Fatalf("Expected %d jobs to execute, got %d", numJobs, atomic.LoadInt32(&counter))
	}

	// Verify order is sequential
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < numJobs; i++ {
		if order[i] != int32(i) {
			t.Fatalf("Job order incorrect at index %d: expected %d, got %d", i, i, order[i])
		}
	}
}

// TestTaskPool_ParallelExecution tests that jobs with different keys run in parallel
func TestTaskPool_ParallelExecution(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	var started atomic.Int32
	var maxConcurrent atomic.Int32
	var mu sync.Mutex

	const numKeys = 5
	var wg sync.WaitGroup
	wg.Add(numKeys)

	for i := 0; i < numKeys; i++ {
		key := "key-" + string(rune('A'+i))
		job := func(ctx context.Context) {
			defer wg.Done()
			current := started.Add(1)

			mu.Lock()
			if current > maxConcurrent.Load() {
				maxConcurrent.Store(current)
			}
			mu.Unlock()

			time.Sleep(100 * time.Millisecond) // Simulate work
			started.Add(-1)
		}
		pool.SubmitByKey(key, job)
	}

	wg.Wait()

	// Should have executed concurrently
	if maxConcurrent.Load() < 2 {
		t.Fatalf("Expected concurrent execution, max concurrent was %d", maxConcurrent.Load())
	}

	t.Logf("Max concurrent jobs: %d", maxConcurrent.Load())
}

// TestTaskPool_MultipleKeysWithMultipleJobs tests mixed serial and parallel execution
func TestTaskPool_MultipleKeysWithMultipleJobs(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	const numKeys = 3
	const jobsPerKey = 5

	counters := make([]int32, numKeys)
	var wg sync.WaitGroup
	wg.Add(numKeys * jobsPerKey)

	for keyIdx := 0; keyIdx < numKeys; keyIdx++ {
		key := "key-" + string(rune('0'+keyIdx))
		counterIdx := keyIdx

		for jobIdx := 0; jobIdx < jobsPerKey; jobIdx++ {
			expectedValue := int32(jobIdx + 1)
			job := func(ctx context.Context) {
				defer wg.Done()
				current := atomic.AddInt32(&counters[counterIdx], 1)
				time.Sleep(10 * time.Millisecond)

				// For each key, jobs should execute serially
				if current != expectedValue {
					t.Errorf("Key %s: expected counter %d, got %d", key, expectedValue, current)
				}
			}
			pool.SubmitByKey(key, job)
		}
	}

	wg.Wait()

	// Verify all counters
	for i, counter := range counters {
		if counter != jobsPerKey {
			t.Fatalf("Key %d: expected %d jobs, got %d", i, jobsPerKey, counter)
		}
	}
}

// TestTaskPool_ContextCancellation tests that jobs respect context cancellation
func TestTaskPool_ContextCancellation(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()

	var started atomic.Int32
	var cancelled atomic.Int32
	done := make(chan struct{})

	// Submit long-running jobs to different keys so they can start in parallel
	for i := 0; i < 5; i++ {
		key := "key-" + string(rune('0'+i))
		go func() {
			job := func(ctx context.Context) {
				started.Add(1)

				select {
				case <-time.After(500 * time.Millisecond):
					// Job completed
				case <-ctx.Done():
					// Job cancelled
					cancelled.Add(1)
				}
			}
			pool.SubmitByKey(key, job)
		}()
	}

	// Wait for jobs to start
	time.Sleep(50 * time.Millisecond)

	// Stop pool - should cancel running jobs
	pool.Stop()

	// Signal completion
	close(done)

	// At least one job should have been started and cancelled
	if started.Load() > 0 && cancelled.Load() == 0 {
		t.Fatal("Expected at least one job to be cancelled")
	}

	t.Logf("Started: %d, Cancelled: %d", started.Load(), cancelled.Load())
}

// TestTaskPool_Stop tests graceful shutdown
func TestTaskPool_Stop(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()

	var completed atomic.Int32

	// Submit jobs that complete quickly
	for i := 0; i < 3; i++ {
		job := func(ctx context.Context) {
			completed.Add(1)
		}
		pool.SubmitByKey("key1", job)
	}

	// Wait for jobs to complete
	time.Sleep(50 * time.Millisecond)

	// Stop the pool
	pool.Stop()

	// Verify jobs completed
	if completed.Load() != 3 {
		t.Logf("Only %d out of 3 jobs completed before stop", completed.Load())
	}

	// Verify no new jobs can be submitted after stop
	var executed atomic.Bool
	pool.SubmitByKey("key1", func(ctx context.Context) {
		executed.Store(true)
	})

	time.Sleep(50 * time.Millisecond)

	if executed.Load() {
		t.Fatal("Job should not execute after pool is stopped")
	}
}

// TestTaskPool_FixedWorkers tests that pool has fixed number of workers
func TestTaskPool_FixedWorkers(t *testing.T) {
	pool := NewTaskPool(3)
	pool.Start()
	defer pool.Stop()

	var wg sync.WaitGroup
	wg.Add(3)

	// Submit jobs to different keys
	for i := 0; i < 3; i++ {
		key := "key-" + string(rune('0'+i))
		job := func(ctx context.Context) {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
		}
		pool.SubmitByKey(key, job)
	}

	// Should have fixed number of workers (3)
	if pool.NumWorkers() != 3 {
		t.Fatalf("Expected 3 workers, got %d", pool.NumWorkers())
	}

	wg.Wait()

	// Workers remain active (fixed number)
	if pool.NumWorkers() != 3 {
		t.Fatalf("Expected 3 workers to remain, got %d", pool.NumWorkers())
	}
}

// TestTaskPool_HighConcurrency tests with many keys and jobs
func TestTaskPool_HighConcurrency(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	const numKeys = 20
	const jobsPerKey = 10
	const numUniqueKeys = 10 // Number of unique keys to generate (keyIdx % numUniqueKeys)

	var totalJobs atomic.Int32
	var wg sync.WaitGroup
	wg.Add(numKeys * jobsPerKey)

	for keyIdx := 0; keyIdx < numKeys; keyIdx++ {
		key := "key-" + string(rune('0'+(keyIdx%numUniqueKeys)))

		for jobIdx := 0; jobIdx < jobsPerKey; jobIdx++ {
			job := func(ctx context.Context) {
				defer wg.Done()
				totalJobs.Add(1)
				time.Sleep(time.Microsecond)
			}
			pool.SubmitByKey(key, job)
		}
	}

	wg.Wait()

	expectedTotal := int32(numKeys * jobsPerKey)
	if totalJobs.Load() != expectedTotal {
		t.Fatalf("Expected %d jobs to execute, got %d", expectedTotal, totalJobs.Load())
	}
}

// TestTaskPool_BufferedJobs tests that job buffer works correctly
func TestTaskPool_BufferedJobs(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	const numJobs = 50
	var counter atomic.Int32
	var wg sync.WaitGroup
	wg.Add(numJobs)

	// Submit many jobs quickly to the same key
	for i := 0; i < numJobs; i++ {
		job := func(ctx context.Context) {
			defer wg.Done()
			counter.Add(1)
			time.Sleep(5 * time.Millisecond)
		}
		pool.SubmitByKey("key1", job)
	}

	wg.Wait()

	if counter.Load() != numJobs {
		t.Fatalf("Expected %d jobs to execute, got %d", numJobs, counter.Load())
	}
}

// TestTaskPool_EmptyKey tests submitting jobs with empty keys
func TestTaskPool_EmptyKey(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	var executed atomic.Bool
	job := func(ctx context.Context) {
		executed.Store(true)
	}

	pool.SubmitByKey("", job)

	time.Sleep(50 * time.Millisecond)

	if !executed.Load() {
		t.Fatal("Job with empty key was not executed")
	}
}

// TestTaskPool_RaceDetector tests for data races under concurrent access
func TestTaskPool_RaceDetector(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	const numGoroutines = 20
	const numOpsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOpsPerGoroutine; j++ {
				key := "key-" + string(rune('0'+(j%5)))
				job := func(ctx context.Context) {
					// Minimal work
					time.Sleep(time.Microsecond)
				}
				pool.SubmitByKey(key, job)
			}
		}(i)
	}

	wg.Wait()

	// Allow cleanup
	time.Sleep(100 * time.Millisecond)
}

// TestTaskPool_JobOrdering tests that jobs for the same key maintain order
func TestTaskPool_JobOrdering(t *testing.T) {
	pool := NewTaskPool(8)
	pool.Start()
	defer pool.Stop()

	var order []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	const numJobs = 20
	wg.Add(numJobs)

	for i := 0; i < numJobs; i++ {
		idx := i
		job := func(ctx context.Context) {
			defer wg.Done()
			mu.Lock()
			order = append(order, idx)
			mu.Unlock()
		}
		pool.SubmitByKey("ordered-key", job)
	}

	wg.Wait()

	// Verify strict ordering
	mu.Lock()
	defer mu.Unlock()

	if len(order) != numJobs {
		t.Fatalf("Expected %d jobs in order, got %d", numJobs, len(order))
	}

	for i := 0; i < numJobs; i++ {
		if order[i] != i {
			t.Fatalf("Job order broken at index %d: expected %d, got %d", i, i, order[i])
		}
	}
}

// TestTaskPool_WorkerLifecycle tests worker lifecycle with fixed pool
func TestTaskPool_WorkerLifecycle(t *testing.T) {
	const numWorkers = 4
	pool := NewTaskPool(numWorkers)

	// Before Start, should have numWorkers
	if pool.NumWorkers() != numWorkers {
		t.Fatalf("Expected %d workers before Start, got %d", numWorkers, pool.NumWorkers())
	}

	pool.Start()

	// After Start, should still have numWorkers
	if pool.NumWorkers() != numWorkers {
		t.Fatalf("Expected %d workers after Start, got %d", numWorkers, pool.NumWorkers())
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Submit a job
	pool.SubmitByKey("key1", func(ctx context.Context) {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
	})

	// Workers remain fixed
	if pool.NumWorkers() != numWorkers {
		t.Fatalf("Expected %d workers after job submission, got %d", numWorkers, pool.NumWorkers())
	}

	wg.Wait()

	// Still fixed number of workers
	if pool.NumWorkers() != numWorkers {
		t.Fatalf("Expected %d workers after job completion, got %d", numWorkers, pool.NumWorkers())
	}

	// Stop and verify
	pool.Stop()

	// NumWorkers still returns the configured count (doesn't change after Stop)
	if pool.NumWorkers() != numWorkers {
		t.Fatalf("Expected %d workers after Stop, got %d", numWorkers, pool.NumWorkers())
	}
}
