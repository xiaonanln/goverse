# Object Call Policies in Goverse

## Overview

This document explains the different semantic guarantees for object method calls in distributed systems and analyzes which policies are appropriate for Goverse's virtual actor model.

## Background: Call Semantics in Distributed Systems

In distributed systems, there are three primary semantic guarantees for remote procedure calls:

### 1. At-Least-Once Semantics

**Definition**: A call is delivered at least once, potentially multiple times if retries occur.

**Characteristics**:
- Automatic retry on transient failures (network issues, temporary unavailability)
- May result in duplicate calls if a previous attempt succeeded but the acknowledgment was lost
- Requires idempotent operations or application-level deduplication
- Provides high availability and fault tolerance

**Use Cases**:
- Idempotent operations (e.g., `SetValue`, `UpdateStatus`)
- Operations where duplicate execution is acceptable
- Systems prioritizing availability over consistency

**Example**:
```go
// Retry loop with exponential backoff
for attempt := 0; attempt < maxRetries; attempt++ {
    response, err := CallObject(ctx, objType, id, method, request)
    if err == nil {
        return response, nil
    }
    if isTransientError(err) {
        time.Sleep(backoff(attempt))
        continue
    }
    return nil, err
}
```

### 2. At-Most-Once Semantics

**Definition**: A call is delivered at most once—it either succeeds once or fails without retry.

**Characteristics**:
- No automatic retries on failure
- Guaranteed no duplicate execution
- May fail even if the operation could succeed with a retry
- Lower latency for failed operations (fail fast)

**Use Cases**:
- Non-idempotent operations (e.g., `IncrementCounter`, `DeductBalance`)
- Operations with side effects that must not duplicate
- Systems prioritizing consistency over availability
- Best-effort delivery scenarios

**Example**:
```go
// Single attempt, no retry
response, err := CallObject(ctx, objType, id, method, request)
if err != nil {
    return nil, err  // Fail immediately
}
return response, nil
```

### 3. Exactly-Once Semantics

**Definition**: A call is delivered exactly once, regardless of failures or retries.

**Characteristics**:
- Combines retry logic (availability) with deduplication (consistency)
- Requires distributed coordination (e.g., unique request IDs, idempotency keys)
- Higher implementation complexity and latency overhead
- Provides strongest guarantees

**Implementation Requirements**:
- Request ID tracking (client-side generation)
- Server-side deduplication cache
- Persistent storage for request states
- Coordination across retries and node failures

**Use Cases**:
- Financial transactions (payments, transfers)
- Critical state mutations
- Systems requiring both high availability and strict consistency

**Example**:
```go
// Client generates unique request ID
requestID := generateUUID()
request.RequestID = requestID

// Retry with deduplication
for attempt := 0; attempt < maxRetries; attempt++ {
    response, err := CallObject(ctx, objType, id, method, request)
    if err == nil {
        return response, nil
    }
    if isDuplicateRequest(err) {
        // Server already processed this request
        return getCachedResponse(requestID), nil
    }
    if isTransientError(err) {
        time.Sleep(backoff(attempt))
        continue
    }
    return nil, err
}
```

## Object Creation Policies

Related to call semantics is the question of **when objects are created**:

### A. Auto-Create on First Call (Current Goverse Behavior)

**Definition**: Objects are automatically created when first accessed via CallObject.

**Characteristics**:
- No explicit CreateObject call required before method invocation
- Objects are lazily instantiated on-demand
- Aligns with virtual actor/grain model (Orleans, Akka)
- Simplifies client code—no need to track object lifecycle

**Goverse Implementation**:
```go
// Auto-creation happens inside CallObject (simplified for illustration)
func (node *Node) CallObject(ctx context.Context, typ, id, method string, request proto.Message) (proto.Message, error) {
    // Acquire locks and check node state (omitted for brevity)
    node.stopMu.RLock()
    defer node.stopMu.RUnlock()
    
    // Attempt to create object if it doesn't exist (idempotent)
    err := node.createObject(ctx, typ, id)
    if err != nil {
        return nil, fmt.Errorf("failed to auto-create object %s: %w", id, err)
    }
    
    // Acquire per-object lock and retrieve object
    unlockKey := node.keyLock.RLock(id)
    defer unlockKey()
    
    // Now object exists, proceed with method invocation via reflection
    // (full implementation includes type validation, method lookup, and metrics)
    // ...
}
```

**Advantages**:
- **Developer convenience**: No need to manage object creation separately
- **Natural virtual actor semantics**: Actors are created on first message
- **Idempotent**: Multiple calls to create the same object succeed
- **Persistence-aware**: Automatically loads from storage if available

