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
			err := db.SaveObject(context.Background(), tt.objectID, tt.objectType, []byte(`{"key":"value"}`))
			if (err != nil) != tt.wantErr {
				t.Errorf("SaveObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadObject_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		_, err := db.LoadObject(context.Background(), "")
		if err == nil {
			t.Error("LoadObject() should return error for empty object_id")
		}
	})
}

func TestDeleteObject_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		err := db.DeleteObject(context.Background(), "")
		if err == nil {
			t.Error("DeleteObject() should return error for empty object_id")
		}
	})
}

func TestObjectExists_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_id", func(t *testing.T) {
		_, err := db.ObjectExists(context.Background(), "")
		if err == nil {
			t.Error("ObjectExists() should return error for empty object_id")
		}
	})
}

func TestListObjectsByType_Validation(t *testing.T) {
	db := &DB{}

	t.Run("empty object_type", func(t *testing.T) {
		_, err := db.ListObjectsByType(context.Background(), "")
		if err == nil {
			t.Error("ListObjectsByType() should return error for empty object_type")
		}
	})
}

// Note: Integration tests that require actual PostgreSQL database
// would be placed in a separate _integration_test.go file
// and would test actual save/load/delete operations
