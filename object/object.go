package object

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/uniqueid"
	"google.golang.org/protobuf/proto"
)

type Object interface {
	// Id returns the unique identifier of the object.
	//
	// The ID is set during object initialization via OnInit and remains constant
	// throughout the object's lifetime. IDs are globally unique within the cluster.
	//
	// Returns:
	//   - string: The unique object identifier
	Id() string

	// Type returns the type name of the object.
	//
	// The type name is derived from the concrete struct type name using reflection
	// (e.g., "Counter" for *Counter). This is used for object registration, routing,
	// and diagnostic purposes.
	//
	// Returns:
	//   - string: The object type name
	Type() string

	// String returns a human-readable string representation of the object.
	//
	// The default format is "TypeName(id)" (e.g., "Counter(abc-123)").
	// This method is primarily used for logging and debugging.
	//
	// Returns:
	//   - string: String representation of the object
	String() string

	// CreationTime returns the timestamp when the object was created.
	//
	// This is set during OnInit and represents when the object instance was
	// first initialized in memory, not necessarily when it was first persisted.
	//
	// Returns:
	//   - time.Time: Object creation timestamp
	CreationTime() time.Time

	// OnInit initializes the object with its identity.
	//
	// This is the first method called when an object is created or reactivated.
	// It sets up the object's ID, creation time, logger, and lifetime context.
	// This method is called by the runtime and should not be called directly.
	//
	// Parameters:
	//   - self: Reference to the concrete object implementation
	//   - id: Unique identifier for the object (generated if empty string provided)
	//
	// Implementation Note:
	// When embedding BaseObject, this method is automatically implemented.
	// Custom implementations must initialize all required fields.
	OnInit(self Object, id string)

	// OnCreated is called after the object is fully initialized and registered.
	//
	// This lifecycle hook is invoked after OnInit and after the object is added
	// to the node's object registry. Use this method to perform initialization
	// logic that requires the object to be fully operational (e.g., starting
	// background tasks, establishing connections, loading data).
	//
	// Thread-Safety: This method is called with the object's per-key lock held,
	// ensuring no concurrent calls can be made to the object during initialization.
	OnCreated()

	// Destroy is called when the object is being removed from memory.
	//
	// This lifecycle hook allows the object to perform cleanup before removal.
	// The default implementation cancels the object's lifetime context, which
	// signals to any background goroutines that they should terminate.
	//
	// Thread-Safety: This method may be called concurrently with object methods
	// if the object is being destroyed while processing requests. Implementations
	// should handle cleanup gracefully and be idempotent (safe to call multiple times).
	//
	// Implementation Note:
	// When overriding, always call the base implementation to ensure proper
	// context cancellation:
	//   func (obj *MyObject) Destroy() {
	//       // Custom cleanup
	//       obj.BaseObject.Destroy()  // Cancel context
	//   }
	Destroy()

	// ToData serializes the object state to a proto.Message for persistence.
	//
	// Thread-Safety Requirements:
	// This method MUST be thread-safe as it can be called concurrently with
	// object method execution (e.g., during periodic persistence while the
	// object is processing requests).
	//
	// Best Practice - Use a mutex to ensure consistent state snapshots:
	//   func (obj *MyObject) ToData() (proto.Message, error) {
	//       obj.mu.Lock()
	//       defer obj.mu.Unlock()
	//       // ... serialize fields safely
	//   }
	//
	// Returns:
	//   - proto.Message: Serialized object state
	//   - error: ErrNotPersistent for non-persistent objects, or serialization error
	ToData() (proto.Message, error)

	// FromData deserializes object state from a proto.Message.
	//
	// Thread-Safety Requirements:
	// This method MUST be thread-safe as it may be called during object
	// initialization or reactivation while other operations are in progress.
	//
	// Best Practice - Use the same mutex as ToData():
	//   func (obj *MyObject) FromData(data proto.Message) error {
	//       obj.mu.Lock()
	//       defer obj.mu.Unlock()
	//       // ... restore fields safely
	//   }
	//
	// Parameters:
	//   - data: proto.Message containing serialized state (may be nil)
	// Returns:
	//   - error: Deserialization error, or nil for non-persistent objects
	FromData(data proto.Message) error

	// ToDataWithSeq atomically captures object state and nextRcseq together.
	// This ensures consistency between persisted data and progress marker.
	// Must be used by persistence layer instead of separate ToData() + GetNextRcseq() calls.
	//
	// Thread-Safety: This method is thread-safe and uses RWMutex synchronization with
	// reliable call execution to ensure consistent snapshots.
	//
	// Returns:
	//   - proto.Message: Serialized object state
	//   - int64: The nextRcseq value consistent with the returned state
	//   - error: ErrNotPersistent for non-persistent objects, or serialization error
	ToDataWithSeq() (proto.Message, int64, error)

	// GetNextRcseq returns the next reliable call sequence number for this object.
	//
	// Reliable calls are sequentially numbered to ensure exactly-once execution
	// semantics. This method returns the sequence number that will be assigned
	// to the next reliable call. Sequence numbers start at 0 and increment by 1
	// for each processed call.
	//
	// Thread-Safety: This method is thread-safe and uses atomic operations.
	//
	// Returns:
	//   - int64: The next reliable call sequence number
	GetNextRcseq() int64

	// SetNextRcseq sets the next reliable call sequence number for this object.
	//
	// This method is used during object reactivation to restore the reliable call
	// sequence number from persistent storage. It updates the internal counter to
	// ensure sequence continuity across object lifecycle events.
	//
	// Thread-Safety: This method is thread-safe and uses atomic operations.
	//
	// Parameters:
	//   - rcseq: The sequence number to set as the next reliable call sequence
	SetNextRcseq(rcseq int64)

	// LockSeqWrite acquires the sequential write lock for reliable call INSERTs.
	//
	// This method is used by the node to serialize reliable call INSERTs for this object,
	// ensuring sequential seq allocation in the persistence layer. The lock is held only
	// during the INSERT operation and released immediately after.
	//
	// Thread-Safety: This method is thread-safe and uses mutex synchronization.
	//
	// Returns:
	//   - unlock: Function to release the lock (must be called by the caller)
	LockSeqWrite() (unlock func())

	// ProcessPendingReliableCalls processes pending reliable calls up to and including the specified sequence.
	//
	// This method fetches reliable calls from the persistence provider and executes them
	// sequentially in order. It ensures exactly-once execution semantics by tracking
	// processed sequence numbers. Multiple concurrent callers will wait for a single
	// processing goroutine to handle all pending calls.
	//
	// The returned channel receives the ReliableCall matching the requested sequence number
	// if it is processed. The channel is closed after processing completes, regardless of
	// whether the call was found or already executed.
	//
	// Behavior:
	//   - If seq < current sequence: Channel closes immediately (already processed)
	//   - If processing is already running: Waits for existing goroutine to process
	//   - If no processing is running: Starts a new processing goroutine
	//   - Processing continues until no more pending calls exist in persistence
	//
	// Thread-Safety: This method is thread-safe and can be called concurrently.
	//
	// Parameters:
	//   - provider: PersistenceProvider for fetching pending calls from storage
	//   - seq: The sequence number of the reliable call to wait for
	//
	// Returns:
	//   - <-chan *ReliableCall: Channel that receives the matching ReliableCall (if found and processed)
	ProcessPendingReliableCalls(provider PersistenceProvider, seq int64) <-chan *ReliableCall

	// InvokeMethod invokes the specified method on the object using reflection.
	//
	// This method uses reflection to dynamically call object methods by name. It validates
	// that the method exists, has the correct signature (ctx context.Context, req proto.Message)
	// returning (proto.Message, error), and then invokes it with the provided arguments.
	//
	// Method Signature Requirements:
	//   - Must accept exactly 2 parameters: (context.Context, *ConcreteProtoMessage)
	//   - Must return exactly 2 values: (*ConcreteProtoMessage, error)
	//   - Request and response must be concrete protobuf message pointer types
	//
	// Example Method:
	//   func (obj *Counter) Add(ctx context.Context, req *wrapperspb.Int32Value) (*wrapperspb.Int32Value, error) {
	//       // Implementation
	//   }
	//
	// Thread-Safety: This method is called with the object's per-key lock held by the
	// runtime, ensuring serialized access. The invoked method should not attempt to
	// acquire this lock again.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - method: Name of the method to invoke (case-sensitive)
	//   - request: Protobuf message containing method arguments
	//
	// Returns:
	//   - proto.Message: The response from the method invocation
	//   - error: Method invocation error, validation error, or nil on success
	InvokeMethod(ctx context.Context, method string, request proto.Message) (proto.Message, error)
}

