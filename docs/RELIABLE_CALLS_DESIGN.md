# Reliable Calls Design Document

## Overview

Reliable calls provide **exactly-once semantics** for inter-object calls in Goverse's distributed runtime. This feature ensures that remote method invocations between objects are processed exactly once, even in the presence of failures, retries, or network issues.

### Purpose

In distributed systems, achieving exactly-once semantics is crucial for maintaining consistency and preventing duplicate processing. Reliable calls solve this problem by:

- **Deduplication**: Ensuring each call is executed only once, even if retried
- **Persistence**: Storing call state in a durable database
- **Recovery**: Enabling call processing to resume after node failures
- **Fault-Tolerance**: Surviving node crashes and network partitions

### Key Features

1. **Exactly-Once Guarantee**: Each call identified by a unique `call_id` is processed exactly once
2. **Automatic Deduplication**: Duplicate calls with the same `call_id` return cached results
3. **Crash Recovery**: Pending calls survive node crashes and are processed after recovery
4. **Sequential Processing**: Calls are executed in order using the `seq` field
5. **Incremental Processing**: Objects track progress via `nextRcseq` to avoid reprocessing
6. **Status Tracking**: Each call has a clear lifecycle: pending → processing → completed/failed

## Architecture

### Components

The reliable calls system consists of several key components:

```
┌─────────────────────────────────────────────────────────────┐
│                     Application Layer                        │
│              (goverseapi.CallObject)                         │
└────────────────────┬────────────────────────────────────────┘
                     │
                     │ Initiates reliable call
                     │
┌────────────────────▼────────────────────────────────────────┐
│                  Cluster Layer                               │
│     - Routes calls to target nodes                           │
│     - Handles node-to-node communication                     │
└────────────────────┬────────────────────────────────────────┘
                     │
                     │ Invokes via gRPC
                     │
┌────────────────────▼────────────────────────────────────────┐
│                   Node Layer                                 │
│     - Receives CallObject RPC requests                       │
│     - Processes pending reliable calls                       │
│     - Updates call status                                    │
└────────────────────┬────────────────────────────────────────┘
                     │
                     │ Uses persistence provider
                     │
┌────────────────────▼────────────────────────────────────────┐
│            PersistenceProvider Interface                     │
│     - InsertOrGetReliableCall()                             │
│     - GetPendingReliableCalls()                             │
│     - UpdateReliableCallStatus()                            │
│     - GetReliableCall()                                     │
└────────────────────┬────────────────────────────────────────┘
                     │
                     │ Implemented by
                     │
┌────────────────────▼────────────────────────────────────────┐
│       PostgresPersistenceProvider                            │
│     - Adapts PersistenceProvider to PostgreSQL              │
│     - Delegates to DB layer                                  │
└────────────────────┬────────────────────────────────────────┘
                     │
                     │ Uses
                     │
┌────────────────────▼────────────────────────────────────────┐
│              PostgreSQL Database                             │
│     Table: goverse_reliable_calls                            │
│     - seq (BIGSERIAL, ordering)                             │
│     - call_id (VARCHAR, PRIMARY KEY)                        │
│     - object_id, object_type, method_name                   │
│     - request_data, result_data, error_message              │
│     - status (pending/processing/completed/failed)          │
│     - created_at, updated_at                                │
└─────────────────────────────────────────────────────────────┘
```

### Dependencies

- **PostgreSQL**: Required for persistent storage of reliable calls
- **PersistenceProvider**: Interface that enables alternative storage backends
- **gRPC**: Used for inter-node communication in the cluster

### File References

Key files implementing reliable calls:

- **Interface**: `object/persistence.go` - Defines `ReliableCall` struct and `PersistenceProvider` interface
- **Database Operations**: `util/postgres/persistence.go` - Implements CRUD operations for reliable calls
- **Provider**: `util/postgres/provider.go` - Adapter implementing `PersistenceProvider` interface
- **Schema**: `util/postgres/db.go` - Contains `InitSchema()` with table creation
- **Tests**: `util/postgres/persistence_integration_test.go` - Integration tests demonstrating usage

