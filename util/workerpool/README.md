# Worker Pool

A robust fixed-size goroutine pool implementation for concurrent task execution.

## Features

- **Fixed Pool Size**: Creates a fixed number of worker goroutines upfront
- **Resource Efficiency**: Reuses goroutines instead of spawning one per task
- **Context Support**: Respects context cancellation for graceful shutdown
- **Flexible Submission**: Submit individual tasks or batches
- **Graceful Shutdown**: `Stop()` waits for completion, `StopNow()` cancels immediately

## Usage

### Basic Example

```go
package main

import (
    "context"
    "fmt"
    "github.com/xiaonanln/goverse/util/workerpool"
)

func main() {
    // Create a pool with 10 workers
    ctx := context.Background()
    pool := workerpool.New(ctx, 10)
    pool.Start()
    defer pool.Stop()

    // Submit a single task
    resultChan := pool.Submit(func(ctx context.Context) error {
        fmt.Println("Task executed")
        return nil
    })
    
    // Wait for result
    err := <-resultChan
    if err != nil {
        fmt.Println("Task failed:", err)
    }
}
```

### Batch Processing

```go
// Create tasks
tasks := make([]workerpool.Task, 100)
for i := 0; i < 100; i++ {
    idx := i
    tasks[i] = func(ctx context.Context) error {
        fmt.Printf("Processing item %d\n", idx)
        return nil
    }
}

// Execute all tasks
ctx := context.Background()
results := pool.SubmitAndWait(ctx, tasks)

// Check results
for i, result := range results {
    if result.Err != nil {
        fmt.Printf("Task %d failed: %v\n", i, result.Err)
    }
}
```

## Performance Benefits

### Goroutine Efficiency

The worker pool dramatically reduces goroutine creation overhead:

| Number of Tasks | Worker Pool Goroutines | Semaphore Goroutines | Improvement |
|-----------------|------------------------|---------------------|-------------|
| 10              | 20                     | 10                  | 2x worse    |
| 100             | 20                     | 100                 | 5x better   |
| 1,000           | 20                     | 1,000               | 50x better  |
| 8,192           | 20                     | 8,192               | 409x better |

### Memory Efficiency

For 8,192 tasks:
- **Worker Pool**: ~2.2 MB allocated
- **Semaphore**: ~1.2 MB allocated

While the semaphore approach uses less memory for the task structures themselves, the worker pool provides:
- Predictable resource usage
- No goroutine scheduling overhead for thousands of goroutines
- Better CPU cache efficiency with fixed workers
- Bounded memory growth regardless of task count

## Use Cases

The worker pool is ideal for:

1. **High Task Count**: Processing thousands of tasks (e.g., storing 8,192 shards in etcd)
2. **Resource Constraints**: Limiting concurrent operations to prevent overwhelming external systems
3. **Predictable Performance**: Fixed worker count ensures consistent resource usage
4. **Long-Running Operations**: Reusing goroutines reduces creation/destruction overhead

## API Reference

### Creating a Pool

```go
pool := workerpool.New(ctx, numWorkers)
```

Creates a new worker pool with the provided context as the base context. If `numWorkers <= 0`, defaults to 1.

### Starting the Pool

```go
pool.Start()
```

Initializes and starts all worker goroutines. Must be called before submitting tasks.

### Submitting Tasks

#### Single Task

```go
resultChan := pool.Submit(task)
err := <-resultChan
```

Submit a single task and receive a channel for the result.

#### Batch Tasks

```go
results := pool.SubmitAndWait(ctx, tasks)
```

Submit multiple tasks and wait for all to complete. Returns results in completion order (not submission order).

### Shutdown

#### Graceful Shutdown

```go
pool.Stop()
```

Closes the task channel and waits for all workers to finish their current tasks.

#### Immediate Shutdown

```go
pool.StopNow()
```

Cancels the pool's context, causing workers to exit immediately. Current tasks may not complete.

## Implementation in GoVerse

The worker pool is used in `cluster/consensusmanager/consensusmanager.go` for the `storeShardMapping` function, which stores up to 8,192 shards in etcd concurrently. This replaced the previous semaphore-based approach, providing:

- **409x reduction** in goroutine count (20 vs 8,192)
- **Predictable resource usage** regardless of shard count
- **Better error handling** with structured result collection
- **Cleaner code** with improved readability
