package taskpool

import (
	"context"
	"sync"
)

// Job represents a unit of work to be executed by the task pool
type Job func(ctx context.Context)

// keyQueue manages jobs for a single key
type keyQueue struct {
	jobs   chan Job
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// TaskPool manages per-key job serialization
//
// TaskPool provides a way to execute jobs serially for each key while allowing
// parallel execution across different keys. This prevents unbounded goroutine
// creation for keyed operations like notifications.
//
// Key Features:
// - Jobs with the same key run serially in one goroutine
// - Jobs with different keys run in parallel
// - Automatic cleanup of idle key workers
// - Context-based cancellation support
// - Thread-safe under high concurrency
//
// Usage Pattern:
//
//	pool := NewTaskPool()
//	pool.Start()
//	defer pool.Stop()
//
//	// Submit jobs with keys
//	pool.Submit("user-123", func(ctx context.Context) {
//	    // Work for user-123
//	})
//	pool.Submit("user-456", func(ctx context.Context) {
//	    // Work for user-456 (runs in parallel)
//	})
//	pool.Submit("user-123", func(ctx context.Context) {
//	    // Another job for user-123 (runs serially after first job)
//	})
type TaskPool struct {
	mu      sync.RWMutex
	queues  map[string]*keyQueue
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool
}

// NewTaskPool creates a new TaskPool
func NewTaskPool() *TaskPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskPool{
		queues: make(map[string]*keyQueue),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start initializes the task pool (currently a no-op for interface consistency)
func (tp *TaskPool) Start() {
	// No-op: workers are started on-demand per key
}

// Submit adds a job to the queue for the specified key
// Jobs for the same key are executed serially in order
// Jobs for different keys are executed in parallel
func (tp *TaskPool) Submit(key string, job Job) {
	tp.mu.Lock()

	// Check if stopped
	if tp.stopped {
		tp.mu.Unlock()
		return
	}

	queue, exists := tp.queues[key]
	if !exists {
		// Create new queue for this key
		queueCtx, queueCancel := context.WithCancel(tp.ctx)
		queue = &keyQueue{
			jobs:   make(chan Job, 100), // Buffer for pending jobs
			ctx:    queueCtx,
			cancel: queueCancel,
		}
		tp.queues[key] = queue

		// Start worker for this key
		queue.wg.Add(1)
		go tp.worker(key, queue)
	}

	tp.mu.Unlock()

	// Try to submit job
	select {
	case queue.jobs <- job:
		// Job submitted successfully
	case <-queue.ctx.Done():
		// Queue is being shut down
	case <-tp.ctx.Done():
		// Pool is being shut down
	}
}

// worker processes jobs for a single key
func (tp *TaskPool) worker(key string, queue *keyQueue) {
	defer queue.wg.Done()
	defer tp.cleanupQueue(key)

	for {
		select {
		case <-queue.ctx.Done():
			// Queue-specific cancellation
			return
		case <-tp.ctx.Done():
			// Pool-wide cancellation
			return
		case job, ok := <-queue.jobs:
			if !ok {
				// Channel closed
				return
			}
			// Execute the job
			job(queue.ctx)
		}
	}
}

// cleanupQueue removes the queue for a key after the worker exits
func (tp *TaskPool) cleanupQueue(key string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	delete(tp.queues, key)
}

// Stop gracefully shuts down the task pool
// It cancels all workers and waits for them to finish
func (tp *TaskPool) Stop() {
	tp.mu.Lock()
	tp.stopped = true
	queues := make([]*keyQueue, 0, len(tp.queues))
	for _, queue := range tp.queues {
		queues = append(queues, queue)
	}
	tp.mu.Unlock()

	// Cancel the pool context first to stop new submissions
	tp.cancel()

	// Cancel all queue contexts to stop workers
	for _, queue := range queues {
		queue.cancel()
	}

	// Wait for all workers to finish
	for _, queue := range queues {
		queue.wg.Wait()
	}
}

// Len returns the number of active key queues (for testing)
func (tp *TaskPool) Len() int {
	tp.mu.RLock()
	defer tp.mu.RUnlock()
	return len(tp.queues)
}
