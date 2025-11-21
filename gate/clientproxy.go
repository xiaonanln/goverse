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
	closed      bool
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
// Returns nil if the client proxy has been closed
func (cp *ClientProxy) MessageChan() <-chan proto.Message {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	if cp.closed {
		return nil
	}
	return cp.messageChan
}

// Close closes the message channel and cleans up resources
// Safe to call multiple times
func (cp *ClientProxy) Close() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if !cp.closed {
		cp.closed = true
		if cp.messageChan != nil {
			close(cp.messageChan)
			cp.messageChan = nil
		}
	}
}

// HandleMessage handles a message received from a node for this client
// It forwards the message to the client's message channel
// Returns true if message was sent, false if dropped or client is closed
func (cp *ClientProxy) HandleMessage(msg proto.Message) bool {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	if cp.closed {
		return false
	}
	select {
	case cp.messageChan <- msg:
		// Message sent successfully
		return true
	default:
		// Channel full, drop message
		return false
	}
}
