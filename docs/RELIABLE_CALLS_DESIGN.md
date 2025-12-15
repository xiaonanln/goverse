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
6. **Status Tracking**: Each call has a clear lifecycle: pending → success/failed

## Architecture

### Components

The reliable calls system consists of several key components:

```
┌─────────────────────────────────────────────────────────────┐
│                     Application Layer                        │
│           (goverseapi.ReliableCallObject)                    │
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
│     - Receives ReliableCallObject RPC requests               │
│     - Triggers processing of pending reliable calls          │
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
│     - status (pending/success/failed)                       │
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
     │ 3. ReliableCallObject RPC    │                              │
     │─────────────────────────────>│                              │
     │   (triggers pending calls)   │                              │
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
     │                              │    (success/failed)          │
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
   - Caller routes the call to the target node via `ReliableCallObject()` RPC
   - This RPC triggers the target node to process pending reliable calls for the object
   - Cluster layer determines which node hosts the target object
   - Call is sent via gRPC to the target node

4. **Pending Call Retrieval**
   - Target node, upon receiving the `ReliableCallObject` RPC:
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
   - Status becomes "success" (with `result_data`) or "failed" (with `error_message`)

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

// 3. Route call to target node via ReliableCallObject RPC
// This triggers the target node to process pending calls
result, err := cluster.This().ReliableCallObject(
    ctx,
    "TargetType",
    "target-obj-123",
)

// Target node receives ReliableCallObject RPC and:
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
            "success",
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

2. **On Receiving ReliableCallObject RPC**
   - Whenever a node receives a `ReliableCallObject` RPC for an object, it triggers processing of pending calls
   - The RPC does not carry the method/request data itself - that data is already stored in the database
   - The node fetches pending calls from the database and executes them in order
   - Returns the result of the call that was just registered (identified by `call_id`)

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

2. **Object Crash Before Save**
   - If an object crashes (or the node crashes) after executing a reliable call but before saving state, the call status remains "pending" in the database
   - Upon recovery, the object's `nextRcseq` still points to before the call (since state wasn't persisted)
   - The call will be re-executed when the object is activated again
   - This ensures no reliable call is lost, even if the result wasn't persisted
   - **Important**: Methods invoked via reliable calls should be idempotent or designed to handle re-execution gracefully

3. **Network Partition**
   - If the caller cannot reach the target node, the call remains "pending"
   - When connectivity is restored, the target node processes the call

4. **Database Failure**
   - If the database is unavailable, calls cannot be registered
   - This is intentional: without persistence, exactly-once guarantees cannot be provided

5. **Retry Safety**
   - Retrying a call with the same `call_id` is safe
   - The deduplication mechanism ensures no duplicate execution
   - The cached result is returned if the call was already successful

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
    
    -- Response data (populated when status = 'success')
    result_data BYTEA,
    
    -- Error message (populated when status = 'failed')
    error_message TEXT,
    
    -- Processing status with state machine enforcement
    status VARCHAR(50) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'success', 'failed')),
    
    -- Timestamps for lifecycle tracking
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Constraints to enforce data integrity
    CONSTRAINT valid_success_state CHECK (
        status != 'success' OR result_data IS NOT NULL
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
| `status` | VARCHAR(50) | Call status: pending, success, failed |
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

// Reliable call with exactly-once semantics
result, err := goverseapi.ReliableCallObject(
    ctx,
    "OrderProcessor",
    "order-123",
    "ProcessPayment",
    request,
)
```

The cluster layer handles:
- Inserting the call into the database with a unique `call_id`
- Routing the `ReliableCallObject` RPC to the correct node
- Triggering the target node to process pending calls
- Returning the result of the specified call

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

## Implementation Plan

This section outlines a series of incremental PRs to implement the reliable calls feature. Each PR is small, focused, and independently testable.

### PR 1: Database Schema and Basic CRUD Operations ✅ DONE

**Focus**: Create the database table and implement basic persistence operations.

**Changes**:
- Add `goverse_reliable_calls` table schema to `util/postgres/db.go`
- Implement `InsertOrGetReliableCall()` in `util/postgres/persistence.go`
- Implement `GetReliableCall()` for retrieving a single call by `call_id`
- Implement `UpdateReliableCallStatus()` for status updates
- Add integration tests for all CRUD operations

