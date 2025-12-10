package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/gate"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/testutil"
)

// mockPersistenceProvider is a simple mock for testing ReliableCallObject
type mockPersistenceProvider struct {
	calls map[string]*object.ReliableCall
	seq   int64
	mu    sync.Mutex
}

func newMockPersistenceProvider() *mockPersistenceProvider {
	return &mockPersistenceProvider{
		calls: make(map[string]*object.ReliableCall),
		seq:   0,
	}
}

func (m *mockPersistenceProvider) SaveObject(ctx context.Context, objectID, objectType string, data []byte, nextRcseq int64) error {
	return nil
}

func (m *mockPersistenceProvider) LoadObject(ctx context.Context, objectID string) ([]byte, int64, error) {
	return nil, 0, fmt.Errorf("object not found: %s", objectID)
}

func (m *mockPersistenceProvider) DeleteObject(ctx context.Context, objectID string) error {
	return nil
}

func (m *mockPersistenceProvider) InsertOrGetReliableCall(ctx context.Context, requestID string, objectID string, objectType string, methodName string, requestData []byte) (*object.ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if call already exists
	if existing, ok := m.calls[requestID]; ok {
		return existing, nil
	}

	// Create new call
	m.seq++
	rc := &object.ReliableCall{
		Seq:         m.seq,
		CallID:      requestID,
		ObjectID:    objectID,
		ObjectType:  objectType,
		MethodName:  methodName,
		RequestData: requestData,
		Status:      "pending",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.calls[requestID] = rc
	return rc, nil
}

func (m *mockPersistenceProvider) UpdateReliableCallStatus(ctx context.Context, seq int64, status string, resultData []byte, errorMessage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find call by seq
	for _, rc := range m.calls {
		if rc.Seq == seq {
			rc.Status = status
			rc.ResultData = resultData
			rc.Error = errorMessage
			rc.UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("reliable call not found: %d", seq)
}

func (m *mockPersistenceProvider) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var pending []*object.ReliableCall
	for _, rc := range m.calls {
		if rc.ObjectID == objectID && rc.Seq > nextRcseq && rc.Status == "pending" {
			pending = append(pending, rc)
		}
	}
	return pending, nil
}

func (m *mockPersistenceProvider) GetReliableCall(ctx context.Context, requestID string) (*object.ReliableCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if rc, ok := m.calls[requestID]; ok {
		return rc, nil
	}
	return nil, fmt.Errorf("reliable call not found: %s", requestID)
}

func (m *mockPersistenceProvider) HasStoredData(callID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.calls[callID]
	return ok
}

func TestReliableCallObject_ValidatesInputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	// Set up a mock persistence provider
	mockProvider := newMockPersistenceProvider()
	n.SetPersistenceProvider(mockProvider)

	tests := []struct {
		name        string
		callID      string
		objectType  string
		objectID    string
		methodName  string
		requestData []byte
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty callID",
			callID:      "",
			objectType:  "TestType",
			objectID:    "test-obj-1",
			methodName:  "TestMethod",
			requestData: []byte("test"),
			wantErr:     true,
			errContains: "callID cannot be empty",
		},
		{
			name:        "empty objectType",
			callID:      "test-call-1",
			objectType:  "",
			objectID:    "test-obj-1",
			methodName:  "TestMethod",
			requestData: []byte("test"),
			wantErr:     true,
			errContains: "objectType cannot be empty",
		},
		{
			name:        "empty objectID",
			callID:      "test-call-1",
			objectType:  "TestType",
			objectID:    "",
			methodName:  "TestMethod",
			requestData: []byte("test"),
			wantErr:     true,
			errContains: "objectID cannot be empty",
		},
		{
			name:        "empty methodName",
			callID:      "test-call-1",
			objectType:  "TestType",
			objectID:    "test-obj-1",
			methodName:  "",
			requestData: []byte("test"),
			wantErr:     true,
			errContains: "methodName cannot be empty",
		},
		{
			name:        "valid inputs",
			callID:      "test-call-1",
			objectType:  "TestType",
			objectID:    "test-obj-1",
			methodName:  "TestMethod",
			requestData: []byte("test"),
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := cluster.ReliableCallObject(ctx, tt.callID, tt.objectType, tt.objectID, tt.methodName, tt.requestData)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				if rc != nil {
					t.Errorf("Expected nil result on error, got %v", rc)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if rc == nil {
					t.Error("Expected non-nil result, got nil")
				}
				if rc != nil && rc.Status != "pending" {
					t.Errorf("Expected status 'pending', got %q", rc.Status)
				}
			}
		})
	}
}

func TestReliableCallObject_NoPersistenceProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	// Don't set a persistence provider
	_, err := cluster.ReliableCallObject(ctx, "call-1", "TestType", "obj-1", "Method", []byte("test"))

	if err == nil {
		t.Fatal("Expected error when persistence provider is not configured")
	}
	if !contains(err.Error(), "persistence provider is not configured") {
		t.Errorf("Expected error about persistence provider, got %v", err)
	}
}

func TestReliableCallObject_GateCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	// Create a gate cluster (not a node cluster)
	gateConfig := &gate.GateConfig{
		AdvertiseAddress: "test-gate:7001",
		EtcdAddress:      "localhost:2379",
		EtcdPrefix:       "/test-gate",
	}
	g, err := gate.NewGate(gateConfig)
	if err != nil {
		t.Fatalf("Failed to create gate: %v", err)
	}
	cluster := newClusterForTesting(nil, "TestGateCluster")
	cluster.gate = g

	_, err = cluster.ReliableCallObject(ctx, "call-1", "TestType", "obj-1", "Method", []byte("test"))

	if err == nil {
		t.Fatal("Expected error when calling on gate cluster")
	}
	if !contains(err.Error(), "can only be called on node clusters") {
		t.Errorf("Expected error about node clusters only, got %v", err)
	}
}