## Process Flow

### Detailed Call Flow

```
Caller Node                    Target Node                    Database
     │                              │                              │
     │ 1. Generate unique call_id   │                              │
     │    (e.g., UUID)              │                              │
     │                              │                              │
     │ 2. InsertOrGetReliableCall() │                              │
     │────────────────────────────────────────────────────────────>│
     │                              │                              │
     │<─ Return ReliableCall ───────────────────────────────────────│
     │   (status: pending)          │                              │
     │                              │                              │
     │ 3. CallObject RPC            │                              │
     │─────────────────────────────>│                              │
     │                              │                              │
     │                              │ 4. GetPendingReliableCalls() │
     │                              │─────────────────────────────>│
     │                              │                              │
     │                              │<── Pending calls (seq order)─│
     │                              │                              │
     │                              │ 5. Execute call sequentially │
     │                              │    (invoke object method)    │
     │                              │                              │
     │                              │ 6. UpdateReliableCallStatus()│
     │                              │    (completed/failed)        │
     │                              │─────────────────────────────>│
     │                              │                              │
     │                              │ 7. Increment nextRcseq       │
     │                              │    (track progress)          │
     │                              │                              │
     │<── Return result ────────────│                              │
     │                              │                              │
```

### Step-by-Step Process

1. **Call Initiation**
   - Caller generates a unique `call_id` (typically a UUID)
   - This `call_id` serves as the deduplication key

2. **Call Registration**
   - Caller invokes `InsertOrGetReliableCall()` with:
     - `call_id`: Unique identifier
     - `object_id`: Target object
     - `object_type`: Type of target object
     - `method_name`: Method to invoke
     - `request_data`: Serialized request payload
   - Database inserts new call with status "pending"
   - If `call_id` already exists, returns existing record (deduplication)

3. **Call Routing**
   - Caller routes the call to the target node via `CallObject()` RPC
   - Cluster layer determines which node hosts the target object
   - Call is sent via gRPC to the target node

4. **Pending Call Retrieval**
   - Target node, upon receiving a call OR during object activation:
     - Fetches pending calls via `GetPendingReliableCalls(objectID, nextRcseq)`
     - Returns calls with `seq > nextRcseq` and `status = pending`
     - Results are ordered by `seq` (ascending)

5. **Call Execution**
   - Target node processes pending calls sequentially
   - For each call:
     - Deserializes `request_data`
     - Invokes the object's method
     - Captures result or error

6. **Status Update**
   - After execution, calls `UpdateReliableCallStatus(seq, status, result, error)`
   - Status becomes "completed" (with `result_data`) or "failed" (with `error_message`)

7. **Progress Tracking**
   - Object increments its `nextRcseq` to the processed call's `seq`
   - This prevents reprocessing the same call on future retrievals

### Example Code Flow

```go
// 1. Caller generates unique call ID
callID := uniqueid.Gen()

// 2. Register the call in database
rc, err := provider.InsertOrGetReliableCall(
    ctx,
    callID,
    "target-obj-123",
    "TargetType",
    "ProcessOrder",
    requestData,
)

// 3. Route call to target node (handled by cluster)
result, err := cluster.This().CallObject(
    ctx,
    "TargetType",
    "target-obj-123",
    "ProcessOrder",
    request,
)

// Target node receives the call and:
// 4. Fetches pending calls
pending, err := provider.GetPendingReliableCalls(
    ctx,
    objectID,
    obj.nextRcseq,
)

// 5. Processes each call
for _, call := range pending {
    result, err := obj.executeMethod(call.MethodName, call.RequestData)
    
    // 6. Updates status
    if err != nil {
        provider.UpdateReliableCallStatus(
            ctx,
            call.Seq,
            "failed",
            nil,
            err.Error(),
        )
    } else {
        provider.UpdateReliableCallStatus(
            ctx,
            call.Seq,
            "completed",
            result,
            "",
        )
    }
    
    // 7. Track progress
    obj.nextRcseq = call.Seq
}
```

## Key Behaviors

### Execution Triggers

Pending reliable calls are processed in two scenarios:

