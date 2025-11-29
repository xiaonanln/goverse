package gateserver

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// parseSSEEvent parses a single SSE event from the scanner.
// Returns the event type and data, or empty strings if no event found.
func parseSSEEvent(scanner *bufio.Scanner) (eventType string, data string) {
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// End of event
			return eventType, data
		}
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	return eventType, data
}

// TestGateSSEIntegration tests the SSE push messaging via HTTP
// This test verifies that:
// 1. Client can connect to SSE endpoint
// 2. Client receives register event with client ID
// 3. Client can receive pushed messages from objects
func TestGateSSEIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Start mock gRPC server for the node to handle inter-node communication
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(testNode)
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })
	t.Logf("Started mock node server at %s", nodeAddr)

	// Create and start the real GateServer with HTTP enabled
	gateAddr := testutil.GetFreeAddress()
	httpAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:     gateAddr,
		AdvertiseAddress:  gateAddr,
		HTTPListenAddress: httpAddr,
		EtcdAddress:       "localhost:2379",
		EtcdPrefix:        etcdPrefix,
		NumShards:         testutil.TestNumShards,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	// Start the gate server (non-blocking)
	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}
	t.Logf("Started real gate server at gRPC=%s, HTTP=%s", gateAddr, httpAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	testutil.WaitForClusterReady(t, nodeCluster)

	// Wait for gate to register with the node
	testutil.WaitFor(t, 10*time.Second, "gate to register with node", func() bool {
		return nodeCluster.IsGateConnected(gateAddr)
	})
	t.Logf("Gate %s registered with node %s", gateAddr, nodeAddr)

	// Construct HTTP SSE URL
	sseURL := fmt.Sprintf("http://%s/api/v1/events/stream", httpAddr)

	// Create an HTTP request for SSE
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		t.Fatalf("Failed to create SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Connect to SSE endpoint
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SSE connection failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Expected content type 'text/event-stream', got '%s'", contentType)
	}
	t.Logf("SSE connection established, Content-Type: %s", contentType)

	// Read SSE events
	scanner := bufio.NewScanner(resp.Body)

	// First event should be 'register' with client ID
	eventType, eventData := parseSSEEvent(scanner)
	if eventType != "register" {
		t.Fatalf("Expected 'register' event, got '%s'", eventType)
	}

	var regEvent SSERegisterEvent
	if err := json.Unmarshal([]byte(eventData), &regEvent); err != nil {
		t.Fatalf("Failed to unmarshal register event: %v", err)
	}

	if regEvent.ClientID == "" {
		t.Fatalf("Expected non-empty client ID")
	}

	// Verify client ID has gate address prefix
	expectedPrefix := gateAddr + "/"
	if !strings.HasPrefix(regEvent.ClientID, expectedPrefix) {
		t.Fatalf("Expected client ID to start with %q, got %q", expectedPrefix, regEvent.ClientID)
	}
	t.Logf("Received register event, clientId: %s", regEvent.ClientID)

	// Push a message to the client via cluster
	testMessage := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"text":      structpb.NewStringValue("Hello from SSE test!"),
			"timestamp": structpb.NewNumberValue(float64(time.Now().Unix())),
		},
	}

	err = nodeCluster.PushMessageToClient(ctx, regEvent.ClientID, testMessage)
	if err != nil {
		t.Fatalf("Failed to push message from node: %v", err)
	}
	t.Logf("Node pushed message to client %s", regEvent.ClientID)

	// Read the message event
	eventType, eventData = parseSSEEvent(scanner)
	if eventType != "message" {
		t.Fatalf("Expected 'message' event, got '%s'", eventType)
	}

	var msgEvent SSEMessageEvent
	if err := json.Unmarshal([]byte(eventData), &msgEvent); err != nil {
		t.Fatalf("Failed to unmarshal message event: %v", err)
	}

	// Verify the message type
	if !strings.Contains(msgEvent.Type, "Struct") {
		t.Fatalf("Expected message type to contain 'Struct', got '%s'", msgEvent.Type)
	}

	// Decode the payload
	payloadBytes, err := base64.StdEncoding.DecodeString(msgEvent.Payload)
	if err != nil {
		t.Fatalf("Failed to decode payload base64: %v", err)
	}

	// Unmarshal the Any
	anyMsg := &anypb.Any{}
	if err := proto.Unmarshal(payloadBytes, anyMsg); err != nil {
		t.Fatalf("Failed to unmarshal Any: %v", err)
	}

	// Unmarshal to Struct
	var receivedStruct structpb.Struct
	if err := anyMsg.UnmarshalTo(&receivedStruct); err != nil {
		t.Fatalf("Failed to unmarshal Struct: %v", err)
	}

	// Verify message content
	text := receivedStruct.Fields["text"].GetStringValue()
	if text != "Hello from SSE test!" {
		t.Fatalf("Expected text 'Hello from SSE test!', got '%s'", text)
	}

	t.Logf("SSE client successfully received message: %s", text)
}

// TestSSEMethodNotAllowed verifies that only GET method is allowed for SSE endpoint
func TestSSEMethodNotAllowed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	t.Cleanup(func() { nodeCluster.Stop(ctx) })

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })

	// Create and start the GateServer with HTTP enabled
	gateAddr := testutil.GetFreeAddress()
	httpAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:     gateAddr,
		AdvertiseAddress:  gateAddr,
		HTTPListenAddress: httpAddr,
		EtcdAddress:       "localhost:2379",
		EtcdPrefix:        etcdPrefix,
		NumShards:         testutil.TestNumShards,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}

	// Wait for cluster to be ready (this also ensures the HTTP server is listening)
	testutil.WaitForClusterReady(t, nodeCluster)

	sseURL := fmt.Sprintf("http://%s/api/v1/events/stream", httpAddr)
	client := &http.Client{Timeout: 5 * time.Second}

	// Test POST method
	resp, err := client.Post(sseURL, "application/json", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("Expected status 405 for POST, got %d", resp.StatusCode)
	}

	t.Logf("POST to SSE endpoint correctly returned 405 Method Not Allowed")
}

