package taskpool

import (
	"context"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/xiaonanln/goverse/util/logger"
)

// Job represents a unit of work to be executed by the task pool
type Job func(ctx context.Context)

// worker manages a queue of jobs
type worker struct {
	jobs chan Job
	wg   sync.WaitGroup
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
// - Fixed number of worker goroutines
// - Context-based cancellation support
// - Thread-safe under high concurrency
//
// Usage Pattern:
//
//	pool := NewTaskPool(8) // 8 workers
//	pool.Start()
//	defer pool.Stop()
//
//	// Submit jobs with keys
//	pool.SubmitByKey("user-123", func(ctx context.Context) {
//	    // Work for user-123
//	})
//	pool.SubmitByKey("user-456", func(ctx context.Context) {
//	    // Work for user-456 (runs in parallel if hashed to different worker)
//	})
//	pool.SubmitByKey("user-123", func(ctx context.Context) {
//	    // Another job for user-123 (runs serially after first job)
//	})
//
//	// Submit jobs without a key (distributed round-robin)
//	pool.Submit(func(ctx context.Context) {
//	    // Fire-and-forget work
//	})
type TaskPool struct {
	numWorkers int
	workers    []*worker
	ctx        context.Context
	cancel     context.CancelFunc
	stopped    bool
	mu         sync.RWMutex
	logger     *logger.Logger
	nextWorker uint32
}

var (
	// defaultPool is a library-scope TaskPool instance for convenience
	defaultPool     *TaskPool
	defaultPoolOnce sync.Once
)

// DefaultPool returns the default library-scope TaskPool instance
// It creates a pool with 8 workers on first call
func DefaultPool() *TaskPool {
	defaultPoolOnce.Do(func() {
		defaultPool = NewTaskPool(8)
		defaultPool.Start()
	})
	return defaultPool
}

// Submit submits a job without a key to the default pool
func Submit(job Job) {
	DefaultPool().Submit(job)
}

// SubmitByKey submits a keyed job to the default pool
func SubmitByKey(key string, job Job) {
	DefaultPool().SubmitByKey(key, job)
}

// NewTaskPool creates a new TaskPool with the specified number of workers
// If numWorkers is less than 1, it defaults to 1
func NewTaskPool(numWorkers int) *TaskPool {
	if numWorkers <= 0 {
		numWorkers = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	workers := make([]*worker, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = &worker{
			jobs: make(chan Job, 100), // Buffer for pending jobs
		}
	}

	return &TaskPool{
		numWorkers: numWorkers,
		workers:    workers,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger.NewLogger("taskpool"),
	}
}

// Start initializes and starts all worker goroutines
func (tp *TaskPool) Start() {
	for i := 0; i < tp.numWorkers; i++ {
		tp.workers[i].wg.Add(1)
		go tp.worker(i, tp.workers[i])
	}
}

// hashKey returns the worker index for a given key
func (tp *TaskPool) hashKey(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % uint32(tp.numWorkers))
}

// Submit adds a job to the queue without a key
// Jobs submitted this way are spread round-robin across workers
func (tp *TaskPool) Submit(job Job) {
	tp.enqueueJob(tp.nextWorkerIndex(), "", job)
}

// SubmitByKey adds a job to the queue for the specified key
// Jobs for the same key are executed serially in order
// Jobs for different keys may run in parallel (if hashed to different workers)
func (tp *TaskPool) SubmitByKey(key string, job Job) {
	workerIdx := tp.hashKey(key)
	tp.enqueueJob(workerIdx, key, job)
}

// enqueueJob dispatches a job to a specific worker index
func (tp *TaskPool) enqueueJob(workerIdx int, key string, job Job) {
	tp.mu.RLock()
	stopped := tp.stopped
	tp.mu.RUnlock()

	// Check if stopped
	if stopped {
		tp.rejectLog(key, "TaskPool is stopped, rejecting job")
		return
	}
	w := tp.workers[workerIdx]

	// Try to submit job
	select {
	case w.jobs <- job:
		// Job submitted successfully
	case <-tp.ctx.Done():
		// Pool is being shut down
		tp.rejectLog(key, "TaskPool context cancelled, rejecting job")
	}
}

// nextWorkerIndex returns the next worker index using round-robin selection
func (tp *TaskPool) nextWorkerIndex() int {
	val := atomic.AddUint32(&tp.nextWorker, 1) - 1
	return int(val % uint32(tp.numWorkers))
}

// rejectLog logs a rejection message with optional key context
func (tp *TaskPool) rejectLog(key, msg string) {
	if key == "" {
		tp.logger.Warnf("%s", msg)
		return
	}
	tp.logger.Warnf("%s for key: %s", msg, key)
}

// worker processes jobs for its queue
// Jobs are processed serially in the order they arrive
// Since jobs with the same key always hash to the same worker,
// serial execution per key is guaranteed
func (tp *TaskPool) worker(id int, w *worker) {
	defer w.wg.Done()

	for {
		select {
		case <-tp.ctx.Done():
			// Pool-wide cancellation
			return
		case job, ok := <-w.jobs:
			if !ok {
				// Channel closed
				return
			}
			// Execute the job
			job(tp.ctx)
		}
	}
}

// Stop gracefully shuts down the task pool
// It cancels all workers and waits for them to finish
func (tp *TaskPool) Stop() {
	tp.mu.Lock()
	tp.stopped = true
	tp.mu.Unlock()

	// Cancel the pool context to stop new submissions and signal workers
	tp.cancel()

	// Wait for all workers to finish
	// Workers will exit when they see context is cancelled
	for _, w := range tp.workers {
		w.wg.Wait()
	}
}

// NumWorkers returns the number of workers (for testing)
func (tp *TaskPool) NumWorkers() int {
	return tp.numWorkers
}
