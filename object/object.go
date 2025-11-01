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
	OnInit(self Object, id string, data proto.Message)
	OnCreated()
	// ToData serializes the object state to a map for persistence
	// Returns (nil, error) for non-persistent objects
	ToData() (map[string]interface{}, error)
	// FromData deserializes object state from a map
	FromData(data map[string]interface{}) error
}

type BaseObject struct {
	self         Object
	id           string
	creationTime time.Time
	Logger       *logger.Logger
}

func (base *BaseObject) OnInit(self Object, id string, data proto.Message) {
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
func (base *BaseObject) ToData() (map[string]interface{}, error) {
	return nil, fmt.Errorf("object type %s is not persistent", base.Type())
}

// FromData provides a default implementation for non-persistent objects
// Returns an error indicating this object type is not persistent
func (base *BaseObject) FromData(data map[string]interface{}) error {
	return fmt.Errorf("object type %s is not persistent", base.Type())
}
