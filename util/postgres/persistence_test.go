package postgres

import (
	"context"
	"testing"
)

func TestSaveObject_Validation(t *testing.T) {
	// Create a DB instance with nil connection for validation testing
	db := &DB{}

	tests := []struct {
		name       string
		objectID   string
		objectType string
		wantErr    bool
	}{
		{
			name:       "empty object_id",
			objectID:   "",
			objectType: "TestType",
			wantErr:    true,
		},
		{
			name:       "empty object_type",
			objectID:   "test-123",
			objectType: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.SaveObject(context.Background(), tt.objectID, tt.objectType, []byte(`{"key":"value"}`), 0)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SaveObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadObject_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		_, _, err := db.LoadObject(context.Background(), "")
		if err == nil {
			t.Fatal("LoadObject() should return error for empty object_id")
		}
	})
}

func TestDeleteObject_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		err := db.DeleteObject(context.Background(), "")
		if err == nil {
			t.Fatal("DeleteObject() should return error for empty object_id")
		}
	})
}

func TestObjectExists_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		_, err := db.ObjectExists(context.Background(), "")
		if err == nil {
			t.Fatal("ObjectExists() should return error for empty object_id")
		}
	})
}

func TestListObjectsByType_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_type", func(t *testing.T) {
		_, err := db.ListObjectsByType(context.Background(), "")
		if err == nil {
			t.Fatal("ListObjectsByType() should return error for empty object_type")
		}
	})
}

func TestInsertOrGetReliableCall_Validation(t *testing.T) {
	db := &DB{}

	tests := []struct {
		name        string
		requestID   string
		objectID    string
		objectType  string
		methodName  string
		requestData []byte
		wantErr     bool
	}{
		{
			name:        "empty call_id",
			requestID:   "",
			objectID:    "obj-123",
			objectType:  "TestType",
			methodName:  "TestMethod",
			requestData: []byte("data"),
			wantErr:     true,
		},
		{
			name:        "empty object_id",
			requestID:   "req-123",
			objectID:    "",
			objectType:  "TestType",
			methodName:  "TestMethod",
			requestData: []byte("data"),
			wantErr:     true,
		},
		{
			name:        "empty object_type",
			requestID:   "req-123",
			objectID:    "obj-123",
			objectType:  "",
			methodName:  "TestMethod",
			requestData: []byte("data"),
			wantErr:     true,
		},
		{
			name:        "empty method_name",
			requestID:   "req-123",
			objectID:    "obj-123",
			objectType:  "TestType",
			methodName:  "",
			requestData: []byte("data"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.InsertOrGetReliableCall(context.Background(), tt.requestID, tt.objectID, tt.objectType, tt.methodName, tt.requestData)
			if (err != nil) != tt.wantErr {
				t.Fatalf("InsertOrGetReliableCall() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateReliableCallStatus_Validation(t *testing.T) {
	db := &DB{}

	tests := []struct {
		name         string
		seq          int64
		status       string
		resultData   []byte
		errorMessage string
		wantErr      bool
	}{
		{
			name:         "zero seq",
			seq:          0,
			status:       "completed",
			resultData:   []byte("result"),
			errorMessage: "",
			wantErr:      true,
		},
		{
			name:         "negative seq",
			seq:          -1,
			status:       "completed",
			resultData:   []byte("result"),
			errorMessage: "",
			wantErr:      true,
		},
		{
			name:         "empty status",
			seq:          1,
			status:       "",
			resultData:   []byte("result"),
			errorMessage: "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.UpdateReliableCallStatus(context.Background(), tt.seq, tt.status, tt.resultData, tt.errorMessage)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UpdateReliableCallStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetPendingReliableCalls_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		_, err := db.GetPendingReliableCalls(context.Background(), "", 0)
		if err == nil {
			t.Fatal("GetPendingReliableCalls() should return error for empty object_id")
		}
	})
}

func TestGetReliableCall_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty call_id", func(t *testing.T) {
		_, err := db.GetReliableCall(context.Background(), "")
		if err == nil {
			t.Fatal("GetReliableCall() should return error for empty call_id")
		}
	})
}

// Note: Integration tests that require actual PostgreSQL database
// would be placed in a separate _integration_test.go file
// and would test actual save/load/delete operations
