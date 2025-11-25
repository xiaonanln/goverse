package gateserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		// Create the request message
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("Hello from HTTP"),
			},
		}

		// Marshal to Any, then to bytes, then to base64
		anyReq, err := anypb.New(echoReq)
		if err != nil {
			t.Fatalf("Failed to create Any: %v", err)
		}

		reqBytes, err := proto.Marshal(anyReq)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		encodedReq := base64.StdEncoding.EncodeToString(reqBytes)

		// Build HTTP request body
		httpReq := HTTPRequest{Request: encodedReq}
		reqBody, err := json.Marshal(httpReq)
		if err != nil {
			t.Fatalf("Failed to marshal HTTP request: %v", err)
		}

		// POST /api/v1/objects/call/{type}/{id}/{method}
		url := fmt.Sprintf("%s/api/v1/objects/call/%s/%s/Echo", httpBaseURL, objType, objID)
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
			t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
		}

		var httpResp HTTPResponse
		if err := json.Unmarshal(body, &httpResp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}

		// Decode base64, then unmarshal protobuf Any
		respBytes, err := base64.StdEncoding.DecodeString(httpResp.Response)
		if err != nil {
			t.Fatalf("Failed to decode base64 response: %v", err)
		}

		anyResp := &anypb.Any{}
		if err := proto.Unmarshal(respBytes, anyResp); err != nil {
			t.Fatalf("Failed to unmarshal Any response: %v", err)
		}

		var echoResp structpb.Struct
		if err := anyResp.UnmarshalTo(&echoResp); err != nil {
			t.Fatalf("Failed to unmarshal response struct: %v", err)
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
			echoReq := &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"message": structpb.NewStringValue(fmt.Sprintf("Call %d", i)),
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

			url := fmt.Sprintf("%s/api/v1/objects/call/%s/%s/Echo", httpBaseURL, objType, objID)
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
			if err != nil {
				t.Fatalf("Failed to create HTTP request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}

			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, string(body))
			}

			var httpResp HTTPResponse
			if err := json.Unmarshal(body, &httpResp); err != nil {
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

			var echoResp structpb.Struct
			if err := anyResp.UnmarshalTo(&echoResp); err != nil {
				t.Fatalf("Failed to unmarshal response struct: %v", err)
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
		echoReq := &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"message": structpb.NewStringValue("After recreation"),
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

		url := fmt.Sprintf("%s/api/v1/objects/call/%s/%s/Echo", httpBaseURL, objType, objID)
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

		// Object is automatically recreated on call (virtual actor model)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200 (object recreated), got %d. Body: %s", resp.StatusCode, string(body))
		}

		var httpResp HTTPResponse
		if err := json.Unmarshal(body, &httpResp); err != nil {
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

		var echoResp structpb.Struct
		if err := anyResp.UnmarshalTo(&echoResp); err != nil {
			t.Fatalf("Failed to unmarshal response struct: %v", err)
		}

		// Verify the object was recreated with fresh state (callCount should be 1)
		callCount := int(echoResp.Fields["callCount"].GetNumberValue())
		if callCount != 1 {
			t.Fatalf("Expected call count 1 (fresh object), got %d", callCount)
		}

		t.Logf("Verified object was recreated with fresh state, callCount: %d", callCount)
	})
}