// ErrNotPersistent is returned when an object type does not support persistence.
var ErrNotPersistent = fmt.Errorf("object is not persistent")

// seqWaiter represents a waiter for a specific reliable call sequence number
type seqWaiter struct {
	seq int64
	ch  chan<- *ReliableCall
}

type BaseObject struct {
	self            Object
	id              string
	creationTime    time.Time
	nextRcseq       atomic.Int64
	processingCalls bool         // true if a ProcessPendingReliableCalls goroutine is running (protected by seqWaitersMu)
	seqWaiters      []seqWaiter  // waiters sorted by seq (can have duplicates)
	seqWaitersMu    sync.Mutex   // protects seqWaiters and processingCalls
	seqWriteMu      sync.Mutex   // serializes reliable call INSERTs for per-object ordering
	stateMu         sync.RWMutex // protects state + nextRcseq consistency during RC execution and ToDataWithSeq
	Logger          *logger.Logger
	ctx             context.Context
	cancelFunc      context.CancelFunc
}

func (base *BaseObject) OnInit(self Object, id string) {
	base.self = self
	if id == "" {
		id = uniqueid.UniqueId()
	}
	base.id = id
	base.creationTime = time.Now()
	base.seqWaiters = nil // sorted slice, initialized empty
	base.Logger = logger.NewLogger(fmt.Sprintf("%s@%s", base.Type(), base.id))
	base.ctx, base.cancelFunc = context.WithCancel(context.Background())
}

