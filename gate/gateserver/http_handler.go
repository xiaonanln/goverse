package gateserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xiaonanln/goverse/util/callcontext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// HTTPRequest represents the standard HTTP request format for object operations
type HTTPRequest struct {
	Request string `json:"request"` // Base64-encoded protobuf Any bytes
}

// HTTPResponse represents the standard HTTP response format for object operations
type HTTPResponse struct {
	Response string `json:"response"` // Base64-encoded protobuf Any bytes
}

// HTTPCreateResponse represents the response for object creation
type HTTPCreateResponse struct {
	ID string `json:"id"`
}

// HTTPDeleteResponse represents the response for object deletion
type HTTPDeleteResponse struct {
	Success bool `json:"success"`
}

// HTTPErrorResponse represents an error response
type HTTPErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// SSERegisterEvent represents the register event payload for SSE
type SSERegisterEvent struct {
	ClientID string `json:"clientId"`
}

// SSEMessageEvent represents a push message event payload for SSE
type SSEMessageEvent struct {
	Type    string `json:"type"`    // Protobuf type URL from Any
	Payload string `json:"payload"` // Base64-encoded protobuf Any bytes
}

// SSEHeartbeatEvent represents a heartbeat event payload for SSE
type SSEHeartbeatEvent struct {
	// Empty struct - heartbeat has no payload
}

// sseHeartbeatInterval is the interval between SSE heartbeat events
const sseHeartbeatInterval = 30 * time.Second

// setupHTTPRoutes configures HTTP routes for the gate server
func (s *GateServer) setupHTTPRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Object operations with CORS support
	mux.HandleFunc("/api/v1/objects/call/", s.corsMiddleware(s.handleCallObject))
	mux.HandleFunc("/api/v1/objects/create/", s.corsMiddleware(s.handleCreateObject))
	mux.HandleFunc("/api/v1/objects/delete/", s.corsMiddleware(s.handleDeleteObject))

	// SSE events stream for push messaging
	mux.HandleFunc("/api/v1/events/stream", s.corsMiddleware(s.handleEventsStream))

	// Prometheus metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// pprof profiling endpoints
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	return mux
}

// corsMiddleware adds CORS headers to allow cross-origin requests
func (s *GateServer) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Client-ID, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next(w, r)
	}
}

// handleCallObject handles HTTP POST /api/v1/objects/call/{type}/{id}/{method}
func (s *GateServer) handleCallObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	// Parse URL path: /api/v1/objects/call/{type}/{id}/{method}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/objects/call/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) != 3 {
		s.writeError(w, http.StatusBadRequest, "INVALID_PATH", "Path must be /api/v1/objects/call/{type}/{id}/{method}")
		return
	}

	objType := parts[0]
	objID := parts[1]
	method := parts[2]

	if objType == "" || objID == "" || method == "" {
		s.writeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "Type, ID, and method must not be empty")
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_BODY", fmt.Sprintf("Failed to read request body: %v", err))
		return
	}

	var httpReq HTTPRequest
	if err := json.Unmarshal(body, &httpReq); err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_JSON", fmt.Sprintf("Failed to parse JSON: %v", err))
		return
	}

	// Decode base64 request
	requestBytes, err := base64.StdEncoding.DecodeString(httpReq.Request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_BASE64", fmt.Sprintf("Failed to decode base64 request: %v", err))
		return
	}

	// Unmarshal to Any
	anyReq := &anypb.Any{}
	if err := proto.Unmarshal(requestBytes, anyReq); err != nil {
		s.writeError(w, http.StatusBadRequest, "INVALID_PROTOBUF", fmt.Sprintf("Failed to unmarshal protobuf Any: %v", err))
		return
	}

	// Create context with client ID if provided
	ctx := r.Context()
	clientID := r.Header.Get("X-Client-ID")
	if clientID != "" {
		ctx = callcontext.WithClientID(ctx, clientID)
	}

	// Call the object via cluster
	anyResp, err := s.cluster.CallObjectAnyRequest(ctx, objType, objID, method, anyReq)
	if err != nil {
		// Map cluster errors to HTTP status codes
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, http.StatusNotFound, "OBJECT_NOT_FOUND", err.Error())
		} else {
			s.writeError(w, http.StatusInternalServerError, "CALL_FAILED", err.Error())
		}
		return
	}

	// Marshal response Any to bytes
	respBytes, err := proto.Marshal(anyResp)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "MARSHAL_ERROR", fmt.Sprintf("Failed to marshal response: %v", err))
		return
	}

	// Encode to base64
	encodedResp := base64.StdEncoding.EncodeToString(respBytes)

	// Write response
	httpResp := HTTPResponse{Response: encodedResp}
	s.writeJSON(w, http.StatusOK, httpResp)
}