**Trade-offs**:
- Implicit object lifecycle (may be surprising to developers unfamiliar with actor model)
- Every call incurs a creation check (though optimized with double-check locking)
- Type must be provided on every call (for auto-creation)

### B. Explicit Creation Required

**Definition**: Objects must be explicitly created with CreateObject before calling methods.

**Characteristics**:
- Explicit lifecycle management
- CallObject fails if object doesn't exist
- Clearer separation of concerns (creation vs. invocation)

**Example**:
```go
// Must create first
objID, err := goverseapi.CreateObject(ctx, "Counter", "Counter-1")
if err != nil {
    return err
}

// Then call methods
response, err := goverseapi.CallObject(ctx, "Counter", objID, "Increment", req)
```

**Advantages**:
- Explicit control over object creation
- Type only needed at creation time
- CallObject fails fast if object doesn't exist

**Trade-offs**:
- More boilerplate code
- Need to track whether object has been created
- Requires error handling for "object not found" cases

### C. Conditional Auto-Create (Policy Parameter)

**Definition**: Support both modes with a policy parameter.

**Example**:
```go
// Auto-create if missing
response, err := CallObject(ctx, objType, id, method, request, 
    WithCreatePolicy(CreateIfMissing))

// Fail if object doesn't exist
response, err := CallObject(ctx, objType, id, method, request, 
    WithCreatePolicy(MustExist))
```

**Advantages**:
- Flexibility for different use cases
- Backward compatible (default to auto-create)

**Trade-offs**:
- Increased API complexity
- More code paths to test and maintain

## Analysis: Which Policies Should Goverse Provide?

### Current State

Goverse currently implements:
- **Call Semantics**: **At-Most-Once** (single attempt, no automatic retry)
- **Creation Policy**: **Auto-Create on First Call**

This aligns with the virtual actor/grain model:
- Objects represent stateful entities with identity
- Objects are activated on first access
- Callers don't manage object lifecycle explicitly

### Recommendations

#### 1. Keep Auto-Create as Default Behavior ✅

**Rationale**:
- Aligns with virtual actor model (Orleans, Akka, Ray)
- Simplifies application code
- Enables natural patterns: `CallObject(ctx, "User", userID, "GetProfile")`
- Already well-tested and understood

**Current Implementation**: Already in place via `node.createObject()` in CallObject path.

#### 2. Maintain At-Most-Once Call Semantics ✅

**Rationale**:
- Simpler implementation (no distributed coordination)
- Lower latency (no retry overhead on failures)
- Applications can implement retry logic at higher level if needed
- Appropriate for RPC-style interactions

**Current Implementation**: Already in place—CallObject makes single gRPC call without retries.

#### 3. Do NOT Add Explicit Must-Exist Policy ❌

**Rationale**:
- Adds complexity without clear benefit in virtual actor model
- Applications can check existence with GetObject or similar if needed
- Auto-creation is idempotent and efficient (double-check locking)
- Deviating from virtual actor semantics would confuse users

**Alternative**: If explicit lifecycle control is needed, use:
```go
// Create object explicitly if you need to control timing/initialization
objID, _ := goverseapi.CreateObject(ctx, "Counter", "Counter-1")

// CallObject will find existing object (idempotent auto-create succeeds)
response, _ := goverseapi.CallObject(ctx, "Counter", objID, "Increment", req)
```

#### 4. Do NOT Add Automatic Retry (At-Least-Once) ❌

**Rationale**:
- Most methods are NOT naturally idempotent
- Automatic retry could cause duplicate execution (e.g., double-charging, duplicate messages)
- Retry logic should be application-specific (which errors to retry, backoff strategy)
- RPC clients can implement retry loops themselves if needed

**Alternative**: Document retry pattern for clients:
```go
func callWithRetry(ctx context.Context, objType, id, method string, request proto.Message) (proto.Message, error) {
    for attempt := 0; attempt < 3; attempt++ {
        response, err := goverseapi.CallObject(ctx, objType, id, method, request)
        if err == nil {
            return response, nil
        }
        if isTransient(err) {
            time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
            continue
        }
        return nil, err
    }
    return nil, fmt.Errorf("max retries exceeded")
}
```

#### 5. Do NOT Add Exactly-Once Semantics ❌

**Rationale**:
- Requires significant infrastructure (request ID tracking, deduplication cache, persistence)
- High implementation complexity
- Performance overhead (latency, storage, coordination)
- Most use cases don't require this level of guarantee
- Better handled at application level for critical operations

