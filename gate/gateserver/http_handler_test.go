package gateserver

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/util/protohelper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestHTTPRequestResponseEncoding(t *testing.T) {
	// Test that we can correctly encode and decode protobuf Any messages
	// This validates the core encoding/decoding logic used by HTTP handlers

	// Create a test message
	reqMsg := &wrapperspb.StringValue{Value: "hello"}
	anyReq, err := protohelper.MsgToAny(reqMsg)
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

			// Read body once
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.Unmarshal(bodyBytes, &errResp); err != nil {
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

			// Read body once
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.Unmarshal(bodyBytes, &errResp); err != nil {
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

			// Read body once
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.Unmarshal(bodyBytes, &errResp); err != nil {
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

	t.Run("setupHTTPClientRoutes", func(t *testing.T) {
		mux := gs.setupHTTPClientRoutes()
		if mux == nil {
			t.Fatalf("setupHTTPClientRoutes returned nil")
		}
		// Verify the mux is not nil - actual route handlers are tested in other test functions
	})

	t.Run("setupHTTPOpsRoutes", func(t *testing.T) {
		mux := gs.setupHTTPOpsRoutes()
		if mux == nil {
			t.Fatalf("setupHTTPOpsRoutes returned nil")
		}
		// Verify the mux is not nil - actual route handlers are tested in other test functions
	})
}

func TestHandleEventsStream_MethodNotAllowed(t *testing.T) {
	// Test that only GET method is allowed for SSE endpoint
	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedCode   string
	}{
		{
			name:           "method not allowed - POST",
			method:         http.MethodPost,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:           "method not allowed - PUT",
			method:         http.MethodPut,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:           "method not allowed - DELETE",
			method:         http.MethodDelete,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedCode:   "METHOD_NOT_ALLOWED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := &GateServer{}

			req := httptest.NewRequest(tt.method, "/api/v1/events/stream", nil)
			w := httptest.NewRecorder()

			gs.handleEventsStream(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
			}

			var errResp HTTPErrorResponse
			if err := json.Unmarshal(bodyBytes, &errResp); err != nil {
				t.Fatalf("Failed to decode error response: %v", err)
			}

			if errResp.Code != tt.expectedCode {
				t.Fatalf("Expected error code %s, got %s", tt.expectedCode, errResp.Code)
			}
		})
	}
}

func TestSSEEventTypes(t *testing.T) {
	// Test that SSE event types are correctly structured

	t.Run("SSERegisterEvent", func(t *testing.T) {
		event := SSERegisterEvent{ClientID: "test-client-123"}
		jsonData, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Failed to marshal SSERegisterEvent: %v", err)
		}

		var decoded SSERegisterEvent
		if err := json.Unmarshal(jsonData, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal SSERegisterEvent: %v", err)
		}

		if decoded.ClientID != "test-client-123" {
			t.Fatalf("Expected clientId 'test-client-123', got '%s'", decoded.ClientID)
		}
	})

	t.Run("SSEMessageEvent", func(t *testing.T) {
		event := SSEMessageEvent{
			Type:    "type.googleapis.com/google.protobuf.StringValue",
			Payload: "SGVsbG8gV29ybGQ=", // Base64 for "Hello World"
		}
		jsonData, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Failed to marshal SSEMessageEvent: %v", err)
		}

		var decoded SSEMessageEvent
		if err := json.Unmarshal(jsonData, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal SSEMessageEvent: %v", err)
		}

		if decoded.Type != "type.googleapis.com/google.protobuf.StringValue" {
			t.Fatalf("Expected type 'type.googleapis.com/google.protobuf.StringValue', got '%s'", decoded.Type)
		}
		if decoded.Payload != "SGVsbG8gV29ybGQ=" {
			t.Fatalf("Expected payload 'SGVsbG8gV29ybGQ=', got '%s'", decoded.Payload)
		}
	})

	t.Run("SSEHeartbeatEvent", func(t *testing.T) {
		event := SSEHeartbeatEvent{}
		jsonData, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Failed to marshal SSEHeartbeatEvent: %v", err)
		}

		// Heartbeat should be an empty JSON object
		if string(jsonData) != "{}" {
			t.Fatalf("Expected '{}', got '%s'", string(jsonData))
		}
	})
}

