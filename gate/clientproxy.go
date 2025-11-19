package gate

import (
	"sync"

	"google.golang.org/protobuf/proto"
)

// ClientProxy represents a client connection in the gateway
// It holds minimal connection-local state needed for transport
type ClientProxy struct {
	id          string
	messageChan chan proto.Message
	mu          sync.RWMutex
}

// NewClientProxy creates a new client proxy with the given ID
func NewClientProxy(id string) *ClientProxy {
	return &ClientProxy{
		id:          id,
		messageChan: make(chan proto.Message, 10), // Buffered channel to avoid blocking
	}
}

// GetID returns the client proxy ID
func (cp *ClientProxy) GetID() string {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.id
}

// MessageChan returns the message channel for push notifications
func (cp *ClientProxy) MessageChan() chan proto.Message {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	return cp.messageChan
}

// Close closes the message channel and cleans up resources
func (cp *ClientProxy) Close() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.messageChan != nil {
		close(cp.messageChan)
		cp.messageChan = nil
	}
}