func (base *BaseObject) String() string {
	selfTypeName := reflect.TypeOf(base.self).Elem().Name()
	return fmt.Sprintf("%s(%s)", selfTypeName, base.id)
}

func (base *BaseObject) Id() string {
	return base.id
}

func (base *BaseObject) Type() string {
	return reflect.TypeOf(base.self).Elem().Name()
}

func (base *BaseObject) CreationTime() time.Time {
	return base.creationTime
}

// ToData provides a default implementation for non-persistent objects
// Returns an error indicating this object type is not persistent
func (base *BaseObject) ToData() (proto.Message, error) {
	return nil, ErrNotPersistent
}

// FromData provides a default implementation for non-persistent objects
// Returns an error indicating this object type is not persistent
func (base *BaseObject) FromData(data proto.Message) error {
	return nil
}

// ToDataWithSeq atomically captures object state and nextRcseq together.
// Uses RLock since ToData() is documented as read-only/thread-safe.
func (base *BaseObject) ToDataWithSeq() (proto.Message, int64, error) {
	base.stateMu.RLock()
	defer base.stateMu.RUnlock()

	data, err := base.self.ToData()
	if err != nil {
		return nil, 0, err
	}

	return data, base.nextRcseq.Load(), nil
}

// GetNextRcseq returns the next reliable call sequence number for this object
func (base *BaseObject) GetNextRcseq() int64 {
	return base.nextRcseq.Load()
}