1. **On Object Activation**
   - When an object is activated (loaded into memory), the node fetches and processes all pending calls
   - Ensures calls made while the object was inactive are not lost

2. **On Receiving New Calls**
   - Whenever an object receives a `CallObject` RPC, it checks for pending calls
   - Processes any pending calls before handling the new request
   - Maintains sequential ordering of operations

### Deduplication

The `call_id` field ensures deduplication:

- **Primary Key**: `call_id` is the PRIMARY KEY in the database
- **Idempotent Insert**: `InsertOrGetReliableCall()` uses `INSERT ... ON CONFLICT DO NOTHING`
- **Returns Existing**: If `call_id` exists, the existing record is returned
- **No Duplicate Execution**: Only the first insert creates a pending call; subsequent attempts with the same `call_id` return the cached result

Example:
```sql
INSERT INTO goverse_reliable_calls (call_id, ...) 
VALUES ($1, ...) 
ON CONFLICT (call_id) DO NOTHING 
RETURNING ...
```

### Recovery

Incremental recovery via `nextRcseq`:

- **Progress Tracking**: Each object maintains a `nextRcseq` field
- **Incremental Fetch**: `GetPendingReliableCalls(objectID, nextRcseq)` only returns calls with `seq > nextRcseq`
- **Avoids Reprocessing**: Already processed calls (with `seq <= nextRcseq`) are skipped
- **Efficient**: Reduces database queries and prevents duplicate work

Example query:
```sql
SELECT * FROM goverse_reliable_calls 
WHERE object_id = $1 AND seq > $2 AND status = 'pending' 
ORDER BY seq ASC
```

### Fault-Tolerance

Reliable calls survive failures:

1. **Node Crash During Call**
   - Call remains in "pending" status in the database
   - When the object is activated on another node (or the same node after restart), pending calls are fetched and processed

2. **Network Partition**
   - If the caller cannot reach the target node, the call remains "pending"
   - When connectivity is restored, the target node processes the call

3. **Database Failure**
   - If the database is unavailable, calls cannot be registered
   - This is intentional: without persistence, exactly-once guarantees cannot be provided

4. **Retry Safety**
   - Retrying a call with the same `call_id` is safe
   - The deduplication mechanism ensures no duplicate execution
   - The cached result is returned if the call was already completed

## Database Schema

### Table: `goverse_reliable_calls`

```sql
CREATE TABLE IF NOT EXISTS goverse_reliable_calls (
    -- Auto-incrementing integer sequence for ordering and reference
    seq BIGSERIAL UNIQUE,
    
    -- Unique identifier for the call (PRIMARY KEY for deduplication)
    call_id VARCHAR(255) PRIMARY KEY,
    
    -- Target object information
    object_id VARCHAR(255) NOT NULL,
    object_type VARCHAR(255) NOT NULL,
    method_name VARCHAR(255) NOT NULL,
    
    -- Request payload (serialized protobuf or JSON)
    request_data BYTEA NOT NULL,
    
    -- Response data (populated when status = 'completed')
    result_data BYTEA,
    
    -- Error message (populated when status = 'failed')
    error_message TEXT,
    
    -- Processing status with state machine enforcement
    status VARCHAR(50) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    
    -- Timestamps for lifecycle tracking
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints to enforce data integrity
    CONSTRAINT valid_completed_state CHECK (
        status != 'completed' OR result_data IS NOT NULL
    ),
    CONSTRAINT valid_failed_state CHECK (
        status != 'failed' OR error_message IS NOT NULL
    )
);
```

### Indexes

```sql
-- Primary index for finding pending calls by object
CREATE INDEX IF NOT EXISTS idx_goverse_reliable_calls_object_status 
    ON goverse_reliable_calls(object_id, status);
```

**Why this index?**
- **Primary Query Pattern**: Fetching pending calls for a specific object
- **Query**: `WHERE object_id = $1 AND seq > $2 AND status = 'pending'`
- **Performance**: Enables fast lookup of pending calls per object

### Automatic Timestamp Update

