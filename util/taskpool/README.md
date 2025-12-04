# TaskPool

TaskPool provides per-key job serialization for Go applications.

## Overview

TaskPool manages job execution with the following guarantees:
- Jobs with the **same key** run serially (one at a time) in a dedicated goroutine
- Jobs with **different keys** run in parallel
- Prevents unbounded goroutine creation for keyed operations

This is particularly useful for:
- Keyed notifications where order matters per key
- Rate-limiting operations per entity
- Coordinating updates to specific resources

## Usage

```go
import "github.com/xiaonanln/goverse/util/taskpool"

// Create and start a task pool
pool := taskpool.NewTaskPool()
pool.Start()
defer pool.Stop()

// Submit jobs with keys
pool.Submit("user-123", func(ctx context.Context) {
    // Process notification for user-123
    // This runs serially with other jobs for user-123
})

pool.Submit("user-456", func(ctx context.Context) {
    // Process notification for user-456
    // This runs in parallel with user-123 jobs
})

pool.Submit("user-123", func(ctx context.Context) {
    // Another job for user-123
    // This waits for the first user-123 job to complete
})
```

## Features

- **Per-key Serialization**: Jobs with the same key execute in order
- **Cross-key Parallelism**: Different keys execute in parallel
- **Context Support**: Jobs receive a context for cancellation
- **Graceful Shutdown**: `Stop()` cancels running jobs and waits for completion
- **Thread-safe**: Safe for concurrent use from multiple goroutines
- **Efficient**: One goroutine per active key (not per job)

## Design

Each unique key gets its own:
- Dedicated goroutine (worker)
- Buffered channel (100 jobs capacity)
- Context for cancellation

Workers are created on-demand when the first job for a key is submitted and remain active until `Stop()` is called.

## API

### NewTaskPool

```go
func NewTaskPool() *TaskPool
```

Creates a new TaskPool instance.

### Start

```go
func (tp *TaskPool) Start()
```

Initializes the task pool. Currently a no-op for interface consistency (workers start on-demand).

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

### Len

```go
func (tp *TaskPool) Len() int
```

Returns the number of active key queues (for testing/monitoring).

## Example: Notification System

```go
// Use taskpool to serialize notifications per user
pool := taskpool.NewTaskPool()
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

- **Memory**: ~2KB per active key (goroutine + channel buffer)
- **Latency**: Minimal overhead (channel send/receive)
- **Throughput**: Limited only by job execution time

## Comparison with Alternatives

| Approach | Goroutines | Order Guarantee | Cross-key Parallel |
|----------|------------|-----------------|-------------------|
| taskpool | 1 per key | ✓ | ✓ |
| Lock per key | Variable | ✓ | ✓ |
| Global lock | Variable | ✓ | ✗ |
| Goroutine per job | 1 per job | ✗ | ✓ |

TaskPool provides the best balance of resource usage and execution guarantees.
