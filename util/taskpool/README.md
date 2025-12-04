# TaskPool

TaskPool provides per-key job serialization for Go applications with a fixed number of workers.

## Overview

TaskPool manages job execution with the following guarantees:
- Jobs with the **same key** run serially (one at a time)
- Jobs with **different keys** may run in parallel (if hashed to different workers)
- Fixed number of worker goroutines (prevents unbounded goroutine creation)
- Keys are hashed to workers for consistent serial execution

This is particularly useful for:
- Keyed notifications where order matters per key
- Rate-limiting operations per entity
- Coordinating updates to specific resources

## Usage

### Using the Default Pool

```go
import "github.com/xiaonanln/goverse/util/taskpool"

// Use the library-scope default pool (8 workers)
taskpool.Submit("user-123", func(ctx context.Context) {
    // Process notification for user-123
    // This runs serially with other jobs for user-123
})

taskpool.Submit("user-456", func(ctx context.Context) {
    // Process notification for user-456
    // May run in parallel if hashed to different worker
})
```

### Creating a Custom Pool

```go
import "github.com/xiaonanln/goverse/util/taskpool"

// Create a pool with 16 workers
pool := taskpool.NewTaskPool(16)
pool.Start()
defer pool.Stop()

// Submit jobs with keys
pool.Submit("user-123", func(ctx context.Context) {
    // Process notification for user-123
    // This runs serially with other jobs for user-123
})

pool.Submit("user-456", func(ctx context.Context) {
    // Process notification for user-456
    // May run in parallel if hashed to different worker
})

pool.Submit("user-123", func(ctx context.Context) {
    // Another job for user-123
    // This waits for the first user-123 job to complete
})
```

## Features

- **Per-key Serialization**: Jobs with the same key execute in order
- **Fixed Workers**: Bounded number of goroutines (specified at creation)
- **Key Hashing**: Keys are hashed to workers for consistent routing
- **Context Support**: Jobs receive a context for cancellation
- **Graceful Shutdown**: `Stop()` cancels running jobs and waits for completion
- **Thread-safe**: Safe for concurrent use from multiple goroutines
- **Default Pool**: Convenient library-scope instance for simple use cases

## Design

The pool has a fixed number of workers (goroutines), each with:
- Buffered channel (100 jobs capacity)
- Jobs processed serially in arrival order

Keys are hashed using FNV-1a to determine which worker handles the job. This ensures:
- Same key always goes to same worker (serial execution guarantee)
- Different keys may go to different workers (parallelism)
- Bounded resource usage (fixed number of goroutines)

## API

### DefaultPool

```go
func DefaultPool() *TaskPool
```

Returns the default library-scope TaskPool instance (8 workers). Created and started automatically on first use.

### Submit (package-level)

```go
func Submit(key string, job Job)
```

Submits a job to the default pool.

### NewTaskPool

```go
func NewTaskPool(numWorkers int) *TaskPool
```

Creates a new TaskPool instance with the specified number of workers. Must be at least 1.

### Start

```go
func (tp *TaskPool) Start()
```

Starts all worker goroutines. Must be called before submitting jobs.

### Submit

```go
func (tp *TaskPool) Submit(key string, job Job)
```

Submits a job for execution. Jobs with the same key run serially.

**Parameters:**
- `key`: The key to serialize jobs under
- `job`: Function to execute with signature `func(ctx context.Context)`

### Stop

```go
func (tp *TaskPool) Stop()
```

Gracefully shuts down the task pool:
1. Stops accepting new jobs
2. Cancels all running jobs via context
3. Waits for workers to finish

### NumWorkers

```go
func (tp *TaskPool) NumWorkers() int
```

Returns the number of workers in the pool.

## Example: Notification System

```go
// Use taskpool to serialize notifications per user
pool := taskpool.NewTaskPool(8)
pool.Start()
defer pool.Stop()

// Multiple events for the same user are processed in order
for _, event := range events {
    userID := event.UserID
    pool.Submit(userID, func(ctx context.Context) {
        // Send notification for this event
        sendNotification(ctx, event)
    })
}
```

## Testing

Run tests:
```bash
go test -v ./util/taskpool/...
```

Run with race detector:
```bash
go test -race ./util/taskpool/...
```

## Performance

- **Memory**: Fixed overhead based on number of workers (~2KB per worker)
- **Latency**: Minimal overhead (channel send/receive + hash computation)
- **Throughput**: Limited by number of workers and job execution time
- **Scalability**: Use more workers for higher throughput, fewer for lower memory

## Comparison with Alternatives

| Approach | Goroutines | Order Guarantee | Cross-key Parallel | Resource Bounded |
|----------|------------|-----------------|-------------------|------------------|
| taskpool (fixed) | Fixed N | ✓ | ✓ (limited) | ✓ |
| 1 per key | Variable | ✓ | ✓ | ✗ |
| Lock per key | Variable | ✓ | ✓ | ✗ |
| Global lock | Variable | ✓ | ✗ | ✗ |
| Goroutine per job | 1 per job | ✗ | ✓ | ✗ |

TaskPool provides bounded resource usage while maintaining per-key ordering guarantees.