**Tests**:
- Test insert new call returns pending status
- Test duplicate insert returns existing record (deduplication)
- Test status update from pending to success/failed
- Test constraints (success requires result_data, failed requires error_message)

**Status**: Already implemented in `util/postgres/db.go` and `util/postgres/persistence.go`.

---

### PR 2: Pending Calls Query and Sequential Processing Support ✅ DONE

**Focus**: Add ability to fetch pending calls for an object with ordering.

**Changes**:
- Implement `GetPendingReliableCalls(objectID, nextRcseq)` in `util/postgres/persistence.go`
- Add index for efficient pending call queries
- Add integration tests for sequential retrieval

**Tests**:
- Test fetching pending calls returns correct order by `seq`
- Test `nextRcseq` filtering excludes already processed calls
- Test empty result when no pending calls exist
- Test multiple objects have isolated pending queues

**Status**: Already implemented in `util/postgres/persistence.go`.

---

### PR 3: PersistenceProvider Interface Extension ✅ DONE

**Focus**: Extend the `PersistenceProvider` interface with reliable call methods.

**Changes**:
- Add reliable call methods to `PersistenceProvider` interface in `object/persistence.go`
- Define `ReliableCall` struct in `object/persistence.go`
- Implement interface methods in `PostgresPersistenceProvider`
- Update mock persistence provider for testing

**Tests**:
- Test `PostgresPersistenceProvider` implements all interface methods
- Test mock provider works correctly for unit tests

**Status**: Already implemented in `object/persistence.go` and `util/postgres/provider.go`.

---

### PR 4: Add ReliableCallObject gRPC Definition ✅ DONE

**Focus**: Define the new `ReliableCallObject` RPC in the protobuf schema.

**Changes**:
- Add `ReliableCallObject` RPC to `proto/goverse.proto`
- Define `ReliableCallObjectRequest` message (seq, object_id, object_type)
- Define `ReliableCallObjectResponse` message (result, error)
- Run `compile-proto.sh` to generate Go code

**Tests**:
- Verify generated gRPC code compiles
- Test request/response serialization

**Status**: Implemented in `proto/goverse.proto` with `ReliableCallObject` RPC and corresponding request/response messages.

---

### PR 5: High-Level API: goverseapi.GenerateCallID ✅ DONE

**Focus**: Expose call ID generation through the high-level API for users to generate unique call IDs.

**Changes**:
- Add `GenerateCallID()` to `goverseapi/goverseapi.go`
- Delegate to `util/uniqueid.UniqueId()` internally
- Document usage for reliable call deduplication

**Tests**:
- Test generated IDs are unique across multiple calls
- Test ID format is valid and consistent
- Test concurrent generation produces no duplicates

**Status**: Implemented in `goverseapi/goverseapi.go` with comprehensive tests in `goverseapi/goverseapi_test.go`.

---

### PR 6: Server-Level ReliableCallObject RPC Handler ✅ DONE

**Focus**: Wire up the gRPC server to accept incoming `ReliableCallObject` requests.

**Changes**:
- Implement `ReliableCallObject` in `server/server.go`
- Route RPC to node's handler method
- Handle errors and return appropriate responses
- Validate request parameters (seq, object_type, object_id)
- Validate object shard ownership

**Tests**:
- Integration test: gRPC client can call `ReliableCallObject`
- Test error handling for missing parameters

**Status**: Implemented in `server/server.go` with full parameter validation and routing to `Node.ReliableCallObject()`.

---

### PR 7: Node-Level ReliableCallObject Handler (Fetch and Execute) ✅ DONE

**Focus**: Implement the node handler that fetches and executes pending calls.

**Changes**:
- Add `ReliableCallObject()` method to `node/node.go`
- Fetch pending calls from database for the target object
- Execute pending calls sequentially in order by `seq`
- Activate object if not already active (auto-create if needed)
- Trigger `ProcessPendingReliableCalls` on the object
- Wait for the specific seq to be processed via channel

**Tests**:
- Test handler fetches pending calls in correct order
- Test calls are executed sequentially
- Test object is activated if needed