func TestWriteSSEEvent(t *testing.T) {
	// Test the writeSSEEvent helper function
	gs := &GateServer{}

	tests := []struct {
		name      string
		eventType string
		data      interface{}
		expected  string
	}{
		{
			name:      "register event",
			eventType: "register",
			data:      SSERegisterEvent{ClientID: "client-123"},
			expected:  "event: register\ndata: {\"clientId\":\"client-123\"}\n\n",
		},
		{
			name:      "heartbeat event",
			eventType: "heartbeat",
			data:      SSEHeartbeatEvent{},
			expected:  "event: heartbeat\ndata: {}\n\n",
		},
		{
			name:      "message event",
			eventType: "message",
			data:      SSEMessageEvent{Type: "test.type", Payload: "YWJj"},
			expected:  "event: message\ndata: {\"type\":\"test.type\",\"payload\":\"YWJj\"}\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			// httptest.ResponseRecorder implements both http.ResponseWriter and http.Flusher
			err := gs.writeSSEEvent(w, w, tt.eventType, tt.data)
			if err != nil {
				t.Fatalf("writeSSEEvent failed: %v", err)
			}

			result := w.Body.String()
			if result != tt.expected {
				t.Fatalf("Expected:\n%q\nGot:\n%q", tt.expected, result)
			}
		})
	}
}

