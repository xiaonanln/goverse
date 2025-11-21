package gate

import (
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestNewClientProxy(t *testing.T) {
	clientID := "test-gateway/test-client-123"
	proxy := NewClientProxy(clientID)

	if proxy == nil {
		t.Fatalf("NewClientProxy returned nil")
	}

	if proxy.GetID() != clientID {
		t.Fatalf("ClientProxy ID = %s, want %s", proxy.GetID(), clientID)
	}

	if proxy.MessageChan() == nil {
		t.Fatalf("MessageChan is nil")
	}
}

func TestClientProxyMessageChan(t *testing.T) {
	clientID := "test-gateway/test-client-123"
	proxy := NewClientProxy(clientID)

	msgChan := proxy.MessageChan()
	if msgChan == nil {
		t.Fatalf("MessageChan is nil")
	}

	// Test that we can send a message to the channel
	testMsg := &emptypb.Empty{}
	go func() {
		proxy.PushMessage(testMsg)
	}()

	// Receive the message
	received := <-msgChan
	if received == nil {
		t.Fatalf("Received nil message")
	}
}

func TestClientProxyClose(t *testing.T) {
	clientID := "test-gateway/test-client-123"
	proxy := NewClientProxy(clientID)

	msgChan := proxy.MessageChan()
	if msgChan == nil {
		t.Fatalf("MessageChan is nil before close")
	}

	// Close the proxy
	proxy.Close()

	// After close, the channel should be closed
	// Try to receive from the saved channel reference - should get zero value and closed status
	_, ok := <-msgChan
	if ok {
		t.Fatalf("MessageChan should be closed after Close()")
	}

	// Multiple Close calls should not panic
	proxy.Close()
	proxy.Close()
}

func TestClientProxyGetID(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
	}{
		{"simple ID", "client-1"},
		{"gateway prefix", "localhost:49000/client-123"},
		{"complex ID", "gateway.example.com:8080/abc-def-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := NewClientProxy(tt.clientID)
			if proxy.GetID() != tt.clientID {
				t.Fatalf("GetID() = %s, want %s", proxy.GetID(), tt.clientID)
			}
		})
	}
}
