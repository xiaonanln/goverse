package gate

import (
	"fmt"

	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/proto"
)

// ClientProxy represents a client connection in the gateway
// It holds minimal connection-local state needed for transport
type ClientProxy struct {
	id          string
	messageChan chan proto.Message
	logger      *logger.Logger
}

// NewClientProxy creates a new client proxy with the given ID
func NewClientProxy(id string) *ClientProxy {
	return &ClientProxy{
		id:          id,
		messageChan: make(chan proto.Message, 10), // Buffered channel to avoid blocking
		logger:      logger.NewLogger(fmt.Sprintf("ClientProxy(%s)", id)),
	}
}

// GetID returns the client proxy ID
func (cp *ClientProxy) GetID() string {
	return cp.id
}

// MessageChan returns the message channel for push notifications
func (cp *ClientProxy) MessageChan() chan proto.Message {
	return cp.messageChan
}

// Close closes the message channel and cleans up resources
func (cp *ClientProxy) Close() {
	close(cp.messageChan)
}

// HandleMessage handles a message received from a node for this client
// It forwards the message to the client's message channel
func (cp *ClientProxy) HandleMessage(msg proto.Message) {
	select {
	case cp.messageChan <- msg:
		// Message sent successfully
	default:
		cp.logger.Warnf("ClientProxy %s message channel full, dropping message!!!", cp.id)
		// Channel full, drop message
	}
}
