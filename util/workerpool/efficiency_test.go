package workerpool

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestGoroutineCount compares the number of goroutines created by each approach
func TestGoroutineCount(t *testing.T) {
	tests := []struct {
		name     string
		numTasks int
	}{
		{"10 tasks", 10},
		{"100 tasks", 100},
		{"1000 tasks", 1000},
		{"8192 tasks", 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("WorkerPool", func(t *testing.T) {
				runtime.GC()
				before := runtime.NumGoroutine()

				pool := New(context.Background(), 20)
				pool.Start()

				during := runtime.NumGoroutine()
				workerGoroutines := during - before

				ctx := context.Background()
				tasks := make([]Task, tt.numTasks)
				for j := 0; j < tt.numTasks; j++ {
					tasks[j] = func(ctx context.Context) error {
						time.Sleep(10 * time.Millisecond)
						return nil
					}
				}

				pool.SubmitAndWait(ctx, tasks)
				pool.Stop()

				t.Logf("Worker pool created %d goroutines for %d tasks", workerGoroutines, tt.numTasks)
			})

			t.Run("Semaphore", func(t *testing.T) {
				runtime.GC()
				before := runtime.NumGoroutine()

				const maxConcurrent = 20
				semaphore := make(chan struct{}, maxConcurrent)

				type result struct {
					err error
				}
				results := make(chan result, tt.numTasks)

				for j := 0; j < tt.numTasks; j++ {
					go func() {
						semaphore <- struct{}{}
						defer func() { <-semaphore }()
						time.Sleep(10 * time.Millisecond)
						results <- result{err: nil}
					}()
				}

				// Give goroutines time to be created
				time.Sleep(5 * time.Millisecond)
				during := runtime.NumGoroutine()
				taskGoroutines := during - before

				// Wait for completion
				for j := 0; j < tt.numTasks; j++ {
					<-results
				}

				t.Logf("Semaphore approach created %d goroutines for %d tasks", taskGoroutines, tt.numTasks)
			})
		})
	}
}

// TestMemoryEfficiency compares memory usage between approaches
func TestMemoryEfficiency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory efficiency test in short mode")
	}

	const numTasks = 8192

	t.Run("WorkerPool", func(t *testing.T) {
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

		pool := New(context.Background(), 20)
		pool.Start()

		ctx := context.Background()
		tasks := make([]Task, numTasks)
		for j := 0; j < numTasks; j++ {
			tasks[j] = func(ctx context.Context) error {
				time.Sleep(1 * time.Microsecond)
				return nil
			}
		}

		pool.SubmitAndWait(ctx, tasks)
		pool.Stop()

		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)

		t.Logf("Worker pool allocated %d bytes for %d tasks", m2.TotalAlloc-m1.TotalAlloc, numTasks)
	})

	t.Run("Semaphore", func(t *testing.T) {
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

		const maxConcurrent = 20
		semaphore := make(chan struct{}, maxConcurrent)

		type result struct {
			err error
		}
		results := make(chan result, numTasks)

		for j := 0; j < numTasks; j++ {
			go func() {
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				time.Sleep(1 * time.Microsecond)
				results <- result{err: nil}
			}()
		}

		for j := 0; j < numTasks; j++ {
			<-results
		}

		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)

		t.Logf("Semaphore approach allocated %d bytes for %d tasks", m2.TotalAlloc-m1.TotalAlloc, numTasks)
	})
}