func TestReliableCallObject_InsertsNewCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	mockProvider := newMockPersistenceProvider()
	n.SetPersistenceProvider(mockProvider)

	callID := "test-call-new"
	objectType := "TestType"
	objectID := "test-obj-1"
	methodName := "TestMethod"
	requestData := []byte("test request data")

	rc, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if rc == nil {
		t.Fatal("Expected non-nil result")
	}
	if rc.CallID != callID {
		t.Errorf("Expected call_id %q, got %q", callID, rc.CallID)
	}
	if rc.ObjectID != objectID {
		t.Errorf("Expected object_id %q, got %q", objectID, rc.ObjectID)
	}
	if rc.ObjectType != objectType {
		t.Errorf("Expected object_type %q, got %q", objectType, rc.ObjectType)
	}
	if rc.MethodName != methodName {
		t.Errorf("Expected method_name %q, got %q", methodName, rc.MethodName)
	}
	if string(rc.RequestData) != string(requestData) {
		t.Errorf("Expected request_data %q, got %q", requestData, rc.RequestData)
	}
	if rc.Status != "pending" {
		t.Errorf("Expected status 'pending', got %q", rc.Status)
	}
	if rc.Seq <= 0 {
		t.Errorf("Expected positive seq, got %d", rc.Seq)
	}

	// Verify the call was stored in mock provider
	if !mockProvider.HasStoredData(callID) {
		t.Error("Expected call to be stored in persistence provider")
	}
}

func TestReliableCallObject_DeduplicationPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	mockProvider := newMockPersistenceProvider()
	n.SetPersistenceProvider(mockProvider)

	callID := "test-call-dup"
	objectType := "TestType"
	objectID := "test-obj-1"
	methodName := "TestMethod"
	requestData := []byte("test request data")

	// Insert first call
	rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Insert duplicate call with same callID
	rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("Duplicate call failed: %v", err)
	}

	// Should return the same call (same seq)
	if rc2.Seq != rc1.Seq {
		t.Errorf("Expected same seq %d, got %d", rc1.Seq, rc2.Seq)
	}
	if rc2.CallID != rc1.CallID {
		t.Errorf("Expected same call_id %q, got %q", rc1.CallID, rc2.CallID)
	}
	if rc2.Status != "pending" {
		t.Errorf("Expected status 'pending', got %q", rc2.Status)
	}
}

func TestReliableCallObject_ReturnsCompletedCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	mockProvider := newMockPersistenceProvider()
	n.SetPersistenceProvider(mockProvider)

	callID := "test-call-completed"
	objectType := "TestType"
	objectID := "test-obj-1"
	methodName := "TestMethod"
	requestData := []byte("test request data")

	// Insert first call
	rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Simulate call completion by updating status
	resultData := []byte("test result")
	err = mockProvider.UpdateReliableCallStatus(ctx, rc1.Seq, "completed", resultData, "")
	if err != nil {
		t.Fatalf("Failed to update call status: %v", err)
	}

	// Try to insert duplicate call with same callID
	rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("Duplicate call failed: %v", err)
	}

	// Should return the completed call with cached result
	if rc2.Status != "completed" {
		t.Errorf("Expected status 'completed', got %q", rc2.Status)
	}
	if string(rc2.ResultData) != string(resultData) {
		t.Errorf("Expected result_data %q, got %q", resultData, rc2.ResultData)
	}
	if rc2.Seq != rc1.Seq {
		t.Errorf("Expected same seq %d, got %d", rc1.Seq, rc2.Seq)
	}
}

func TestReliableCallObject_ReturnsFailedCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	n := node.NewNode("test-node", testutil.TestNumShards)
	cluster := newClusterForTesting(n, "TestCluster")

	mockProvider := newMockPersistenceProvider()
	n.SetPersistenceProvider(mockProvider)

	callID := "test-call-failed"
	objectType := "TestType"
	objectID := "test-obj-1"
	methodName := "TestMethod"
	requestData := []byte("test request data")

	// Insert first call
	rc1, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("First call failed: %v", err)
	}

	// Simulate call failure by updating status
	errorMessage := "test error message"
	err = mockProvider.UpdateReliableCallStatus(ctx, rc1.Seq, "failed", nil, errorMessage)
	if err != nil {
		t.Fatalf("Failed to update call status: %v", err)
	}

	// Try to insert duplicate call with same callID
	rc2, err := cluster.ReliableCallObject(ctx, callID, objectType, objectID, methodName, requestData)
	if err != nil {
		t.Fatalf("Duplicate call failed: %v", err)
	}

	// Should return the failed call with cached error
	if rc2.Status != "failed" {
		t.Errorf("Expected status 'failed', got %q", rc2.Status)
	}
	if rc2.Error != errorMessage {
		t.Errorf("Expected error %q, got %q", errorMessage, rc2.Error)
	}
	if rc2.Seq != rc1.Seq {
		t.Errorf("Expected same seq %d, got %d", rc1.Seq, rc2.Seq)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || 
		(len(s) > 0 && len(substr) > 0 && indexOfSubstring(s, substr) >= 0))
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
