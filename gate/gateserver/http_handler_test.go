package gateserver

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestHTTPRequestResponseEncoding(t *testing.T) {
	// Test that we can correctly encode and decode protobuf Any messages
	// This validates the core encoding/decoding logic used by HTTP handlers

	// Create a test message
	reqMsg := &wrapperspb.StringValue{Value: "hello"}
	anyReq, err := anypb.New(reqMsg)
	if err != nil {
		t.Fatalf("Failed to create Any request: %v", err)
	}

	// Marshal to bytes and encode to base64 (what client would do)
	reqBytes, err := proto.Marshal(anyReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	encodedReq := base64.StdEncoding.EncodeToString(reqBytes)

	// Decode base64 (what server does)
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedReq)
	if err != nil {
		t.Fatalf("Failed to decode base64: %v", err)
	}

	// Unmarshal to Any
	decodedAny := &anypb.Any{}
	if err := proto.Unmarshal(decodedBytes, decodedAny); err != nil {
		t.Fatalf("Failed to unmarshal Any: %v", err)
	}

	// Unmarshal to actual message type
	decodedMsg := &wrapperspb.StringValue{}
	if err := decodedAny.UnmarshalTo(decodedMsg); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if decodedMsg.Value != "hello" {
		t.Fatalf("Expected 'hello', got '%s'", decodedMsg.Value)
	}
}

func TestHandleCallObject_PathParsing(t *testing.T) {
	// Test URL path parsing for CallObject endpoint
	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "method not allowed - GET",
			method:         http.MethodGet,
			path:           "/api/v1/objects/call/TestObject/obj-1/Echo",
			body:           `{"request":""}`,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:           "invalid path - missing method",
			method:         http.MethodPost,
			path:           "/api/v1/objects/call/TestObject/obj-1",
			body:           `{"request":""}`,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_PATH",
		},
		{
			name:           "invalid json body",
			method:         http.MethodPost,
			path:           "/api/v1/objects/call/TestObject/obj-1/Echo",
			body:           `invalid json`,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_JSON",
		},
		{
			name:           "invalid base64",
			method:         http.MethodPost,
			path:           "/api/v1/objects/call/TestObject/obj-1/Echo",
			body:           `{"request":"not-valid-base64!!!"}`,
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_BASE64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal GateServer just for testing HTTP parsing
			gs := &GateServer{}

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			gs.handleCallObject(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errResp.Code != tt.expectedCode {
				t.Fatalf("Expected error code %s, got %s", tt.expectedCode, errResp.Code)
			}
		})
	}
}

func TestHandleCreateObject_PathParsing(t *testing.T) {
	// Test URL path parsing for CreateObject endpoint
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "method not allowed - GET",
			method:         http.MethodGet,
			path:           "/api/v1/objects/create/TestObject/obj-1",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:           "invalid path - missing ID",
			method:         http.MethodPost,
			path:           "/api/v1/objects/create/TestObject",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_PATH",
		},
		{
			name:           "empty type",
			method:         http.MethodPost,
			path:           "/api/v1/objects/create//obj-1",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_PARAMETERS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := &GateServer{}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			gs.handleCreateObject(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errResp.Code != tt.expectedCode {
				t.Fatalf("Expected error code %s, got %s", tt.expectedCode, errResp.Code)
			}
		})
	}
}

func TestHandleDeleteObject_PathParsing(t *testing.T) {
	// Test URL path parsing for DeleteObject endpoint
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "method not allowed - GET",
			method:         http.MethodGet,
			path:           "/api/v1/objects/delete/obj-1",
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:           "empty ID",
			method:         http.MethodPost,
			path:           "/api/v1/objects/delete/",
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "INVALID_PARAMETERS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := &GateServer{}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			gs.handleDeleteObject(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				bodyBytes, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errResp.Code != tt.expectedCode {
				t.Fatalf("Expected error code %s, got %s", tt.expectedCode, errResp.Code)
			}
		})
	}
}

func TestSetupHTTPRoutes(t *testing.T) {
	gs := &GateServer{}

	mux := gs.setupHTTPRoutes()
	if mux == nil {
		t.Fatalf("setupHTTPRoutes returned nil")
	}

	// Verify the mux is not nil - that's all we can test without a full setup
	// The actual route handlers are tested in other test functions
}
