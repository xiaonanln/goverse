package gateserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestHTTPObject is a simple test object with methods for HTTP testing
type TestHTTPObject struct {
	object.BaseObject
	callCount int
}

func (o *TestHTTPObject) OnCreated() {}

// Echo is a simple method that echoes back the message and tracks call count
// Request format: {message: string}
// Response format: {message: string, callCount: number}
func (o *TestHTTPObject) Echo(ctx context.Context, req *structpb.Struct) (*structpb.Struct, error) {
	o.callCount++

	message := ""
	if req != nil && req.Fields["message"] != nil {
		message = req.Fields["message"].GetStringValue()
	}

	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"message":   structpb.NewStringValue("Echo: " + message),
			"callCount": structpb.NewNumberValue(float64(o.callCount)),
		},
	}, nil
}

// prepareCallObjectRequestBody creates the JSON request body for CallObject API.
// It creates a structpb.Struct with the message, wraps it in an Any, and encodes to base64.
func prepareCallObjectRequestBody(t *testing.T, message string) []byte {
	t.Helper()

	echoReq := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"message": structpb.NewStringValue(message),
		},
	}

	anyReq, err := anypb.New(echoReq)
	if err != nil {
		t.Fatalf("Failed to create Any: %v", err)
	}

	reqBytes, err := proto.Marshal(anyReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	encodedReq := base64.StdEncoding.EncodeToString(reqBytes)
	httpReq := HTTPRequest{Request: encodedReq}
	reqBody, err := json.Marshal(httpReq)
	if err != nil {
		t.Fatalf("Failed to marshal HTTP request: %v", err)
	}

	return reqBody
}

// parseCallObjectResponseBody parses the CallObject response body into a structpb.Struct.
func parseCallObjectResponseBody(t *testing.T, bodyBytes []byte) *structpb.Struct {
	t.Helper()

	var httpResp HTTPResponse
	if err := json.Unmarshal(bodyBytes, &httpResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	respBytes, err := base64.StdEncoding.DecodeString(httpResp.Response)
	if err != nil {
		t.Fatalf("Failed to decode base64 response: %v", err)
	}

	anyResp := &anypb.Any{}
	if err := proto.Unmarshal(respBytes, anyResp); err != nil {
		t.Fatalf("Failed to unmarshal Any response: %v", err)
	}

	var respStruct structpb.Struct
	if err := anyResp.UnmarshalTo(&respStruct); err != nil {
		t.Fatalf("Failed to unmarshal response struct: %v", err)
	}

	return &respStruct
}

// httpCallObjectHelper makes an HTTP call to the CallObject endpoint and returns the response
func httpCallObjectHelper(t *testing.T, client *http.Client, httpBaseURL, objType, objID, method string, message string) (statusCode int, echoResp *structpb.Struct, rawBody []byte) {
	t.Helper()

	reqBody := prepareCallObjectRequestBody(t, message)

	url := fmt.Sprintf("%s/api/v1/objects/call/%s/%s/%s", httpBaseURL, objType, objID, method)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("Failed to create HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, body
	}

	return resp.StatusCode, parseCallObjectResponseBody(t, body), body
}

// TestGateHTTPIntegration tests the HTTP API for create, call, and delete operations
// This test verifies that HTTP requests work correctly to:
// 1. Create an object via HTTP POST
// 2. Call a method on the object via HTTP POST
// 3. Delete the object via HTTP POST
func TestGateHTTPIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestHTTPObject)(nil))

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

	// Start the gate server in a goroutine
	gwStartCtx, gwStartCancel := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel)

	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gwServer.Start(gwStartCtx)
	}()

	// Wait for gate to be ready
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gate server failed to start: %v", err)
		}
	default:
		// Gate is running
	}
	t.Logf("Started real gate server at gRPC=%s, HTTP=%s", gateAddr, httpAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	testutil.WaitForClusterReady(t, nodeCluster)

	// Construct HTTP base URL
	httpBaseURL := fmt.Sprintf("http://%s", httpAddr)
	client := &http.Client{Timeout: 30 * time.Second}

	// Test object ID
	objID := "test-http-object-1"
	objType := "TestHTTPObject"

	t.Run("CreateObjectViaHTTP", func(t *testing.T) {
		// POST /api/v1/objects/create/{type}/{id}
		url := fmt.Sprintf("%s/api/v1/objects/create/%s/%s", httpBaseURL, objType, objID)
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatalf("Failed to create HTTP request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}

		var createResp HTTPCreateResponse
		if err := json.Unmarshal(body, &createResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if createResp.ID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createResp.ID)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Successfully created object %s via HTTP", objID)
	})

	t.Run("CallObjectViaHTTP", func(t *testing.T) {
		statusCode, echoResp, body := httpCallObjectHelper(t, client, httpBaseURL, objType, objID, "Echo", "Hello from HTTP")

		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", statusCode, string(body))
		}

		// Verify response
		expectedMsg := "Echo: Hello from HTTP"
		actualMsg := echoResp.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
		}

		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1, got %d", callCount)
		}

		t.Logf("Successfully called Echo via HTTP, response: %s (call count: %d)", actualMsg, callCount)
	})

	t.Run("CallObjectViaHTTPMultipleTimes", func(t *testing.T) {
		// Call the object multiple times to verify call count increments
		for i := 2; i <= 3; i++ {
			statusCode, echoResp, body := httpCallObjectHelper(t, client, httpBaseURL, objType, objID, "Echo", fmt.Sprintf("Call %d", i))

			if statusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d. Body: %s", statusCode, string(body))
			}

			callCount := int(echoResp.Fields["callCount"].GetNumberValue())
			if callCount != i {
				t.Fatalf("Expected call count %d, got %d", i, callCount)
			}

			t.Logf("Call %d succeeded, call count: %d", i, callCount)
		}
	})

	t.Run("DeleteObjectViaHTTP", func(t *testing.T) {
		// POST /api/v1/objects/delete/{id}
		url := fmt.Sprintf("%s/api/v1/objects/delete/%s", httpBaseURL, objID)
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatalf("Failed to create HTTP request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}

		var deleteResp HTTPDeleteResponse
		if err := json.Unmarshal(body, &deleteResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if !deleteResp.Success {
			t.Fatalf("Expected success=true, got false")
		}

		// Wait for object deletion on the node
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Successfully deleted object %s via HTTP", objID)
	})

	t.Run("VerifyObjectRecreatedOnCall", func(t *testing.T) {
		// In the virtual actor model, calling a deleted object recreates it automatically
		// This test verifies that the object is recreated with a fresh state (callCount = 1)
		statusCode, echoResp, body := httpCallObjectHelper(t, client, httpBaseURL, objType, objID, "Echo", "After recreation")

		// Object is automatically recreated on call (virtual actor model)
		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200 (object recreated), got %d. Body: %s", statusCode, string(body))
		}

		// Verify the object was recreated with fresh state (callCount should be 1)
		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1 (fresh object), got %d", callCount)
		}

		t.Logf("Verified object was recreated with fresh state, callCount: %d", callCount)
	})
}

