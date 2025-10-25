package client

import (
	"github.com/xiaonanln/goverse/object"
	"google.golang.org/protobuf/proto"
)

type ClientObject interface {
	object.Object
	MessageChan() chan proto.Message
}

// BaseClient provides a foundational implementation for clients.
// User defined clients can embed this struct to inherit common functionality.
type BaseClient struct {
	object.BaseObject // Embedding BaseObject to inherit common object functionality
	messageChan       chan proto.Message
}

func (cp *BaseClient) MessageChan() chan proto.Message {
	return cp.messageChan
}

func (cp *BaseClient) OnCreated() {
	cp.messageChan = make(chan proto.Message)
}