**Alternative**: For financial or critical operations, implement idempotency at application level:
```go
// Application-level idempotent operation
func (account *Account) DeductBalance(ctx context.Context, req *DeductRequest) (*DeductResponse, error) {
    account.mu.Lock()
    defer account.mu.Unlock()
    
    // Check if transaction already processed
    if account.processedTransactions[req.TransactionID] {
        return account.getTransactionResult(req.TransactionID), nil
    }
    
    // Process transaction
    account.balance -= req.Amount
    account.processedTransactions[req.TransactionID] = true
    
    return &DeductResponse{NewBalance: account.balance}, nil
}
```

## Summary

### Goverse Should Provide

1. **Auto-Create on First Call** (Current ✅)
   - Default behavior for all CallObject invocations
   - Aligns with virtual actor model
   - Idempotent and efficient

2. **At-Most-Once Call Semantics** (Current ✅)
   - Single RPC attempt without automatic retry
   - Simple, predictable behavior
   - Low latency

### Goverse Should NOT Provide

1. **Must-Exist Policy** ❌
   - Unnecessary complexity
   - Counter to virtual actor model
   - Can be worked around if needed

2. **At-Least-Once Semantics (Automatic Retry)** ❌
   - Risk of duplicate execution
   - Should be application-specific
   - Can be implemented by callers

3. **Exactly-Once Semantics** ❌
   - High complexity and overhead
   - Better handled at application level for critical operations
   - Not required for typical use cases

## Design Rationale

Goverse's approach prioritizes:

1. **Simplicity**: Keep the API surface minimal and intuitive
2. **Actor Model Alignment**: Stay true to virtual actor semantics (Orleans, Akka)
3. **Performance**: Avoid unnecessary coordination overhead
4. **Flexibility**: Allow applications to implement higher-level semantics when needed

By keeping the core runtime simple and focused, Goverse enables developers to build sophisticated distributed systems while maintaining the flexibility to implement custom retry, idempotency, and coordination strategies at the application level where business logic resides.

## Examples

### Current Pattern (Recommended)

```go
// Simple call with auto-creation
goverseapi.RegisterObjectType((*Counter)(nil))

// Object is auto-created on first call
response, err := goverseapi.CallObject(ctx, "Counter", "Counter-1", "Increment", req)
if err != nil {
    return fmt.Errorf("call failed: %w", err)
}

// Subsequent calls reuse existing object
response, err = goverseapi.CallObject(ctx, "Counter", "Counter-1", "GetValue", req)
```

### Explicit Creation (Optional, for Specific Use Cases)

```go
// Explicitly create object to control initialization timing
objID, err := goverseapi.CreateObject(ctx, "Counter", "Counter-1")
if err != nil {
    return fmt.Errorf("creation failed: %w", err)
}

// CallObject finds existing object (idempotent auto-create succeeds)
response, err := goverseapi.CallObject(ctx, "Counter", objID, "Increment", req)
```

### Application-Level Retry (If Needed)

```go
// Implement retry logic at application level for specific operations
func IncrementCounterWithRetry(ctx context.Context, counterID string) error {
    req := &IncrementRequest{Amount: 1}
    
    for attempt := 0; attempt < 3; attempt++ {
        _, err := goverseapi.CallObject(ctx, "Counter", counterID, "Increment", req)
        if err == nil {
            return nil
        }
        
        // Only retry on transient errors
        if isNetworkError(err) || isTimeout(err) {
            time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
            continue
        }
        
        // Don't retry on permanent errors (e.g., validation failures)
        return err
    }
    
    return fmt.Errorf("max retries exceeded")
}
```

### Application-Level Idempotency (For Critical Operations)

```go
type PaymentService struct {
    goverseapi.BaseObject
    mu                   sync.Mutex
    processedPayments    map[string]*PaymentResult
}

func (p *PaymentService) ProcessPayment(ctx context.Context, req *PaymentRequest) (*PaymentResponse, error) {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    // Check if payment already processed (idempotency)
    if result, exists := p.processedPayments[req.PaymentID]; exists {
        return &PaymentResponse{
            Success:       result.Success,
            TransactionID: result.TransactionID,
            Message:       "Payment already processed",
        }, nil
    }
    
    // Process payment (only once per PaymentID)
    result := p.executePayment(req)
    p.processedPayments[req.PaymentID] = result
    
    return &PaymentResponse{
        Success:       result.Success,
        TransactionID: result.TransactionID,
    }, nil
}
```

## Conclusion

Goverse's current design—**auto-create on first call** with **at-most-once semantics**—strikes the right balance for a virtual actor runtime:

- **Simple**: Easy to understand and use
- **Efficient**: Minimal overhead for common cases
- **Flexible**: Applications can layer additional semantics as needed
- **Aligned**: Consistent with established actor model frameworks

No changes to the current behavior are recommended. The existing implementation should be documented as the intentional design, not expanded with additional policy options that would increase complexity without proportional benefit.
