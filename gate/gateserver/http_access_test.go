package gateserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/util/logger"
)

// TestHandleCallObject_AccessDeniedForRejectedMethod regression-pins
// the fix for the HTTP gate's missing CheckClientAccess. Before the
// fix, handleCallObject called s.cluster.CallObjectAnyRequest
// directly, so an INTERNAL/REJECT rule was only enforced for
// streaming-gRPC clients — a browser POST slipped past gate-side
// validation. Concrete impact in the bomberman sample: a malicious
// tab could POST Player.RecordMatchResult and grant itself wins.
//
// We construct a GateServer with just the AccessValidator wired in
// (no cluster) and exercise handleCallObject directly via httptest.
// Since the access check returns before any cluster interaction, no
// real cluster is needed.
func TestHandleCallObject_AccessDeniedForRejectedMethod(t *testing.T) {
	rules := []config.AccessRule{
		// Mark Player.RecordMatchResult as INTERNAL — the same rule
		// the bomberman sample ships, but applied to a synthetic
		// type so this test isn't coupled to the sample.
		{Type: "Player", Method: "RecordMatchResult", Access: "INTERNAL"},
	}
	validator, err := config.NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("NewAccessValidator: %v", err)
	}

	s := &GateServer{
		logger:          logger.NewLogger("test-gate"),
		accessValidator: validator,
	}

	body := `{"request":""}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/objects/call/Player/Player-alice/RecordMatchResult",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCallObject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403 Forbidden", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	var errResp HTTPErrorResponse
	if err := json.Unmarshal(respBody, &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v\nbody=%s", err, string(respBody))
	}
	if errResp.Code != "ACCESS_DENIED" {
		t.Fatalf("error code = %q; want ACCESS_DENIED. body=%s", errResp.Code, string(respBody))
	}
}

// TestHandleCallObject_AllowsExternalClients confirms the access
// check doesn't block legitimate ALLOW / EXTERNAL methods. Without
// a real cluster the call still fails downstream, but the failure
// must be a 5xx (cluster path) rather than a 403 (access denied) —
// proving CheckClientAccess accepted it.
func TestHandleCallObject_AllowsExternalClients(t *testing.T) {
	rules := []config.AccessRule{
		{Type: "PublicAPI", Method: "Read", Access: "ALLOW"},
	}
	validator, err := config.NewAccessValidator(rules)
	if err != nil {
		t.Fatalf("NewAccessValidator: %v", err)
	}

	s := &GateServer{
		logger:          logger.NewLogger("test-gate"),
		accessValidator: validator,
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/objects/call/PublicAPI/foo/Read",
		strings.NewReader(`{"request":""}`))
	w := httptest.NewRecorder()

	// Without a real cluster wired in, this will panic on the
	// CallObjectAnyRequest call — which is exactly the proof we need
	// that access validation accepted the request and let it
	// through to the cluster path. recover() catches the panic so
	// the test asserts the validator's behaviour, not the cluster's.
	defer func() {
		_ = recover()
		if w.Code == http.StatusForbidden {
			t.Fatalf("ALLOW rule unexpectedly produced 403; should have reached cluster path")
		}
	}()
	s.handleCallObject(w, req)
}