```sql
-- Trigger function to automatically update updated_at timestamp
CREATE OR REPLACE FUNCTION update_goverse_reliable_calls_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to update timestamp on row updates
CREATE TRIGGER trigger_update_goverse_reliable_calls_timestamp
    BEFORE UPDATE ON goverse_reliable_calls
    FOR EACH ROW
    EXECUTE FUNCTION update_goverse_reliable_calls_timestamp();
```

### Field Descriptions

| Field | Type | Description |
|-------|------|-------------|
| `seq` | BIGSERIAL | Auto-incrementing sequence for ordering; enables incremental processing via `nextRcseq` |
| `call_id` | VARCHAR(255) | Unique call identifier (PRIMARY KEY); used for deduplication |
| `object_id` | VARCHAR(255) | ID of the target object (e.g., "Order-123") |
| `object_type` | VARCHAR(255) | Type of the target object (e.g., "OrderProcessor") |
| `method_name` | VARCHAR(255) | Name of the method to invoke (e.g., "ProcessPayment") |
| `request_data` | BYTEA | Serialized request payload (protobuf or JSON) |
| `result_data` | BYTEA | Serialized result (populated on completion) |
| `error_message` | TEXT | Error description (populated on failure) |
| `status` | VARCHAR(50) | Call status: pending, processing, completed, failed |
| `created_at` | TIMESTAMP | When the call was first registered |
| `updated_at` | TIMESTAMP | Last modification time (auto-updated) |

### Schema Initialization

The schema is automatically created by calling:

```go
db, err := postgres.NewDB(config)
if err != nil {
    return err
}

ctx := context.Background()
err = db.InitSchema(ctx)
if err != nil {
    return err
}
```

Reference: `util/postgres/db.go` (lines 58-123)

## API Integration

### PersistenceProvider Interface

The reliable calls API is defined in `object/persistence.go`:

```go
type PersistenceProvider interface {
    // Object persistence methods
    SaveObject(ctx context.Context, objectID, objectType string, data []byte) error
    LoadObject(ctx context.Context, objectID string) ([]byte, error)
    DeleteObject(ctx context.Context, objectID string) error

    // ReliableCall methods for exactly-once semantics
    InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, 
        objectType string, methodName string, requestData []byte) (*ReliableCall, error)
    UpdateReliableCallStatus(ctx context.Context, seq int64, status string, 
        resultData []byte, errorMessage string) error
    GetPendingReliableCalls(ctx context.Context, objectID string, 
        nextRcseq int64) ([]*ReliableCall, error)
    GetReliableCall(ctx context.Context, requestID string) (*ReliableCall, error)
}
```

### ReliableCall Struct

```go
type ReliableCall struct {
    Seq         int64
    CallID      string
    ObjectID    string
    ObjectType  string
    MethodName  string
    RequestData []byte
    ResultData  []byte
    Error       string
    Status      string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### High-Level API

Applications interact with reliable calls through `goverseapi`:

```go
import "github.com/xiaonanln/goverse/goverseapi"

// Standard call (uses reliable calls internally if configured)
result, err := goverseapi.CallObject(
    ctx,
    "OrderProcessor",
    "order-123",
    "ProcessPayment",
    request,
)
```

The cluster layer handles:
- Routing to the correct node
- Invoking the target object's method
- Managing reliable call lifecycle (if persistence is configured)

### Implementation Example

Creating a PostgreSQL persistence provider:

```go
import (
    "github.com/xiaonanln/goverse/util/postgres"
)

// 1. Configure database connection
config := &postgres.Config{
    Host:     "localhost",
    Port:     5432,
    User:     "goverse",
    Password: "goverse",
    Database: "goverse",
    SSLMode:  "disable",
}

// 2. Create database connection
db, err := postgres.NewDB(config)
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// 3. Initialize schema (creates tables and indexes)
ctx := context.Background()
err = db.InitSchema(ctx)
if err != nil {
    log.Fatal(err)
}

// 4. Create persistence provider
provider := postgres.NewPostgresPersistenceProvider(db)

// 5. Use provider with nodes/cluster
// (configuration depends on your setup)
```

### Testing

Integration tests demonstrate reliable call usage:

```go
// Test file: util/postgres/persistence_integration_test.go

