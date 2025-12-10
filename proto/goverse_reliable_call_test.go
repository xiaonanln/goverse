package goverse_pb

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestReliableCallObjectRequest_Serialization verifies that ReliableCallObjectRequest
// can be marshaled and unmarshaled correctly
func TestReliableCallObjectRequest_Serialization(t *testing.T) {
	original := &ReliableCallObjectRequest{
		CallId:     "test-call-123",
		ObjectType: "TestObject",
		ObjectId:   "test-obj-456",
	}

	// Marshal to bytes
	data, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal ReliableCallObjectRequest: %v", err)
	}

	// Unmarshal back
	decoded := &ReliableCallObjectRequest{}
	err = proto.Unmarshal(data, decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal ReliableCallObjectRequest: %v", err)
	}

	// Verify fields
	if decoded.CallId != original.CallId {
		t.Errorf("CallId mismatch: got %v, want %v", decoded.CallId, original.CallId)
	}
	if decoded.ObjectType != original.ObjectType {
		t.Errorf("ObjectType mismatch: got %v, want %v", decoded.ObjectType, original.ObjectType)
	}
	if decoded.ObjectId != original.ObjectId {
		t.Errorf("ObjectId mismatch: got %v, want %v", decoded.ObjectId, original.ObjectId)
	}
}

// TestReliableCallObjectResponse_Serialization verifies that ReliableCallObjectResponse
// can be marshaled and unmarshaled correctly
func TestReliableCallObjectResponse_Serialization(t *testing.T) {
	t.Run("Success response with result data", func(t *testing.T) {
		original := &ReliableCallObjectResponse{
			ResultData: []byte("test result data"),
			Error:      "",
		}

		// Marshal to bytes
		data, err := proto.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal ReliableCallObjectResponse: %v", err)
		}

		// Unmarshal back
		decoded := &ReliableCallObjectResponse{}
		err = proto.Unmarshal(data, decoded)
		if err != nil {
			t.Fatalf("Failed to unmarshal ReliableCallObjectResponse: %v", err)
		}

		// Verify fields
		if string(decoded.ResultData) != string(original.ResultData) {
			t.Errorf("ResultData mismatch: got %v, want %v", decoded.ResultData, original.ResultData)
		}
		if decoded.Error != original.Error {
			t.Errorf("Error mismatch: got %v, want %v", decoded.Error, original.Error)
		}
	})

	t.Run("Error response with error message", func(t *testing.T) {
		original := &ReliableCallObjectResponse{
			ResultData: nil,
			Error:      "test error message",
		}

		// Marshal to bytes
		data, err := proto.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal ReliableCallObjectResponse: %v", err)
		}

		// Unmarshal back
		decoded := &ReliableCallObjectResponse{}
		err = proto.Unmarshal(data, decoded)
		if err != nil {
			t.Fatalf("Failed to unmarshal ReliableCallObjectResponse: %v", err)
		}

		// Verify fields
		if len(decoded.ResultData) != 0 {
			t.Errorf("ResultData should be empty, got %v", decoded.ResultData)
		}
		if decoded.Error != original.Error {
			t.Errorf("Error mismatch: got %v, want %v", decoded.Error, original.Error)
		}
	})
}

// TestReliableCallObjectMessages_Fields verifies that all expected fields are present
func TestReliableCallObjectMessages_Fields(t *testing.T) {
	// Test that we can create and access all fields on ReliableCallObjectRequest
	req := &ReliableCallObjectRequest{
		CallId:     "call-id",
		ObjectType: "type",
		ObjectId:   "id",
	}

	if req.GetCallId() != "call-id" {
		t.Errorf("GetCallId() = %v, want %v", req.GetCallId(), "call-id")
	}
	if req.GetObjectType() != "type" {
		t.Errorf("GetObjectType() = %v, want %v", req.GetObjectType(), "type")
	}
	if req.GetObjectId() != "id" {
		t.Errorf("GetObjectId() = %v, want %v", req.GetObjectId(), "id")
	}

	// Test that we can create and access all fields on ReliableCallObjectResponse
	resp := &ReliableCallObjectResponse{
		ResultData: []byte("result"),
		Error:      "error",
	}

	if string(resp.GetResultData()) != "result" {
		t.Errorf("GetResultData() = %v, want %v", resp.GetResultData(), "result")
	}
	if resp.GetError() != "error" {
		t.Errorf("GetError() = %v, want %v", resp.GetError(), "error")
	}
}
