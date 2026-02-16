package errors

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorMessage_WithObjectID(t *testing.T) {
	err := NewTimeoutError("CallObject", "Player-123", context.DeadlineExceeded)
	expected := "timeout: CallObject on Player-123: context deadline exceeded"
	if err.Error() != expected {
		t.Fatalf("got %q, want %q", err.Error(), expected)
	}
}

func TestErrorMessage_WithoutObjectID(t *testing.T) {
	err := NewTimeoutError("Connect", "", context.DeadlineExceeded)
	expected := "timeout: Connect: context deadline exceeded"
	if err.Error() != expected {
		t.Fatalf("got %q, want %q", err.Error(), expected)
	}
}

func TestUnwrap(t *testing.T) {
	inner := context.DeadlineExceeded
	err := NewTimeoutError("op", "id", inner)
	if err.Unwrap() != inner {
		t.Fatalf("Unwrap returned wrong error")
	}
}

func TestNewTimeoutError_Fields(t *testing.T) {
	err := NewTimeoutError("Call", "obj-1", context.Canceled)
	if err.Operation != "Call" {
		t.Fatalf("Operation = %q", err.Operation)
	}
	if err.ObjectID != "obj-1" {
		t.Fatalf("ObjectID = %q", err.ObjectID)
	}
	if err.Err != context.Canceled {
		t.Fatalf("Err = %v", err.Err)
	}
}

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"DeadlineExceeded", context.DeadlineExceeded, true},
		{"TimeoutError", NewTimeoutError("op", "id", fmt.Errorf("x")), true},
		{"wrapped DeadlineExceeded", fmt.Errorf("wrap: %w", context.DeadlineExceeded), true},
		{"wrapped TimeoutError", fmt.Errorf("wrap: %w", NewTimeoutError("op", "id", fmt.Errorf("x"))), true},
		{"gRPC DeadlineExceeded", status.Error(codes.DeadlineExceeded, "timeout"), true},
		{"wrapped gRPC DeadlineExceeded", fmt.Errorf("wrap: %w", status.Error(codes.DeadlineExceeded, "timeout")), true},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "unavailable"), false},
		{"regular error", fmt.Errorf("some error"), false},
		{"context.Canceled", context.Canceled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTimeout(tt.err); got != tt.want {
				t.Fatalf("IsTimeout(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