// curlPostHelper executes a curl POST request and returns the parsed status code and response body.
// If requestBody is nil, it sends a POST request without a body.
func curlPostHelper(t *testing.T, url string, requestBody []byte) (statusCode int, bodyBytes []byte) {
	t.Helper()

	var cmd *exec.Cmd
	if requestBody != nil {
		cmd = exec.Command("curl", "-s", "-w", "\n%{http_code}", "-X", "POST",
			"-H", "Content-Type: application/json",
			"-d", string(requestBody),
			url)
	} else {
		cmd = exec.Command("curl", "-s", "-w", "\n%{http_code}", "-X", "POST",
			"-H", "Content-Type: application/json",
			url)
	}

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("curl command failed: %v", err)
	}

	// Parse output - last line is the status code (we add a newline before status code in -w option)
	lines := bytes.Split(output, []byte("\n"))
	if len(lines) < 1 {
		t.Fatalf("Unexpected curl output format: %s", string(output))
	}

	// Handle case where response body might be empty (status code only)
	statusCodeLine := strings.TrimSpace(string(lines[len(lines)-1]))
	if len(lines) > 1 {
		bodyBytes = bytes.Join(lines[:len(lines)-1], []byte("\n"))
	} else {
		bodyBytes = nil
	}

	parsedCode, err := strconv.Atoi(statusCodeLine)
	if err != nil {
		t.Fatalf("Failed to parse status code from curl output: %v", err)
	}

	return parsedCode, bodyBytes
}

// curlCallObjectHelper makes an HTTP call to the CallObject endpoint using curl and returns the response
func curlCallObjectHelper(t *testing.T, httpBaseURL, objType, objID, method, message string) (statusCode int, echoResp *structpb.Struct, rawOutput string) {
	t.Helper()

	reqBody := prepareCallObjectRequestBody(t, message)
	url := fmt.Sprintf("%s/api/v1/objects/call/%s/%s/%s", httpBaseURL, objType, objID, method)

	statusCode, bodyBytes := curlPostHelper(t, url, reqBody)

	if statusCode != http.StatusOK {
		return statusCode, nil, string(bodyBytes)
	}

	return statusCode, parseCallObjectResponseBody(t, bodyBytes), string(bodyBytes)
}