// SetNextRcseq sets the next reliable call sequence number for this object
func (base *BaseObject) SetNextRcseq(rcseq int64) {
	base.nextRcseq.Store(rcseq)
}

// LockSeqWrite acquires the sequential write lock for reliable call INSERTs.
// This ensures that reliable call INSERTs for this object are serialized,
// guaranteeing sequential seq allocation in the persistence layer.
// The caller must call the returned unlock function when done.
func (base *BaseObject) LockSeqWrite() (unlock func()) {
	base.seqWriteMu.Lock()
	return base.seqWriteMu.Unlock
}

// Context returns the object's lifetime context.
// This context is cancelled when the object is destroyed.
// Object methods can use this context to know when to stop background operations.
func (base *BaseObject) Context() context.Context {
	return base.ctx
}

// Destroy is called when the object is being destroyed.
// It cancels the object's lifetime context and allows subclasses to perform cleanup.
// This method is called automatically by the node and is idempotent - multiple calls are safe.
func (base *BaseObject) Destroy() {
	base.cancelFunc()
}

// isConcreteProtoMessage checks if a type is a concrete proto.Message pointer
func isConcreteProtoMessage(t reflect.Type) bool {
	if t.Kind() != reflect.Ptr {
		return false
	}
	if t.Elem().Kind() != reflect.Struct {
		return false
	}
	protoMessageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
	return t.Implements(protoMessageType)
}

// InvokeMethod invokes the specified method on the object using reflection
func (base *BaseObject) InvokeMethod(ctx context.Context, method string, request proto.Message) (proto.Message, error) {
	objValue := reflect.ValueOf(base.self)
	methodValue := objValue.MethodByName(method)
	if !methodValue.IsValid() {
		return nil, fmt.Errorf("method not found in class %s: %s", base.self.Type(), method)
	}

	methodType := methodValue.Type()
	if methodType.NumIn() != 2 {
		return nil, fmt.Errorf("method %s has invalid number of arguments (expected: 2, got: %d)", method, methodType.NumIn())
	}
	if !methodType.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) ||
		!isConcreteProtoMessage(methodType.In(1)) {
		return nil, fmt.Errorf("method %s has invalid argument types (expected: context.Context, *Message; got: %s, %s)", method, methodType.In(0), methodType.In(1))
	}

	// Check method return types: (proto.Message, error)
	if methodType.NumOut() != 2 {
		return nil, fmt.Errorf("method %s has invalid number of return values (expected: 2, got: %d)", method, methodType.NumOut())
	}
	if !isConcreteProtoMessage(methodType.Out(0)) ||
		!methodType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return nil, fmt.Errorf("method %s has invalid return types (expected: *Message, error; got: %s, %s)", method, methodType.Out(0), methodType.Out(1))
	}

	// Unmarshal request to the expected concrete proto.Message type
	expectedReqType := methodType.In(1)
	base.Logger.Infof("Request value: %+v", request)

	if reflect.TypeOf(request) != expectedReqType {
		return nil, fmt.Errorf("request type mismatch: expected %s, got %s", expectedReqType, reflect.TypeOf(request))
	}

	// Call the method with the unmarshaled request as argument
	// At this point the per-key read lock is still held.
	// This guarantees that no concurrent Delete/Create can remove or replace the object while the user method executes.
	results := methodValue.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(request)})

	if len(results) != 2 {
		return nil, fmt.Errorf("method %s has invalid signature", method)
	}

	// Return the actual result from the method
	resp, errVal := results[0], results[1]

	if !errVal.IsNil() {
		return nil, errVal.Interface().(error)
	}

	return resp.Interface().(proto.Message), nil
}