// TestSSEMultipleClients tests that multiple SSE clients can connect and receive their own messages
func TestSSEMultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	t.Logf("Created node cluster at %s", nodeAddr)

	// Start mock gRPC server for the node
	mockNodeServer := testutil.NewMockGoverseServer()
	mockNodeServer.SetNode(nodeCluster.GetThisNode())
	mockNodeServer.SetCluster(nodeCluster)
	nodeServer := testutil.NewTestServerHelper(nodeAddr, mockNodeServer)
	err := nodeServer.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start node mock server: %v", err)
	}
	t.Cleanup(func() { nodeServer.Stop() })

	// Create and start the GateServer with HTTP enabled
	gateAddr := testutil.GetFreeAddress()
	httpAddr := testutil.GetFreeAddress()
	gwServerConfig := &GateServerConfig{
		ListenAddress:     gateAddr,
		AdvertiseAddress:  gateAddr,
		HTTPListenAddress: httpAddr,
		EtcdAddress:       "localhost:2379",
		EtcdPrefix:        etcdPrefix,
		NumShards:         testutil.TestNumShards,
	}
	gwServer, err := NewGateServer(gwServerConfig)
	if err != nil {
		t.Fatalf("Failed to create gate server: %v", err)
	}
	t.Cleanup(func() { gwServer.Stop() })

	if err := gwServer.Start(ctx); err != nil {
		t.Fatalf("Gate server failed to start: %v", err)
	}

	// Wait for cluster to be ready
	testutil.WaitForClusterReady(t, nodeCluster)

	// Wait for gate to register with the node
	testutil.WaitFor(t, 10*time.Second, "gate to register with node", func() bool {
		return nodeCluster.IsGateConnected(gateAddr)
	})

	sseURL := fmt.Sprintf("http://%s/api/v1/events/stream", httpAddr)

	// Connect multiple SSE clients
	numClients := 3
	type sseClient struct {
		clientID string
		scanner  *bufio.Scanner
		resp     *http.Response
	}
	clients := make([]sseClient, numClients)

	for i := 0; i < numClients; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
		if err != nil {
			t.Fatalf("Failed to create SSE request for client %d: %v", i, err)
		}
		req.Header.Set("Accept", "text/event-stream")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("SSE connection failed for client %d: %v", i, err)
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)

		// Read register event
		eventType, eventData := parseSSEEvent(scanner)
		if eventType != "register" {
			t.Fatalf("Client %d: Expected 'register' event, got '%s'", i, eventType)
		}

		var regEvent SSERegisterEvent
		if err := json.Unmarshal([]byte(eventData), &regEvent); err != nil {
			t.Fatalf("Client %d: Failed to unmarshal register event: %v", i, err)
		}

		clients[i] = sseClient{
			clientID: regEvent.ClientID,
			scanner:  scanner,
			resp:     resp,
		}
		t.Logf("Client %d registered with ID: %s", i, regEvent.ClientID)
	}

	// Push different messages to each client
	for i, client := range clients {
		testMessage := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"clientNum": structpb.NewNumberValue(float64(i)),
				"message":   structpb.NewStringValue(fmt.Sprintf("Hello to client %d", i)),
			},
		}

		err = nodeCluster.PushMessageToClient(ctx, client.clientID, testMessage)
		if err != nil {
			t.Fatalf("Failed to push message to client %d: %v", i, err)
		}
		t.Logf("Pushed message to client %d", i)
	}

	// Verify each client receives their specific message
	for i, client := range clients {
		eventType, eventData := parseSSEEvent(client.scanner)
		if eventType != "message" {
			t.Fatalf("Client %d: Expected 'message' event, got '%s'", i, eventType)
		}

		var msgEvent SSEMessageEvent
		if err := json.Unmarshal([]byte(eventData), &msgEvent); err != nil {
			t.Fatalf("Client %d: Failed to unmarshal message event: %v", i, err)
		}

		// Decode and verify the message
		payloadBytes, err := base64.StdEncoding.DecodeString(msgEvent.Payload)
		if err != nil {
			t.Fatalf("Client %d: Failed to decode payload: %v", i, err)
		}

		anyMsg := &anypb.Any{}
		if err := proto.Unmarshal(payloadBytes, anyMsg); err != nil {
			t.Fatalf("Client %d: Failed to unmarshal Any: %v", i, err)
		}

		var receivedStruct structpb.Struct
		if err := anyMsg.UnmarshalTo(&receivedStruct); err != nil {
			t.Fatalf("Client %d: Failed to unmarshal Struct: %v", i, err)
		}

		expectedMsg := fmt.Sprintf("Hello to client %d", i)
		actualMsg := receivedStruct.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Client %d: Expected message %q, got %q", i, expectedMsg, actualMsg)
		}

		clientNum := int(receivedStruct.Fields["clientNum"].GetNumberValue())
		if clientNum != i {
			t.Fatalf("Client %d: Expected clientNum %d, got %d", i, i, clientNum)
		}

		t.Logf("Client %d correctly received message: %s", i, actualMsg)
	}
}
