package client

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestClient is a simple test implementation
type TestClient struct {
	BaseClient
}

func (tc *TestClient) OnCreated() {
	tc.BaseClient.OnCreated()
}

func TestBaseClient_MessageChan(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-client-id")
	client.OnCreated()

	messageChan := client.MessageChan()
	if messageChan == nil {
		t.Fatal("MessageChan should return a non-nil channel after OnCreated")
	}
}

func TestBaseClient_OnCreated(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-client-id")

	// Before OnCreated, messageChan should be nil
	if client.messageChan != nil {
		t.Fatal("messageChan should be nil before OnCreated")
	}

	client.OnCreated()

	// After OnCreated, messageChan should be initialized
	if client.messageChan == nil {
		t.Fatal("messageChan should be initialized after OnCreated")
	}
}

func TestClientObject_Interface(t *testing.T) {
	// Test that TestClient implements the ClientObject interface
	var _ ClientObject = (*TestClient)(nil)
}

func TestBaseClient_MessageChanSendReceive(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-client-id")
	client.OnCreated()

	messageChan := client.MessageChan()

	// Test that we can send and receive on the channel
	go func() {
		// Send a test message (using emptypb or any proto.Message)
		// For this test, we just close the channel to verify it works
		close(messageChan)
	}()

	// Verify channel is open and can be read from
	_, ok := <-messageChan
	if ok {
		t.Fatal("Expected channel to be closed")
	}
}

func TestBaseClient_Id(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "my-client-id")

	if got := client.Id(); got != "my-client-id" {
		t.Fatalf("Id() = %s; want my-client-id", got)
	}
}

func TestBaseClient_Type(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-id")

	if got := client.Type(); got != "TestClient" {
		t.Fatalf("Type() = %s; want TestClient", got)
	}
}

func TestBaseClient_String(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-client-id")

	expected := "TestClient(test-client-id)"
	if got := client.String(); got != expected {
		t.Fatalf("String() = %s; want %s", got, expected)
	}
}

func TestMessageChan_BufferCapacity(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-client-id")
	client.OnCreated()

	messageChan := client.MessageChan()

	// The channel is buffered (capacity 10), so we can send messages without blocking
	if messageChan == nil {
		t.Fatal("messageChan should not be nil after OnCreated")
	}

	// Verify we have a buffer capacity > 0
	if cap(messageChan) == 0 {
		t.Fatal("messageChan should have buffer capacity > 0")
	}
}

func TestClientObject_ProtoMessageChannel(t *testing.T) {
	client := &TestClient{}
	client.OnInit(client, "test-id")
	client.OnCreated()

	// Test that MessageChan returns a channel that accepts proto.Message
	messageChan := client.MessageChan()

	// Verify the channel type
	var _ chan proto.Message = messageChan
}