**Status**: Implemented in `node/node.go` with full object lifecycle handling and channel-based result synchronization.

---

### PR 8: Update Call Status After Execution ✅ DONE

**Focus**: Update the reliable call status in the database after execution.

**Changes**:
- After successful execution, update status to `success` with result data
- After failed execution, update status to `failed` with error message
- Status updates handled in `BaseObject.ProcessPendingReliableCalls()`
- Uses `UpdateReliableCallStatus()` from persistence provider

**Tests**:
- Test status is updated to success on success
- Test status is updated to failed on error
- Test result_data/error_message are stored correctly

**Status**: Implemented in `object/object.go` within the `ProcessPendingReliableCalls` goroutine.

---

### PR 9: Track nextRcseq on Object ✅ DONE

**Focus**: Track which reliable calls have been processed to prevent re-execution.

**Changes**:
- Add `nextRcseq` field to `BaseObject` (atomic.Int64)
- Add `GetNextRcseq()` and `SetNextRcseq()` methods
- Update `nextRcseq` after processing each reliable call
- Ensure `nextRcseq` is persisted with object state
- Support in `SaveObject()` and `LoadObject()` functions

**Tests**:
- Test `nextRcseq` increments after each call
- Test `nextRcseq` is persisted correctly
- Test pending calls query uses `nextRcseq` to filter

**Status**: Implemented in `object/object.go` with persistence support in `object/persistence.go` and comprehensive tests in `object/persistence_nextrcseq_test.go`.

---

### PR 10: Cluster-Level ReliableCallObject (Insert + Route) ✅ DONE

**Focus**: Implement `cluster.ReliableCallObject()` which inserts the call to DB and routes to the target node.

**Changes**:
- Add `ReliableCallObject()` method to `cluster/cluster.go`
- Insert call to database via persistence provider (deduplication via `call_id`)
- Implement shard-based lookup to determine target node
- Forward `ReliableCallObject` RPC to target node via gRPC
- Handle local execution when object is on same node
- Return result from RPC response (no DB polling needed)
- Uses `Node.InsertOrGetReliableCall()` for database operations
- Routes via `GetCurrentNodeForObject()` for shard determination

**Tests**:
- Test call is inserted to DB before routing
- Test routing selects correct node based on object shard
- Test call is forwarded when object is on remote node
- Test local execution when object is on same node
- Test duplicate `call_id` returns existing result without re-execution

**Status**: Implemented in `cluster/cluster.go` with complete routing logic for both local and remote execution.

---

### PR 11: Route Reliable Call Result Back to Caller ✅ DONE

**Focus**: Ensure the reliable call result is returned directly to the caller via RPC response.

**Changes**:
- Node executes pending calls and returns result in `ReliableCallObjectResponse`
- Cluster layer receives result from RPC and returns to caller
- Caller receives result immediately without polling DB
- Handle error responses and propagate appropriately
- Uses channel-based synchronization in `Node.ReliableCallObject()`
- Converts between `anypb.Any` and `proto.Message` using protohelper

**Tests**:
- Test successful result is returned to caller via RPC
- Test error result is returned to caller via RPC
- Test caller does not need to query DB for result
- Test result is also persisted in DB for crash recovery

**Status**: Implemented in `cluster/cluster.go` and `node/node.go` with proper result routing via gRPC.

---

### PR 12: High-Level API: goverseapi.ReliableCallObject

**Focus**: Expose reliable call functionality through the high-level API for external callers (gates, clients).

**Changes**:
- Add `ReliableCallObject()` to `goverseapi/api.go`
- Generate `call_id` if not provided
- Call `cluster.ReliableCallObject()` which handles DB insert + routing
- Return result from cluster call

**Tests**:
- End-to-end test: external caller invokes object method via `ReliableCallObject`
- Test deduplication: same `call_id` returns cached result
- Test call survives node restart (persistence)

---

### PR 13: High-Level API for Initiating Reliable Calls (Object-to-Object)

**Focus**: Create the `goverseapi.ReliableCall()` API for user logic within an object to initiate a reliable call to another object.

**Changes**:
- Add `ReliableCall()` to `goverseapi/api.go`
- Generate unique `call_id` if not provided
- Call `cluster.ReliableCallObject()` which handles DB insert + routing
- Return result from executed call

