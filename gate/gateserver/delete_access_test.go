package gateserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/config"
	gate_pb "github.com/xiaonanln/goverse/gate/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestHandleDeleteObject_LifecycleRejected pins the gate's HTTP
// handleDeleteObject lifecycle check on the typed
// /api/v1/objects/delete/{type}/{id} route. Mirrors
// TestHandleCreateObject_LifecycleRejected; without this check, an
// INTERNAL/REJECT lifecycle rule would only be enforced for
// streaming-gRPC clients and a browser POST would slip past gate-side
// validation.
func TestHandleDeleteObject_LifecycleRejected(t *testing.T) {
	rules := []config.LifecycleRule{
		{Type: "Restricted", Lifecycle: "DELETE", Access: "INTERNAL"},
	}
	validator, err := config.NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("NewLifecycleValidator: %v", err)
	}

	s := &GateServer{
		logger:             logger.NewLogger("test-gate"),
		lifecycleValidator: validator,
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/objects/delete/Restricted/some-id", nil)
	w := httptest.NewRecorder()

	s.handleDeleteObject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403 Forbidden", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	var errResp HTTPErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("decode error response: %v\nbody=%s", err, string(respBody))
	}
	if errResp.Code != "ACCESS_DENIED" {
		t.Fatalf("error code = %q; want ACCESS_DENIED. body=%s", errResp.Code, string(respBody))
	}
}

// TestHandleDeleteObject_RejectsUntypedPath confirms the HTTP delete
// route requires the typed form /api/v1/objects/delete/{type}/{id}.
// The single-segment path is rejected with 400 — type is required so
// the gate has the input it needs for its advisory check.
func TestHandleDeleteObject_RejectsUntypedPath(t *testing.T) {
	s := &GateServer{
		logger: logger.NewLogger("test-gate"),
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/objects/delete/some-id", nil)
	w := httptest.NewRecorder()

	s.handleDeleteObject(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 Bad Request", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	var errResp HTTPErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("decode error response: %v\nbody=%s", err, string(respBody))
	}
	if errResp.Code != "INVALID_PATH" {
		t.Fatalf("error code = %q; want INVALID_PATH. body=%s", errResp.Code, string(respBody))
	}
}

// TestDeleteObject_gRPC_RejectsEmptyType pins that the gRPC handler
// rejects requests without a type field — counterpart to the HTTP
// route's INVALID_PATH rejection. The receiving node still does the
// authoritative check on real type; the gate just refuses to forward
// without the second arg its advisory check needs.
func TestDeleteObject_gRPC_RejectsEmptyType(t *testing.T) {
	s := &GateServer{
		config: &GateServerConfig{DefaultDeleteTimeout: 5 * time.Second},
		logger: logger.NewLogger("test-gate"),
	}

	_, err := s.DeleteObject(context.Background(), &gate_pb.DeleteObjectRequest{
		Id: "some-id",
	})
	if err == nil {
		t.Fatalf("DeleteObject succeeded with empty type; want InvalidArgument")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("status code = %v; want InvalidArgument", st.Code())
	}
}

// TestDeleteObject_gRPC_LifecycleRejected pins the gate's gRPC
// DeleteObject lifecycle check. Counterpart to the HTTP test above.
func TestDeleteObject_gRPC_LifecycleRejected(t *testing.T) {
	rules := []config.LifecycleRule{
		{Type: "Restricted", Lifecycle: "DELETE", Access: "INTERNAL"},
	}
	validator, err := config.NewLifecycleValidator(rules)
	if err != nil {
		t.Fatalf("NewLifecycleValidator: %v", err)
	}

	s := &GateServer{
		config:             &GateServerConfig{DefaultDeleteTimeout: 5 * time.Second},
		logger:             logger.NewLogger("test-gate"),
		lifecycleValidator: validator,
	}

	_, err = s.DeleteObject(context.Background(), &gate_pb.DeleteObjectRequest{
		Type: "Restricted",
		Id:   "some-id",
	})
	if err == nil {
		t.Fatalf("DeleteObject succeeded; want PermissionDenied")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a gRPC status: %v", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("status code = %v; want PermissionDenied", st.Code())
	}
}
