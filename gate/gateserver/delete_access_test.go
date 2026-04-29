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

// TestHandleDeleteObject_LegacyUntypedSkipsCheck confirms that the
// legacy POST /api/v1/objects/delete/{id} path keeps working in v0.2:
// the lifecycle check is skipped (with a warning) instead of
// rejecting, so v0.1 clients keep deleting until they upgrade to the
// typed route. v0.3 will tighten this.
//
// Without a real cluster wired in, the call panics inside
// s.cluster.DeleteObject — which is exactly the proof we need that
// the untyped path bypassed the access check and got through to the
// cluster path. recover() catches the panic so the test asserts the
// validator's behaviour, not the cluster's.
func TestHandleDeleteObject_LegacyUntypedSkipsCheck(t *testing.T) {
	rules := []config.LifecycleRule{
		// Even a default-deny rule must NOT block the legacy untyped
		// path — empty type warns and skips, by design.
		{Type: "Restricted", Lifecycle: "DELETE", Access: "REJECT"},
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
		"/api/v1/objects/delete/some-id", nil)
	w := httptest.NewRecorder()

	defer func() {
		_ = recover()
		if w.Code == http.StatusForbidden {
			t.Fatalf("legacy untyped delete unexpectedly produced 403; should warn-and-skip the lifecycle check")
		}
	}()
	s.handleDeleteObject(w, req)
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
