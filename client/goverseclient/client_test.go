package goverseclient

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		opts      []Option
		wantErr   bool
		errType   error
	}{
		{
			name:      "valid addresses",
			addresses: []string{"localhost:48000", "localhost:48001"},
			opts:      nil,
			wantErr:   false,
		},
		{
			name:      "single address",
			addresses: []string{"localhost:48000"},
			opts:      nil,
			wantErr:   false,
		},
		{
			name:      "no addresses",
			addresses: []string{},
			opts:      nil,
			wantErr:   true,
			errType:   ErrNoAddresses,
		},
		{
			name:      "nil addresses",
			addresses: nil,
			opts:      nil,
			wantErr:   true,
			errType:   ErrNoAddresses,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.addresses, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errType != nil && err != tt.errType {
				t.Errorf("NewClient() error = %v, want %v", err, tt.errType)
				return
			}
			if !tt.wantErr {
				if client == nil {
					t.Error("NewClient() returned nil client")
				}
				if len(client.addresses) != len(tt.addresses) {
					t.Errorf("NewClient() addresses = %v, want %v", client.addresses, tt.addresses)
				}
			}
		})
	}
}

func TestNewClientWithOptions(t *testing.T) {
	client, err := NewClient(
		[]string{"localhost:48000"},
		WithConnectionTimeout(60*time.Second),
		WithCallTimeout(10*time.Second),
		WithReconnectInterval(2*time.Second),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.options.ConnectionTimeout != 60*time.Second {
		t.Errorf("ConnectionTimeout = %v, want %v", client.options.ConnectionTimeout, 60*time.Second)
	}
	if client.options.CallTimeout != 10*time.Second {
		t.Errorf("CallTimeout = %v, want %v", client.options.CallTimeout, 10*time.Second)
	}
	if client.options.ReconnectInterval != 2*time.Second {
		t.Errorf("ReconnectInterval = %v, want %v", client.options.ReconnectInterval, 2*time.Second)
	}
}

func TestClientDefaultOptions(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.options.ConnectionTimeout != DefaultConnectionTimeout {
		t.Errorf("ConnectionTimeout = %v, want %v", client.options.ConnectionTimeout, DefaultConnectionTimeout)
	}
	if client.options.CallTimeout != DefaultCallTimeout {
		t.Errorf("CallTimeout = %v, want %v", client.options.CallTimeout, DefaultCallTimeout)
	}
	if client.options.ReconnectInterval != DefaultReconnectInterval {
		t.Errorf("ReconnectInterval = %v, want %v", client.options.ReconnectInterval, DefaultReconnectInterval)
	}
	if client.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestClientInitialState(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.IsConnected() {
		t.Error("IsConnected() should be false initially")
	}
	if client.ClientID() != "" {
		t.Error("ClientID() should be empty initially")
	}
	if client.CurrentAddress() != "" {
		t.Error("CurrentAddress() should be empty initially")
	}
	if client.messageChan == nil {
		t.Error("messageChan should not be nil")
	}
}

func TestClientClose(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Close should work even when not connected
	err = client.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Double close should be safe
	err = client.Close()
	if err != nil {
		t.Errorf("Close() second call error = %v", err)
	}

	// Operations after close should return ErrClientClosed
	ctx := context.Background()
	err = client.Connect(ctx)
	if err != ErrClientClosed {
		t.Errorf("Connect() after close error = %v, want %v", err, ErrClientClosed)
	}

	_, err = client.CallObject(ctx, "Type", "ID", "Method", nil)
	if err != ErrClientClosed {
		t.Errorf("CallObject() after close error = %v, want %v", err, ErrClientClosed)
	}

	_, err = client.CreateObject(ctx, "Type", "ID")
	if err != ErrClientClosed {
		t.Errorf("CreateObject() after close error = %v, want %v", err, ErrClientClosed)
	}

	err = client.DeleteObject(ctx, "ID")
	if err != ErrClientClosed {
		t.Errorf("DeleteObject() after close error = %v, want %v", err, ErrClientClosed)
	}
}

func TestClientNotConnected(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	_, err = client.CallObject(ctx, "Type", "ID", "Method", nil)
	if err != ErrNotConnected {
		t.Errorf("CallObject() when not connected error = %v, want %v", err, ErrNotConnected)
	}

	_, err = client.CreateObject(ctx, "Type", "ID")
	if err != ErrNotConnected {
		t.Errorf("CreateObject() when not connected error = %v, want %v", err, ErrNotConnected)
	}

	err = client.DeleteObject(ctx, "ID")
	if err != ErrNotConnected {
		t.Errorf("DeleteObject() when not connected error = %v, want %v", err, ErrNotConnected)
	}
}

func TestClientConnectToNonExistentServer(t *testing.T) {
	client, err := NewClient([]string{"localhost:1", "localhost:2"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	// Use a short context timeout to avoid waiting too long
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	if err == nil {
		t.Error("Connect() should fail for non-existent servers")
	}
}

func TestClientAddressesCopied(t *testing.T) {
	original := []string{"localhost:48000", "localhost:48001"}
	client, err := NewClient(original)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Modify the original slice
	original[0] = "modified"

	// Client should have the original value
	if client.addresses[0] == "modified" {
		t.Error("Client addresses should be copied, not referenced")
	}
}

func TestWithCallbacks(t *testing.T) {
	var connectCalled bool
	var disconnectCalled bool
	var messageCalled bool

	client, err := NewClient(
		[]string{"localhost:48000"},
		WithOnConnect(func(clientID string) {
			connectCalled = true
		}),
		WithOnDisconnect(func(err error) {
			disconnectCalled = true
		}),
		WithOnMessage(func(msg proto.Message) {
			messageCalled = true
		}),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	if client.options.OnConnect == nil {
		t.Error("OnConnect callback should be set")
	}
	if client.options.OnDisconnect == nil {
		t.Error("OnDisconnect callback should be set")
	}
	if client.options.OnMessage == nil {
		t.Error("OnMessage callback should be set")
	}

	// Note: Callbacks are not called in these tests since we don't connect
	_ = connectCalled
	_ = disconnectCalled
	_ = messageCalled
}

func TestClientMessageChannel(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Get message channel
	ch := client.MessageChan()
	if ch == nil {
		t.Fatal("MessageChan() should not return nil")
	}

	// Channel should be empty
	select {
	case <-ch:
		t.Error("MessageChan() should be empty initially")
	default:
		// Expected
	}

	// Close the client
	client.Close()

	// Channel should be closed after client is closed
	_, ok := <-ch
	if ok {
		t.Error("MessageChan() should be closed after client is closed")
	}
}

func TestWaitForConnection(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	// WaitForConnection should return error when context is cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = client.WaitForConnection(ctx)
	if err == nil {
		t.Error("WaitForConnection() should fail when not connected and context expires")
	}
}

func TestReconnect(t *testing.T) {
	client, err := NewClient([]string{"localhost:1"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Reconnect should fail for non-existent server
	err = client.Reconnect(ctx)
	if err == nil {
		t.Error("Reconnect() should fail for non-existent server")
	}
}

func TestReconnectWhenClosed(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	client.Close()

	ctx := context.Background()
	err = client.Reconnect(ctx)
	if err != ErrClientClosed {
		t.Errorf("Reconnect() after close error = %v, want %v", err, ErrClientClosed)
	}
}

func TestClientCallObjectAnyNotConnected(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	_, err = client.CallObjectAny(ctx, "Type", "ID", "Method", nil)
	if err != ErrNotConnected {
		t.Errorf("CallObjectAny() when not connected error = %v, want %v", err, ErrNotConnected)
	}
}

func TestClientCallObjectAnyClosed(t *testing.T) {
	client, err := NewClient([]string{"localhost:48000"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	client.Close()

	ctx := context.Background()
	_, err = client.CallObjectAny(ctx, "Type", "ID", "Method", nil)
	if err != ErrClientClosed {
		t.Errorf("CallObjectAny() after close error = %v, want %v", err, ErrClientClosed)
	}
}