// TestGateHTTPIntegrationWithCurl tests the HTTP API using curl as client.
// This test verifies that HTTP requests work correctly when using curl:
// 1. Create an object via HTTP POST using curl
// 2. Call a method on the object via HTTP POST using curl
// 3. Delete the object via HTTP POST using curl
func TestGateHTTPIntegrationWithCurl(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test in short mode")
	}
	t.Parallel()

	// Check if curl is available
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("Skipping test: curl is not installed")
	}

	ctx := context.Background()
	etcdPrefix := testutil.PrepareEtcdPrefix(t, "localhost:2379")

	// Create and start a node with cluster
	nodeAddr := testutil.GetFreeAddress()
	nodeCluster := mustNewCluster(ctx, t, nodeAddr, etcdPrefix)
	testNode := nodeCluster.GetThisNode()
	t.Logf("Created node cluster at %s", nodeAddr)

	// Register test object type on the node
	testNode.RegisterObjectType((*TestHTTPObject)(nil))

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

	// Start the gate server in a goroutine
	gwStartCtx, gwStartCancel := context.WithCancel(ctx)
	t.Cleanup(gwStartCancel)

	gwStarted := make(chan error, 1)
	go func() {
		gwStarted <- gwServer.Start(gwStartCtx)
	}()

	// Wait for gate to be ready
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-gwStarted:
		if err != nil {
			t.Fatalf("Gate server failed to start: %v", err)
		}
	default:
		// Gate is running
	}
	t.Logf("Started real gate server at gRPC=%s, HTTP=%s", gateAddr, httpAddr)

	// Wait for shard mapping to be initialized and nodes to discover each other
	testutil.WaitForClusterReady(t, nodeCluster)

	// Construct HTTP base URL
	httpBaseURL := fmt.Sprintf("http://%s", httpAddr)

	// Test object ID - use unique ID for curl tests
	objID := "test-curl-object-1"
	objType := "TestHTTPObject"

	t.Run("CreateObjectViaCurl", func(t *testing.T) {
		// POST /api/v1/objects/create/{type}/{id}
		url := fmt.Sprintf("%s/api/v1/objects/create/%s/%s", httpBaseURL, objType, objID)

		statusCode, bodyBytes := curlPostHelper(t, url, nil)

		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", statusCode, string(bodyBytes))
		}

		var createResp HTTPCreateResponse
		if err := json.Unmarshal(bodyBytes, &createResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if createResp.ID != objID {
			t.Fatalf("Expected object ID %s, got %s", objID, createResp.ID)
		}

		// Wait for object creation on the node
		waitForObjectCreatedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Successfully created object %s via curl", objID)
	})

	t.Run("CallObjectViaCurl", func(t *testing.T) {
		statusCode, echoResp, output := curlCallObjectHelper(t, httpBaseURL, objType, objID, "Echo", "Hello from curl")

		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Output: %s", statusCode, output)
		}

		// Verify response
		expectedMsg := "Echo: Hello from curl"
		actualMsg := echoResp.Fields["message"].GetStringValue()
		if actualMsg != expectedMsg {
			t.Fatalf("Expected message %q, got %q", expectedMsg, actualMsg)
		}

		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1, got %d", callCount)
		}

		t.Logf("Successfully called Echo via curl, response: %s (call count: %d)", actualMsg, callCount)
	})

	t.Run("CallObjectViaCurlMultipleTimes", func(t *testing.T) {
		// Call the object multiple times to verify call count increments
		for i := 2; i <= 3; i++ {
			statusCode, echoResp, output := curlCallObjectHelper(t, httpBaseURL, objType, objID, "Echo", fmt.Sprintf("Call %d", i))

			if statusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d. Output: %s", statusCode, output)
			}

			callCount := int(echoResp.Fields["callCount"].GetNumberValue())
			if callCount != i {
				t.Fatalf("Expected call count %d, got %d", i, callCount)
			}

			t.Logf("Call %d succeeded via curl, call count: %d", i, callCount)
		}
	})

	t.Run("DeleteObjectViaCurl", func(t *testing.T) {
		// POST /api/v1/objects/delete/{id}
		url := fmt.Sprintf("%s/api/v1/objects/delete/%s", httpBaseURL, objID)

		statusCode, bodyBytes := curlPostHelper(t, url, nil)

		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d. Body: %s", statusCode, string(bodyBytes))
		}

		var deleteResp HTTPDeleteResponse
		if err := json.Unmarshal(bodyBytes, &deleteResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		if !deleteResp.Success {
			t.Fatalf("Expected success=true, got false")
		}

		// Wait for object deletion on the node
		waitForObjectDeletedOnNode(t, testNode, objID, 5*time.Second)
		t.Logf("Successfully deleted object %s via curl", objID)
	})

	t.Run("VerifyObjectRecreatedOnCallViaCurl", func(t *testing.T) {
		// In the virtual actor model, calling a deleted object recreates it automatically
		// This test verifies that the object is recreated with a fresh state (callCount = 1)
		statusCode, echoResp, output := curlCallObjectHelper(t, httpBaseURL, objType, objID, "Echo", "After recreation via curl")

		// Object is automatically recreated on call (virtual actor model)
		if statusCode != http.StatusOK {
			t.Fatalf("Expected status 200 (object recreated), got %d. Output: %s", statusCode, output)
		}

		// Verify the object was recreated with fresh state (callCount should be 1)
		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1 (fresh object), got %d", callCount)
		}

		t.Logf("Verified object was recreated with fresh state via curl, callCount: %d", callCount)
	})
}