// ProcessPendingReliableCalls processes pending reliable calls up to and including the specified seq.
// Returns a channel that receives the ReliableCall matching the given seq (if processed).
// The channel is closed after processing completes.
// If the call was already executed (seq < nextRcseq), the channel is closed immediately without adding anything.
// Always returns a channel; if processing is already running, the caller waits for that goroutine.
func (base *BaseObject) ProcessPendingReliableCalls(provider PersistenceProvider, seq int64) <-chan *ReliableCall {
	callsChan := make(chan *ReliableCall, 1)

	// If the seq is already processed, close immediately
	nextRcseq := base.GetNextRcseq()
	if seq < nextRcseq {
		base.Logger.Infof("[DEBUG] ProcessPendingReliableCalls: seq=%d < nextRcseq=%d, closing immediately", seq, nextRcseq)
		close(callsChan)
		return callsChan
	}
	base.Logger.Infof("[DEBUG] ProcessPendingReliableCalls: registering waiter for seq=%d (nextRcseq=%d)", seq, nextRcseq)

	// Register as a waiter and try to start processing goroutine atomically
	base.seqWaitersMu.Lock()
	base.insertWaiterSorted(seq, callsChan)
	shouldStart := !base.processingCalls
	if shouldStart {
		base.processingCalls = true
	}
	base.seqWaitersMu.Unlock()

	if shouldStart {
		go base.runProcessingLoop(provider)
	}

	return callsChan
}

// insertWaiterSorted inserts a waiter in sorted order by seq.
// Must be called with seqWaitersMu held.
func (base *BaseObject) insertWaiterSorted(seq int64, ch chan<- *ReliableCall) {
	w := seqWaiter{seq: seq, ch: ch}
	base.seqWaiters = append(base.seqWaiters, w)
	// Bubble up to maintain sorted order
	for i := len(base.seqWaiters) - 1; i > 0 && base.seqWaiters[i].seq < base.seqWaiters[i-1].seq; i-- {
		base.seqWaiters[i], base.seqWaiters[i-1] = base.seqWaiters[i-1], base.seqWaiters[i]
	}
}

// notifyWaiters sends the processed call to all waiters for that seq and closes their channels.
// Also closes any waiters with seq < the specified seq (they missed their call).
// Since the list is sorted and calls are processed in order, matching waiters are at the front.
func (base *BaseObject) notifyWaiters(seq int64, call *ReliableCall) {
	base.seqWaitersMu.Lock()
	// Pop waiters from the front: close those with seq < current, notify those with seq == current
	var toClose []chan<- *ReliableCall
	var toNotify []chan<- *ReliableCall
	numWaitersBefore := len(base.seqWaiters)
	for len(base.seqWaiters) > 0 && base.seqWaiters[0].seq <= seq {
		if base.seqWaiters[0].seq == seq {
			toNotify = append(toNotify, base.seqWaiters[0].ch)
		} else {
			toClose = append(toClose, base.seqWaiters[0].ch)
		}
		base.seqWaiters = base.seqWaiters[1:]
	}
	base.seqWaitersMu.Unlock()

	if len(toClose) > 0 || len(toNotify) > 0 {
		base.Logger.Infof("[DEBUG] notifyWaiters(seq=%d, call=%v): before=%d, toClose=%d, toNotify=%d",
			seq, call != nil, numWaitersBefore, len(toClose), len(toNotify))
	}

	for _, ch := range toClose {
		close(ch)
	}
	for _, ch := range toNotify {
		if call != nil {
			ch <- call
		}
		close(ch)
	}
}

