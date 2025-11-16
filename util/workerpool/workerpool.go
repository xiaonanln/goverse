package workerpool

import (
	"context"
	"sync"
)

// Task represents a unit of work to be executed by the worker pool
type Task func(ctx context.Context) error

// Result represents the result of a task execution
type Result struct {
	Err error
}

// WorkerPool is a fixed-size pool of goroutines that execute tasks
type WorkerPool struct {
	numWorkers int
	tasks      chan taskWrapper
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// taskWrapper wraps a task with its result channel
type taskWrapper struct {
	task   Task
	result chan error
}

// New creates a new worker pool with the specified number of workers
// The provided context will be used as the base context for the pool
func New(ctx context.Context, numWorkers int) *WorkerPool {
	if numWorkers <= 0 {
		numWorkers = 1
	}

	ctx, cancel := context.WithCancel(ctx)

	// Buffer size of numWorkers*2 allows for some queuing of tasks
	// while workers are busy, reducing submission blocking
	taskBufferSize := numWorkers * 2

	wp := &WorkerPool{
		numWorkers: numWorkers,
		tasks:      make(chan taskWrapper, taskBufferSize),
		ctx:        ctx,
		cancel:     cancel,
	}

	return wp
}

// Start initializes and starts all worker goroutines
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// worker is the main loop for each worker goroutine
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.ctx.Done():
			return
		case tw, ok := <-wp.tasks:
			if !ok {
				return
			}
			err := tw.task(wp.ctx)
			// Send result back
			select {
			case tw.result <- err:
			case <-wp.ctx.Done():
				return
			}
		}
	}
}

// Submit adds a task to the worker pool for execution
// Returns a channel that will receive the result
// If the pool has been stopped, the result channel will contain context.Canceled
func (wp *WorkerPool) Submit(task Task) <-chan error {
	result := make(chan error, 1)

	// Check if pool is stopped first
	select {
	case <-wp.ctx.Done():
		result <- wp.ctx.Err()
		return result
	default:
	}

	tw := taskWrapper{
		task:   task,
		result: result,
	}

	// Try to send task, handling the case where channel might be closed
	// We use select with default to avoid blocking if the channel is closed
	select {
	case <-wp.ctx.Done():
		result <- wp.ctx.Err()
	case wp.tasks <- tw:
		// Successfully submitted
	default:
		// Channel is likely full or closed, fall back to blocking send with recovery
		defer func() {
			if r := recover(); r != nil {
				// Channel was closed, pool is stopped
				result <- context.Canceled
			}
		}()
		wp.tasks <- tw
	}

	return result
}

// SubmitAndWait submits multiple tasks and waits for all to complete
// Returns a slice of results in the order they complete (not submission order)
func (wp *WorkerPool) SubmitAndWait(ctx context.Context, tasks []Task) []Result {
	if len(tasks) == 0 {
		return nil
	}

	results := make([]Result, 0, len(tasks))
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, task := range tasks {
		wg.Add(1)

		go func(t Task) {
			defer wg.Done()

			resultChan := wp.Submit(t)

			select {
			case <-ctx.Done():
				mu.Lock()
				results = append(results, Result{Err: ctx.Err()})
				mu.Unlock()
			case err := <-resultChan:
				mu.Lock()
				results = append(results, Result{Err: err})
				mu.Unlock()
			}
		}(task)
	}

	wg.Wait()
	return results
}

// Stop gracefully shuts down the worker pool
// It closes the task channel and waits for all workers to finish
func (wp *WorkerPool) Stop() {
	wp.cancel() // Cancel context first to signal workers
	close(wp.tasks)
	wp.wg.Wait()
}

// StopNow forcefully stops the worker pool without waiting for tasks to complete
func (wp *WorkerPool) StopNow() {
	wp.cancel()
	wp.wg.Wait()
}