func TestPprofEndpoints(t *testing.T) {
	// Test that pprof endpoints are properly registered in ops routes
	gs := &GateServer{}
	mux := gs.setupHTTPOpsRoutes()

	tests := []struct {
		name           string
		path           string
		method         string
		expectedStatus int
	}{
		{
			name:           "pprof index",
			path:           "/debug/pprof/",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "pprof cmdline",
			path:           "/debug/pprof/cmdline",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "pprof symbol",
			path:           "/debug/pprof/symbol",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestHandleHealthz(t *testing.T) {
	// Test the healthz endpoint
	tests := []struct {
		name           string
		method         string
		stopped        bool
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "GET - server running",
			method:         http.MethodGet,
			stopped:        false,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"status":"ok"}`,
		},
		{
			name:           "GET - server stopped",
			method:         http.MethodGet,
			stopped:        true,
			expectedStatus: http.StatusServiceUnavailable,
			expectedBody:   "",
		},
		{
			name:           "POST - method not allowed",
			method:         http.MethodPost,
			stopped:        false,
			expectedStatus: http.StatusMethodNotAllowed,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := &GateServer{}
			gs.stopped.Store(tt.stopped)

			req := httptest.NewRequest(tt.method, "/healthz", nil)
			w := httptest.NewRecorder()

			gs.handleHealthz(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if tt.expectedBody != "" {
				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("Failed to read response body: %v", err)
				}
				bodyStr := strings.TrimSpace(string(bodyBytes))
				if bodyStr != tt.expectedBody {
					t.Fatalf("Expected body %q, got %q", tt.expectedBody, bodyStr)
				}
			}
		})
	}
}

func TestSeparateServerRoutes(t *testing.T) {
	// Test that client API and ops endpoints are properly separated by checking
	// that routes are registered in the appropriate mux
	gs := &GateServer{}
	gs.stopped.Store(false) // Mark as running for healthz/ready tests

	t.Run("client API routes registered", func(t *testing.T) {
		mux := gs.setupHTTPClientRoutes()
		
		// Verify mux is created
		if mux == nil {
			t.Fatal("setupHTTPClientRoutes returned nil")
		}
	})

	t.Run("ops routes registered", func(t *testing.T) {
		mux := gs.setupHTTPOpsRoutes()
		
		// Verify mux is created
		if mux == nil {
			t.Fatal("setupHTTPOpsRoutes returned nil")
		}
		
		// Test healthz and ready endpoints work
		healthzTests := []struct {
			name     string
			endpoint string
			wantCode int
		}{
			{"healthz", "/healthz", http.StatusOK},
			{"ready", "/ready", http.StatusOK},
		}
		
		for _, tt := range healthzTests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tt.endpoint, nil)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				
				if w.Code != tt.wantCode {
					t.Errorf("%s: expected status %d, got %d", tt.endpoint, tt.wantCode, w.Code)
				}
			})
		}
	})

	t.Run("client API should not have ops endpoints", func(t *testing.T) {
		mux := gs.setupHTTPClientRoutes()

		// Test that ops endpoints are NOT in client API
		opsEndpoints := []string{
			"/healthz",
			"/ready",
			"/metrics",
		}

		for _, endpoint := range opsEndpoints {
			req := httptest.NewRequest(http.MethodGet, endpoint, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// We expect 404 for ops endpoints in client API mux
			if w.Code != http.StatusNotFound {
				t.Errorf("Client API should not have ops endpoint %s, but got status %d", endpoint, w.Code)
			}
		}
	})

	t.Run("ops should not have client API endpoints", func(t *testing.T) {
		mux := gs.setupHTTPOpsRoutes()

		// Test that client API endpoints are NOT in ops
		clientEndpoints := []string{
			"/api/v1/objects/call/Type/id/method",
			"/api/v1/objects/create/Type/id",
			"/api/v1/objects/delete/id",
			"/api/v1/events/stream",
		}

		for _, endpoint := range clientEndpoints {
			req := httptest.NewRequest(http.MethodPost, endpoint, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// We expect 404 for client API endpoints in ops mux
			if w.Code != http.StatusNotFound {
				t.Errorf("Ops should not have client API endpoint %s, but got status %d", endpoint, w.Code)
			}
		}
	})
}

func TestHandleReady(t *testing.T) {
// Test the /ready endpoint
tests := []struct {
name           string
method         string
stopped        bool
expectedStatus int
}{
{
name:           "GET when running",
method:         http.MethodGet,
stopped:        false,
expectedStatus: http.StatusOK,
},
{
name:           "GET when stopped",
method:         http.MethodGet,
stopped:        true,
expectedStatus: http.StatusServiceUnavailable,
},
{
name:           "POST not allowed",
method:         http.MethodPost,
stopped:        false,
expectedStatus: http.StatusMethodNotAllowed,
},
}

for _, tt := range tests {
t.Run(tt.name, func(t *testing.T) {
gs := &GateServer{}
if tt.stopped {
gs.stopped.Store(true)
}

req := httptest.NewRequest(tt.method, "/ready", nil)
w := httptest.NewRecorder()

gs.handleReady(w, req)

resp := w.Result()
defer resp.Body.Close()

if resp.StatusCode != tt.expectedStatus {
bodyBytes, _ := io.ReadAll(resp.Body)
t.Fatalf("Expected status %d, got %d. Body: %s", tt.expectedStatus, resp.StatusCode, string(bodyBytes))
}

if tt.method == http.MethodGet && !tt.stopped {
// Verify response body contains "ready"
bodyBytes, err := io.ReadAll(resp.Body)
if err != nil {
t.Fatalf("Failed to read response body: %v", err)
}

if !strings.Contains(string(bodyBytes), "ready") {
t.Errorf("Expected response to contain 'ready', got: %s", string(bodyBytes))
}
}
})
}
}

func TestBackwardCompatibilityMode(t *testing.T) {
// Test that when only HTTPListenAddress is set (backward compatibility mode),
// both client API and ops endpoints are served on the same port
gs := &GateServer{
config: &GateServerConfig{
HTTPListenAddress: ":8080",
// OpsListenAddress not set - backward compatibility mode
},
}
gs.stopped.Store(false)

// In backward compatibility mode, startHTTPServer creates a combined mux
// We can't easily test the actual server start, but we can verify the logic
// by checking that both setupHTTPClientRoutes and handler registration work

clientMux := gs.setupHTTPClientRoutes()
if clientMux == nil {
t.Fatal("setupHTTPClientRoutes returned nil")
}

opsMux := gs.setupHTTPOpsRoutes()
if opsMux == nil {
t.Fatal("setupHTTPOpsRoutes returned nil")
}

// Test that ops handlers work independently
t.Run("healthz handler", func(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
w := httptest.NewRecorder()
gs.handleHealthz(w, req)

if w.Code != http.StatusOK {
t.Errorf("Expected status 200, got %d", w.Code)
}
})

t.Run("ready handler", func(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/ready", nil)
w := httptest.NewRecorder()
gs.handleReady(w, req)

if w.Code != http.StatusOK {
t.Errorf("Expected status 200, got %d", w.Code)
}
})
}

func TestSplitPortMode(t *testing.T) {
// Test that when both HTTPListenAddress and OpsListenAddress are set,
// they are served on separate ports
gs := &GateServer{
config: &GateServerConfig{
HTTPListenAddress: ":8080",
OpsListenAddress:  ":9090",
},
}
gs.stopped.Store(false)

// Verify both muxes can be created
clientMux := gs.setupHTTPClientRoutes()
if clientMux == nil {
t.Fatal("setupHTTPClientRoutes returned nil")
}

opsMux := gs.setupHTTPOpsRoutes()
if opsMux == nil {
t.Fatal("setupHTTPOpsRoutes returned nil")
}

// Test that they serve different endpoints
t.Run("client mux should not have ops endpoints", func(t *testing.T) {
req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
w := httptest.NewRecorder()
clientMux.ServeHTTP(w, req)

if w.Code != http.StatusNotFound {
t.Errorf("Client mux should not serve /healthz, got status %d", w.Code)
}
})

t.Run("ops mux should not have client endpoints", func(t *testing.T) {
req := httptest.NewRequest(http.MethodPost, "/api/v1/objects/call/Type/id/method", nil)
w := httptest.NewRecorder()
opsMux.ServeHTTP(w, req)

if w.Code != http.StatusNotFound {
t.Errorf("Ops mux should not serve client API, got status %d", w.Code)
}
})
}