// handleCreateObject handles HTTP POST /api/v1/objects/create/{type}/{id}
func (s *GateServer) handleCreateObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	// Parse URL path: /api/v1/objects/create/{type}/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/objects/create/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		s.writeError(w, http.StatusBadRequest, "INVALID_PATH", "Path must be /api/v1/objects/create/{type}/{id}")
		return
	}

	objType := parts[0]
	objID := parts[1]

	if objType == "" || objID == "" {
		s.writeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "Type and ID must not be empty")
		return
	}

	// Create context
	ctx := r.Context()

	// Create the object via cluster
	createdID, err := s.cluster.CreateObject(ctx, objType, objID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	// Write response
	httpResp := HTTPCreateResponse{ID: createdID}
	s.writeJSON(w, http.StatusOK, httpResp)
}

// handleDeleteObject handles HTTP POST /api/v1/objects/delete/{id}
func (s *GateServer) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed")
		return
	}

	// Parse URL path: /api/v1/objects/delete/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/objects/delete/")
	objID := strings.TrimSpace(path)

	if objID == "" || strings.Contains(objID, "/") {
		s.writeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "Object ID must not be empty and must not contain slashes")
		return
	}

	// Create context
	ctx := r.Context()

	// Delete the object via cluster
	err := s.cluster.DeleteObject(ctx, objID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	// Write response
	httpResp := HTTPDeleteResponse{Success: true}
	s.writeJSON(w, http.StatusOK, httpResp)
}

// handleEventsStream handles GET /api/v1/events/stream for Server-Sent Events (SSE)
// This allows HTTP clients to receive pushed messages from objects in real-time.
func (s *GateServer) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET method is allowed for SSE")
		return
	}

	// Verify the response writer supports flushing (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "SSE_NOT_SUPPORTED", "Streaming not supported by server")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Register client with the gate
	ctx := r.Context()
	clientProxy := s.gate.Register(ctx)
	clientID := clientProxy.GetID()

	// Make sure to unregister when done
	defer s.gate.Unregister(clientID)

	s.logger.Infof("SSE client connected: %s", clientID)

	// Send register event with client ID
	regEvent := SSERegisterEvent{ClientID: clientID}
	if err := s.writeSSEEvent(w, flusher, "register", regEvent); err != nil {
		s.logger.Errorf("Failed to send register event to SSE client %s: %v", clientID, err)
		return
	}

	// Create heartbeat ticker
	heartbeatTicker := time.NewTicker(sseHeartbeatInterval)
	defer heartbeatTicker.Stop()

	// Stream messages to the client
	for {
		select {
		case <-ctx.Done():
			s.logger.Infof("SSE client %s disconnected: %v", clientID, ctx.Err())
			return
		case <-heartbeatTicker.C:
			// Send heartbeat to keep connection alive
			if err := s.writeSSEEvent(w, flusher, "heartbeat", SSEHeartbeatEvent{}); err != nil {
				s.logger.Errorf("Failed to send heartbeat to SSE client %s: %v", clientID, err)
				return
			}
		case anyMsg, ok := <-clientProxy.MessageChan():
			if !ok {
				s.logger.Infof("SSE client %s message channel closed", clientID)
				return
			}

			// Marshal Any to bytes and encode as base64
			msgBytes, err := proto.Marshal(anyMsg)
			if err != nil {
				s.logger.Errorf("Failed to marshal message for SSE client %s: %v", clientID, err)
				continue
			}

			// Create message event with type and base64-encoded payload
			msgEvent := SSEMessageEvent{
				Type:    anyMsg.GetTypeUrl(),
				Payload: base64.StdEncoding.EncodeToString(msgBytes),
			}

			if err := s.writeSSEEvent(w, flusher, "message", msgEvent); err != nil {
				s.logger.Errorf("Failed to send message to SSE client %s: %v", clientID, err)
				return
			}

			s.logger.Debugf("Sent message to SSE client %s", clientID)
		}
	}
}

// writeSSEEvent writes a Server-Sent Event to the response writer
func (s *GateServer) writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) error {
	// Marshal data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Write SSE format: event: <type>\ndata: <json>\n\n
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}

	flusher.Flush()
	return nil
}

// writeJSON writes a JSON response with the given status code
func (s *GateServer) writeJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Errorf("Failed to encode JSON response: %v", err)
	}
}

// writeError writes an error response in JSON format
func (s *GateServer) writeError(w http.ResponseWriter, statusCode int, code string, message string) {
	errResp := HTTPErrorResponse{
		Error: message,
		Code:  code,
	}
	s.writeJSON(w, statusCode, errResp)
}

// startHTTPServer starts the HTTP server for REST API
func (s *GateServer) startHTTPServer(ctx context.Context) error {
	if s.config.HTTPListenAddress == "" {
		return nil // HTTP not enabled
	}

	mux := s.setupHTTPRoutes()

	s.httpServer = &http.Server{
		Addr:              s.config.HTTPListenAddress,
		Handler:           mux,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		s.logger.Infof("HTTP gate server listening on %s", s.config.HTTPListenAddress)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("HTTP server error: %v", err)
		}
		s.logger.Infof("HTTP gate server stopped")
	}()

	return nil
}

// stopHTTPServer stops the HTTP server
func (s *GateServer) stopHTTPServer() error {
	if s.httpServer == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Errorf("HTTP server shutdown error: %v", err)
		return err
	}

	return nil
}