**Tests**:
- Test initiating reliable call from object method via `goverseapi.ReliableCall()`
- Test call is persisted before routing
- Test deduplication when same `call_id` is used

---

### PR 14: Object Activation Processes Pending Calls ✅ DONE

**Focus**: Automatically process pending reliable calls when an object is created/activated.

**Changes**:
- Modify object activation in `node/node.go` to fetch pending calls
- Execute pending calls in order before returning activated object
- Update `nextRcseq` after processing
- Triggers `ProcessPendingReliableCalls()` after object creation
- Uses `math.MaxInt64` to ensure all pending calls are processed

**Tests**:
- Test object activation processes pending calls
- Test calls made while object was inactive are executed on activation
- Test `nextRcseq` prevents reprocessing on subsequent activations

**Status**: Implemented in `node/node.go` in the object creation flow after successful object construction.

---

### PR 15: Crash Recovery - Re-execute on Unsaved State

**Focus**: Ensure calls are re-executed if object crashes before saving state.

**Changes**:
- Ensure `nextRcseq` is only updated when object state is persisted
- On recovery, pending calls with `seq >= persisted_nextRcseq` are re-executed
- Add documentation for idempotency requirements

**Tests**:
- Test: crash before save → call re-executes on recovery
- Test: crash after save → call not re-executed
- Test: multiple pending calls all re-execute correctly

---

### Summary

| PR | Focus | Key Deliverable | Status |
|----|-------|------------------|--------|
| 1 | Database Schema | Table + basic CRUD | ✅ Done |
| 2 | Pending Calls Query | Sequential retrieval | ✅ Done |
| 3 | Interface Extension | `PersistenceProvider` methods | ✅ Done |
| 4 | gRPC Definition | `ReliableCallObject` proto | ✅ Done |
| 5 | Call ID Generation API | `goverseapi.GenerateCallID()` | ✅ Done |
| 6 | Server RPC Handler | Wire up gRPC endpoint | ✅ Done |
| 7 | Node Handler | Fetch and execute pending calls | ✅ Done |
| 8 | Status Update | Mark success/failed in DB | ✅ Done |
| 9 | nextRcseq Tracking | Prevent re-execution | ✅ Done |
| 10 | Cluster Insert + Route | `cluster.ReliableCallObject()` | ✅ Done |
| 11 | Result Routing | Return result via RPC to caller | ✅ Done |
| 12 | External Caller API | `goverseapi.ReliableCallObject()` | Not Started |
| 13 | Object-to-Object API | `goverseapi.ReliableCall()` | Not Started |
| 14 | Object Activation | Process on create/activate | ✅ Done |
| 15 | Crash Recovery | Re-execute unsaved calls | Not Started |

Each PR builds on the previous one, allowing incremental development and testing. PRs 1-11 and 14 have been completed, providing the core infrastructure for reliable calls including database layer, gRPC endpoints, node execution, cluster routing, and result return. PRs 12-13 (high-level public APIs) and PR 15 (crash recovery enhancements) remain to be implemented.

## Conclusion

Reliable calls provide a robust foundation for exactly-once semantics in Goverse's distributed object runtime. The current implementation offers:

✅ **Deduplication**: Prevents duplicate call execution  
✅ **Persistence**: Survives node failures and restarts  
✅ **Recovery**: Incremental processing via `nextRcseq`  
✅ **Fault-Tolerance**: Handles crashes and network issues  
✅ **Simple API**: Easy to integrate and use  
✅ **Production-Ready**: Tested with PostgreSQL backend

The modular design with the `PersistenceProvider` interface makes future enhancements straightforward to implement without breaking existing functionality.

## References

- **Interface Definition**: `object/persistence.go`
- **Database Operations**: `util/postgres/persistence.go`
- **Schema Initialization**: `util/postgres/db.go`
- **Provider Implementation**: `util/postgres/provider.go`
- **Integration Tests**: `util/postgres/persistence_integration_test.go`
- **Persistence Guide**: `docs/PERSISTENCE_IMPLEMENTATION.md`
- **PostgreSQL Setup**: `docs/POSTGRES_SETUP.md`
