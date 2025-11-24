package gateserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

// setupHTTPRoutes configures HTTP routes for the gate server
func (s *GateServer) setupHTTPRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Object operations
	mux.HandleFunc("/api/v1/objects/call/", s.handleCallObject)
	mux.HandleFunc("/api/v1/objects/create/", s.handleCreateObject)
	mux.HandleFunc("/api/v1/objects/delete/", s.handleDeleteObject)

	return mux
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
	defer r.Body.Close()

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

	if objID == "" {
		s.writeError(w, http.StatusBadRequest, "INVALID_PARAMETERS", "Object ID must not be empty")
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

	// Combine metrics handler if metrics are enabled
	if s.config.MetricsListenAddress == "" {
		// No separate metrics server, combine with HTTP server
		// Note: This merges prometheus metrics into the same HTTP server
		// For now, we keep them separate as per existing design
	}

	s.httpServer = &http.Server{
		Addr:    s.config.HTTPListenAddress,
		Handler: mux,
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