func TestInsertOrGetReliableCall_Integration(t *testing.T) {
    // Setup database
    db, err := NewDB(config)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close()

    ctx := context.Background()
    err = db.InitSchema(ctx)
    if err != nil {
        t.Fatal(err)
    }

    // Insert a new call
    rc1, err := db.InsertOrGetReliableCall(
        ctx,
        "test-req-123",
        "test-obj-123",
        "TestType",
        "TestMethod",
        []byte("test-data"),
    )
    if err != nil {
        t.Fatal(err)
    }

    // Verify initial status
    if rc1.Status != "pending" {
        t.Fatalf("Expected status 'pending', got %s", rc1.Status)
    }

    // Duplicate call returns existing record
    rc2, err := db.InsertOrGetReliableCall(
        ctx,
        "test-req-123", // Same call_id
        "test-obj-123",
        "TestType",
        "TestMethod",
        []byte("test-data"),
    )
    if err != nil {
        t.Fatal(err)
    }

    if rc2.Seq != rc1.Seq {
        t.Fatal("Duplicate call should return same record")
    }
}
```

## Limitations and Future Improvements

### Current Limitations

1. **Synchronous Execution Only**
   - Calls are processed synchronously during object activation or method invocation
   - No background processing of pending calls
   - May delay object responses if many pending calls exist

2. **No Automatic Retry**
   - Failed calls remain in "failed" status indefinitely
   - No automatic retry mechanism for transient failures
   - Applications must implement their own retry logic

3. **Single Database Dependency**
   - Only PostgreSQL is currently implemented
   - No support for distributed databases or alternative backends
   - Database becomes a single point of failure

4. **No Call Expiration**
   - Pending calls never expire
   - Old, stale calls remain in the database forever
   - No automatic cleanup mechanism

5. **No Priority Ordering**
   - All calls are processed in strict `seq` order
   - No way to prioritize urgent calls
   - High-priority calls may wait behind low-priority ones

6. **Limited Batch Operations**
   - Calls are inserted one at a time
   - No bulk insert or update operations
   - May be inefficient for high-volume scenarios

7. **No Observability**
   - No built-in metrics for call processing
   - No monitoring of pending call queue depth
   - Limited visibility into system health

### Suggested Future Enhancements

#### 1. Asynchronous Processing

Add background workers to process pending calls:

```go
type ReliableCallProcessor struct {
    provider PersistenceProvider
    interval time.Duration
}

func (p *ReliableCallProcessor) Start(ctx context.Context) {
    ticker := time.NewTicker(p.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            p.processPendingCalls(ctx)
        case <-ctx.Done():
            return
        }
    }
}
```

Benefits:
- Objects respond faster (don't wait for pending calls)
- Decouples call processing from object activation
- Enables parallel processing of independent calls

#### 2. Automatic Retry with Exponential Backoff

Add retry logic for failed calls:

```go
type ReliableCall struct {
    // ... existing fields ...
    RetryCount    int
    NextRetryAt   time.Time
    MaxRetries    int
}

// Retry policy
if call.RetryCount < call.MaxRetries {
    nextRetry := time.Now().Add(backoff(call.RetryCount))
    UpdateReliableCallForRetry(ctx, call.Seq, nextRetry)
}
```

Benefits:
- Handles transient failures automatically
- Reduces manual intervention
- Improves reliability

#### 3. Call Expiration and Cleanup

Add TTL for pending calls:

```go
// Schema enhancement
ALTER TABLE goverse_reliable_calls 
ADD COLUMN expires_at TIMESTAMP;

// Cleanup query
DELETE FROM goverse_reliable_calls 
WHERE expires_at < CURRENT_TIMESTAMP 
AND status IN ('pending', 'failed');
```

Benefits:
- Prevents unbounded database growth
- Removes stale calls
- Improves query performance

#### 4. Priority-Based Processing

Add priority field for urgent calls:

```go
type ReliableCall struct {
    // ... existing fields ...
    Priority int  // 0 = normal, 1 = high, 2 = urgent
}

