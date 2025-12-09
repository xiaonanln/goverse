package object

import (
	"fmt"
	"reflect"
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

	// GetNextRcid returns the next reliable call ID for this object
	GetNextRcid() int64

	// SetNextRcid sets the next reliable call ID for this object
	SetNextRcid(rcid int64)
}

// ErrNotPersistent is returned when an object type does not support persistence.
var ErrNotPersistent = fmt.Errorf("object is not persistent")

type BaseObject struct {
	self         Object
	id           string
	creationTime time.Time
	nextRcid     int64
	Logger       *logger.Logger
}

func (base *BaseObject) OnInit(self Object, id string) {
	base.self = self
	if id == "" {
		id = uniqueid.UniqueId()
	}
	base.id = id
	base.creationTime = time.Now()
	base.Logger = logger.NewLogger(fmt.Sprintf("%s@%s", base.Type(), base.id))
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

// GetNextRcid returns the next reliable call ID for this object
func (base *BaseObject) GetNextRcid() int64 {
	return base.nextRcid
}

// SetNextRcid sets the next reliable call ID for this object
func (base *BaseObject) SetNextRcid(rcid int64) {
	base.nextRcid = rcid
}