// runProcessingLoop is the single processing goroutine that processes pending calls.
// Design: The goroutine stays running as long as waiters exist. It only exits when
// there are no waiters AND no pending calls (or on shutdown). This eliminates the
// need for complex restart logic.
func (base *BaseObject) runProcessingLoop(provider PersistenceProvider) {
	defer base.cleanupProcessingLoop()

	for {
		// Check shutdown
		if base.ctx.Err() != nil {
			return
		}

		// Close waiters for already-processed seqs
		base.notifyWaiters(base.GetNextRcseq()-1, nil)

		// Check if we should exit: no waiters means no one is waiting for results.
		// This check is atomic with setting processingCalls=false to prevent races.
		if base.tryExitIfNoWaiters() {
			return
		}

		// Fetch pending calls
		pendingCalls, err := provider.GetPendingReliableCalls(base.ctx, base.id, base.GetNextRcseq())
		if err != nil {
			if base.ctx.Err() != nil {
				return
			}
			base.Logger.Errorf("runProcessingLoop: failed to fetch pending calls: %v", err)
			base.waitWithBackoff()
			continue
		}

		// No pending calls - wait and retry (waiters exist, so we keep polling)
		if len(pendingCalls) == 0 {
			base.waitWithBackoff()
			continue
		}

		// Process each pending call
		for _, call := range pendingCalls {
			if base.ctx.Err() != nil {
				return
			}
			base.processReliableCall(provider, call)
			base.notifyWaiters(call.Seq, call)
		}
	}
}

// tryExitIfNoWaiters atomically checks if there are no waiters and marks processing as stopped.
// Returns true if goroutine should exit.
func (base *BaseObject) tryExitIfNoWaiters() bool {
	base.seqWaitersMu.Lock()
	defer base.seqWaitersMu.Unlock()

	if len(base.seqWaiters) == 0 {
		base.processingCalls = false
		base.Logger.Infof("[DEBUG] tryExitIfNoWaiters: no waiters, exiting goroutine")
		return true
	}
	return false
}

// waitWithBackoff waits briefly before retrying, or returns immediately on shutdown.
func (base *BaseObject) waitWithBackoff() {
	select {
	case <-base.ctx.Done():
	case <-time.After(10 * time.Millisecond):
	}
}

// cleanupProcessingLoop handles cleanup when the processing loop exits.
// Closes all remaining waiter channels so callers don't block forever.
func (base *BaseObject) cleanupProcessingLoop() {
	base.seqWaitersMu.Lock()
	defer base.seqWaitersMu.Unlock()

	numWaiters := len(base.seqWaiters)
	if numWaiters > 0 {
		base.Logger.Warnf("[DEBUG] cleanupProcessingLoop: closing %d remaining waiters!", numWaiters)
	}

	base.processingCalls = false
	for _, waiter := range base.seqWaiters {
		close(waiter.ch)
	}
	base.seqWaiters = nil
}

// processReliableCall executes a single reliable call
// Holds stateMu.Lock() during execution to ensure atomic state mutation + nextRcseq update
func (base *BaseObject) processReliableCall(provider PersistenceProvider, call *ReliableCall) {
	base.stateMu.Lock()
	defer base.stateMu.Unlock()

	// Helper to finalize call status and persist
	finalize := func(status string, resultData []byte, errMsg string) {
		// Persist to DB BEFORE updating in-memory counter
		// This ensures other goroutines querying by seq see the updated status
		updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, status, resultData, errMsg)

		// Now safe to advance in-memory counter - DB is already updated
		base.nextRcseq.Store(call.Seq + 1)
		call.Status = status
		call.ResultData = resultData
		call.Error = errMsg
	}

	// Unmarshal the request data to proto.Message
	requestMsg, err := protohelper.BytesToMsg(call.RequestData)
	if err != nil {
		base.Logger.Errorf("Reliable call %s (seq=%d) failed: %v", call.CallID, call.Seq, err)
		finalize("failed", nil, err.Error())
		return
	}

	// Invoke the method on the object (state mutation happens here)
	result, err := base.self.InvokeMethod(base.ctx, call.MethodName, requestMsg)
	if err != nil {
		base.Logger.Errorf("Reliable call %s (seq=%d) failed: %v", call.CallID, call.Seq, err)
		finalize("failed", nil, err.Error())
		return
	}

	// Update to success status with result
	base.Logger.Infof("Reliable call %s (seq=%d) succeeded: result=%v", call.CallID, call.Seq, result)
	resultData, _ := protohelper.MsgToBytes(result)
	finalize("success", resultData, "")
}
