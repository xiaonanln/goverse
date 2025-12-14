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
	Id() string
	Type() string
	String() string
	CreationTime() time.Time
	OnInit(self Object, id string)
	OnCreated()
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

	// GetNextRcseq returns the next reliable call sequence number for this object
	GetNextRcseq() int64

	// SetNextRcseq sets the next reliable call sequence number for this object
	SetNextRcseq(rcseq int64)

	// ProcessPendingReliableCalls fetches and processes pending reliable calls for this object.
	// Returns a channel that receives each processed ReliableCall.
	// The channel is closed when processing completes.
	ProcessPendingReliableCalls(provider PersistenceProvider, seq int64) <-chan *ReliableCall

	// InvokeMethod invokes the specified method on the object using reflection
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
	processingCalls bool        // true if a ProcessPendingReliableCalls goroutine is running (protected by seqWaitersMu)
	seqWaiters      []seqWaiter // waiters sorted by seq (can have duplicates)
	seqWaitersMu    sync.Mutex  // protects seqWaiters and processingCalls
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

// GetNextRcseq returns the next reliable call sequence number for this object
func (base *BaseObject) GetNextRcseq() int64 {
	return base.nextRcseq.Load()
}

// SetNextRcseq sets the next reliable call sequence number for this object
func (base *BaseObject) SetNextRcseq(rcseq int64) {
	base.nextRcseq.Store(rcseq)
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
	if seq < base.GetNextRcseq() {
		close(callsChan)
		return callsChan
	}

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
	for len(base.seqWaiters) > 0 && base.seqWaiters[0].seq <= seq {
		if base.seqWaiters[0].seq == seq {
			toNotify = append(toNotify, base.seqWaiters[0].ch)
		} else {
			toClose = append(toClose, base.seqWaiters[0].ch)
		}
		base.seqWaiters = base.seqWaiters[1:]
	}
	base.seqWaitersMu.Unlock()

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
// It continues processing as long as there are pending calls in the database,
// even if there are no waiters.
func (base *BaseObject) runProcessingLoop(provider PersistenceProvider) {
	defer func() {
		// Atomically close all waiters and mark processing as done
		base.seqWaitersMu.Lock()
		allWaiters := base.seqWaiters
		base.seqWaiters = nil
		base.processingCalls = false
		base.seqWaitersMu.Unlock()

		for _, w := range allWaiters {
			close(w.ch)
		}
	}()

	for {
		// Check if object is destroyed
		select {
		case <-base.ctx.Done():
			base.Logger.Warnf("runProcessingLoop: object destroyed, exiting")
			return
		default:
		}

		nextRcseq := base.GetNextRcseq()

		// Close waiters for already-processed seqs (those with seq < nextRcseq)
		base.notifyWaiters(nextRcseq-1, nil)

		base.Logger.Infof("Querying for pending reliable calls for object %s with nextRcseq=%d", base.id, nextRcseq)

		// Fetch pending calls for this object
		pendingCalls, err := provider.GetPendingReliableCalls(base.ctx, base.id, nextRcseq)
		if err != nil {
			base.Logger.Errorf("runProcessingLoop: failed to fetch pending calls: %v", err)
			return
		}

		base.Logger.Infof("Query returned %d pending reliable calls for object %s", len(pendingCalls), base.id)

		// No more pending calls - exit (defer will close remaining waiters)
		if len(pendingCalls) == 0 {
			base.Logger.Infof("No pending reliable calls for object %s", base.id)
			return
		}

		// Process each pending call sequentially
		for _, call := range pendingCalls {
			// Check if object is destroyed before processing
			select {
			case <-base.ctx.Done():
				base.Logger.Warnf("runProcessingLoop: object destroyed, stopping processing")
				return
			default:
			}

			base.Logger.Infof("Processing reliable call: seq=%d, call_id=%s, method=%s", call.Seq, call.CallID, call.MethodName)

			// Process the call
			base.processReliableCall(provider, call)
			base.SetNextRcseq(call.Seq + 1)

			// Notify waiters for this seq
			base.notifyWaiters(call.Seq, call)
		}
	}
}

// processReliableCall executes a single reliable call
func (base *BaseObject) processReliableCall(provider PersistenceProvider, call *ReliableCall) {
	// Unmarshal the request data to proto.Message
	requestMsg, err := protohelper.BytesToMsg(call.RequestData)
	if err != nil {
		base.Logger.Errorf("Reliable call %s (seq=%d) failed: %v", call.CallID, call.Seq, err)
		call.Status = "failed"
		call.Error = err.Error()
		updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, "failed", nil, call.Error)
		return
	}

	// Invoke the method on the object
	result, err := base.self.InvokeMethod(base.ctx, call.MethodName, requestMsg)
	if err != nil {
		base.Logger.Errorf("Reliable call %s (seq=%d) failed: %v", call.CallID, call.Seq, err)
		call.Status = "failed"
		call.Error = err.Error()
		updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, "failed", nil, call.Error)
		return
	}

	// Update to success status with result
	base.Logger.Infof("Reliable call %s (seq=%d) succeeded: result=%v", call.CallID, call.Seq, result)
	resultData, _ := protohelper.MsgToBytes(result)
	call.Status = "success"
	call.ResultData = resultData
	updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, "success", resultData, "")
}
