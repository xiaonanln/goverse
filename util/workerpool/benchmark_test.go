package workerpool

import (
	"context"
	"testing"
	"time"
)

// BenchmarkWorkerPoolVsSemaphore compares the worker pool approach to the semaphore approach
// This benchmark simulates what storeShardMapping does

// Worker pool approach
func BenchmarkWorkerPool(b *testing.B) {
	ctx := context.Background()

	b.Run("10_tasks", func(b *testing.B) {
		benchmarkWorkerPool(b, ctx, 10)
	})

	b.Run("100_tasks", func(b *testing.B) {
		benchmarkWorkerPool(b, ctx, 100)
	})

	b.Run("1000_tasks", func(b *testing.B) {
		benchmarkWorkerPool(b, ctx, 1000)
	})

	b.Run("8192_tasks", func(b *testing.B) {
		benchmarkWorkerPool(b, ctx, 8192)
	})
}

func benchmarkWorkerPool(b *testing.B, ctx context.Context, numTasks int) {
	for i := 0; i < b.N; i++ {
		pool := New(context.Background(), 20)
		pool.Start()

		tasks := make([]Task, numTasks)
		for j := 0; j < numTasks; j++ {
			tasks[j] = func(ctx context.Context) error {
				// Simulate some work
				time.Sleep(1 * time.Microsecond)
				return nil
			}
		}

		pool.SubmitAndWait(ctx, tasks)
		pool.Stop()
	}
}

// Semaphore approach (original implementation)
func BenchmarkSemaphore(b *testing.B) {
	ctx := context.Background()

	b.Run("10_tasks", func(b *testing.B) {
		benchmarkSemaphore(b, ctx, 10)
	})

	b.Run("100_tasks", func(b *testing.B) {
		benchmarkSemaphore(b, ctx, 100)
	})

	b.Run("1000_tasks", func(b *testing.B) {
		benchmarkSemaphore(b, ctx, 1000)
	})

	b.Run("8192_tasks", func(b *testing.B) {
		benchmarkSemaphore(b, ctx, 8192)
	})
}

func benchmarkSemaphore(b *testing.B, ctx context.Context, numTasks int) {
	for i := 0; i < b.N; i++ {
		const maxConcurrent = 20
		semaphore := make(chan struct{}, maxConcurrent)

		type result struct {
			err error
		}
		results := make(chan result, numTasks)

		for j := 0; j < numTasks; j++ {
			go func() {
				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Simulate some work
				time.Sleep(1 * time.Microsecond)
				results <- result{err: nil}
			}()
		}

		// Wait for all to complete
		for j := 0; j < numTasks; j++ {
			<-results
		}
	}
}
