package object

import (
	"context"
	"fmt"
	"reflect"
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

type BaseObject struct {
	self            Object
	id              string
	creationTime    time.Time
	nextRcseq       atomic.Int64
	processingCalls atomic.Bool // true if a ProcessPendingReliableCalls goroutine is running
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

// ProcessPendingReliableCalls fetches and processes pending reliable calls for this object.
// Returns a channel that receives each processed ReliableCall.
// The channel is closed when processing completes.
// Only one processing goroutine is allowed at a time; returns nil if already processing.
func (base *BaseObject) ProcessPendingReliableCalls(provider PersistenceProvider, seq int64) <-chan *ReliableCall {
	// Ensure only one processing goroutine runs at a time
	if !base.processingCalls.CompareAndSwap(false, true) {
		base.Logger.Warnf("ProcessPendingReliableCalls: already processing, skipping")
		return nil
	}

	callsChan := make(chan *ReliableCall)

	go func() {
		defer base.processingCalls.Store(false)
		defer close(callsChan)

		nextRcseq := base.GetNextRcseq()

		base.Logger.Infof("Querying for pending reliable calls for object %s with nextRcseq=%d", base.id, nextRcseq)

		// Fetch pending calls for this object
		pendingCalls, err := provider.GetPendingReliableCalls(base.ctx, base.id, nextRcseq)
		if err != nil {
			base.Logger.Errorf("ProcessPendingReliableCalls: failed to fetch pending calls: %v", err)
			return
		}

		base.Logger.Infof("Query returned %d pending reliable calls for object %s", len(pendingCalls), base.id)

		if len(pendingCalls) == 0 {
			base.Logger.Infof("No pending reliable calls for object %s", base.id)
			return
		}

		base.Logger.Infof("Found %d pending reliable calls for object %s, processing sequentially", len(pendingCalls), base.id)

		// Process each pending call sequentially in order of seq
		for _, call := range pendingCalls {
			// Check if object is destroyed before processing
			select {
			case <-base.ctx.Done():
				base.Logger.Warnf("ProcessPendingReliableCalls: object destroyed, stopping processing")
				return
			default:
			}

			base.Logger.Infof("Processing reliable call: seq=%d, call_id=%s, method=%s", call.Seq, call.CallID, call.MethodName)

			fail := func(err error) {
				base.Logger.Errorf("Reliable call %s (seq=%d) failed: %v", call.CallID, call.Seq, err)
				call.Status = "failed"
				call.Error = err.Error()
				// Use a separate context with 60s timeout to ensure status update completes even if ctx is cancelled
				updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, "failed", nil, call.Error)
				base.SetNextRcseq(call.Seq + 1) // Advance nextRcseq on failure to avoid blocking
				callsChan <- call
			}

			success := func(result proto.Message) {
				base.Logger.Infof("Reliable call %s (seq=%d) succeeded: result=%v", call.CallID, call.Seq, result)
				resultData, _ := protohelper.MsgToBytes(result)
				call.Status = "success"
				call.ResultData = resultData
				// Use a separate context with 60s timeout to ensure status update completes even if ctx is cancelled
				updateCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				_ = provider.UpdateReliableCallStatus(updateCtx, call.Seq, "success", resultData, "")
				base.SetNextRcseq(call.Seq + 1) // Advance nextRcseq on success
				callsChan <- call
			}

			// Unmarshal the request data to proto.Message
			requestMsg, err := protohelper.BytesToMsg(call.RequestData)
			if err != nil {
				fail(err)
				continue
			}

			// Invoke the method on the object
			result, err := base.self.InvokeMethod(base.ctx, call.MethodName, requestMsg)
			if err != nil {
				fail(err)
				continue
			}

			// Update to success status with result
			success(result)
		}
	}()

	return callsChan
}
