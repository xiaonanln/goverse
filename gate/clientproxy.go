package gate

import (
	"fmt"
	"sync"

	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/proto"
)

// ClientProxy represents a client connection in the gateway
// It holds minimal connection-local state needed for transport
type ClientProxy struct {
	id          string
	messageChan chan proto.Message
	closed      bool
	mu          sync.RWMutex
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
// Returns the channel even after Close() is called
func (cp *ClientProxy) MessageChan() chan proto.Message {
	return cp.messageChan
}

// Close closes the message channel and cleans up resources
// Multiple calls to Close are safe (idempotent)
func (cp *ClientProxy) Close() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closed {
		return
	}
	cp.closed = true
	close(cp.messageChan)
}

// IsClosed returns true if the client proxy has been closed
func (cp *ClientProxy) IsClosed() bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.closed
}

// PushMessage handles a message received from a node for this client
// It forwards the message to the client's message channel
func (cp *ClientProxy) PushMessage(msg proto.Message) {
	select {
	case cp.messageChan <- msg:
		// Message sent successfully
	default:
		cp.logger.Warnf("ClientProxy %s message channel full, dropping message!!!", cp.id)
		// Channel full, drop message
	}
}
