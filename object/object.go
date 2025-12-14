package object

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
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
}

// ErrNotPersistent is returned when an object type does not support persistence.
var ErrNotPersistent = fmt.Errorf("object is not persistent")

type BaseObject struct {
	self         Object
	id           string
	creationTime time.Time
	nextRcseq    atomic.Int64
	Logger       *logger.Logger
	ctx          context.Context
	cancelFunc   context.CancelFunc
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

// CancelContext cancels the object's lifetime context.
// This is called automatically by the node when the object is destroyed.
// Users should not call this method directly.
func (base *BaseObject) CancelContext() {
	if base.cancelFunc != nil {
		base.cancelFunc()
	}
}