// Query with priority
SELECT * FROM goverse_reliable_calls 
WHERE object_id = $1 AND seq > $2 AND status = 'pending' 
ORDER BY priority DESC, seq ASC
```

Benefits:
- Critical calls processed first
- Better resource utilization
- Improved user experience

#### 5. Batch Operations

Add bulk insert and update:

```go
func (db *DB) InsertReliableCallsBatch(
    ctx context.Context, 
    calls []*ReliableCall,
) error {
    // Use PostgreSQL COPY or multi-row INSERT
}

func (db *DB) UpdateReliableCallStatusBatch(
    ctx context.Context, 
    updates []StatusUpdate,
) error {
    // Batch update with transaction
}
```

Benefits:
- Higher throughput
- Reduced database round trips
- Better performance under load

#### 6. Metrics and Observability

Add Prometheus metrics:

```go
var (
    callsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "goverse_reliable_calls_total",
            Help: "Total number of reliable calls",
        },
        []string{"status", "object_type"},
    )
    
    pendingCallsGauge = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "goverse_pending_reliable_calls",
            Help: "Number of pending reliable calls",
        },
        []string{"object_id"},
    )
    
    callProcessingDuration = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "goverse_call_processing_duration_seconds",
            Help: "Duration of reliable call processing",
        },
        []string{"object_type", "method"},
    )
)
```

Benefits:
- Real-time visibility into call processing
- Alerts for queue buildup
- Performance analysis

#### 7. Alternative Storage Backends

Implement additional providers:

- **Redis**: For low-latency, in-memory processing
- **DynamoDB**: For AWS-native deployments
- **MongoDB**: For document-oriented storage
- **Cassandra**: For high-throughput, distributed scenarios

Example interface remains the same:

```go
type RedisPersistenceProvider struct {
    client *redis.Client
}

func (r *RedisPersistenceProvider) InsertOrGetReliableCall(...) (*ReliableCall, error) {
    // Redis-specific implementation
}
```

Benefits:
- Flexibility in deployment
- Optimized for specific use cases
- Reduced vendor lock-in

#### 8. Transaction Support

Add transactional operations:

```go
func (db *DB) ExecuteInTransaction(
    ctx context.Context, 
    fn func(*sql.Tx) error,
) error {
    tx, err := db.conn.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    
    if err := fn(tx); err != nil {
        tx.Rollback()
        return err
    }
    
    return tx.Commit()
}

// Usage: Atomic multi-call operations
db.ExecuteInTransaction(ctx, func(tx *sql.Tx) error {
    for _, call := range calls {
        if err := insertCall(tx, call); err != nil {
            return err
        }
    }
    return nil
})
```

Benefits:
- Atomic multi-call operations
- Stronger consistency guarantees
- Simplified error handling

## Conclusion

Reliable calls provide a robust foundation for exactly-once semantics in Goverse's distributed object runtime. The current implementation offers:

✅ **Deduplication**: Prevents duplicate call execution  
✅ **Persistence**: Survives node failures and restarts  
✅ **Recovery**: Incremental processing via `nextRcseq`  
✅ **Fault-Tolerance**: Handles crashes and network issues  
✅ **Simple API**: Easy to integrate and use  
✅ **Production-Ready**: Tested with PostgreSQL backend

While the current implementation is solid, the suggested enhancements would further improve:
- Performance (async processing, batch ops)
- Reliability (auto-retry, transaction support)
- Observability (metrics, monitoring)
- Flexibility (alternative backends, priority processing)

The modular design with the `PersistenceProvider` interface makes these enhancements straightforward to implement without breaking existing functionality.

## References

- **Interface Definition**: `object/persistence.go`
- **Database Operations**: `util/postgres/persistence.go`
- **Schema Initialization**: `util/postgres/db.go`
- **Provider Implementation**: `util/postgres/provider.go`
- **Integration Tests**: `util/postgres/persistence_integration_test.go`
- **Persistence Guide**: `docs/PERSISTENCE_IMPLEMENTATION.md`
- **PostgreSQL Setup**: `docs/POSTGRES_SETUP.md`
