package errors

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestTimeoutErrorMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      *TimeoutError
		expected string
	}{
		{
			name:     "with object ID",
			err:      NewTimeoutError("CallObject", "obj-123", context.DeadlineExceeded),
			expected: "timeout: CallObject for object obj-123: context deadline exceeded",
		},
		{
			name:     "without object ID",
			err:      NewTimeoutError("CallObject", "", context.DeadlineExceeded),
			expected: "timeout: CallObject: context deadline exceeded",
		},
		{
			name:     "CreateObject operation",
			err:      NewTimeoutError("CreateObject", "my-obj", context.DeadlineExceeded),
			expected: "timeout: CreateObject for object my-obj: context deadline exceeded",
		},
		{
			name:     "DeleteObject operation",
			err:      NewTimeoutError("DeleteObject", "del-obj", context.DeadlineExceeded),
			expected: "timeout: DeleteObject for object del-obj: context deadline exceeded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.expected {
				t.Fatalf("Expected %q, got %q", tc.expected, tc.err.Error())
			}
		})
	}
}

func TestTimeoutErrorUnwrap(t *testing.T) {
	t.Parallel()
	inner := context.DeadlineExceeded
	te := NewTimeoutError("CallObject", "obj-1", inner)

	if !errors.Is(te, context.DeadlineExceeded) {
		t.Fatalf("Expected TimeoutError to unwrap to context.DeadlineExceeded")
	}
}

func TestIsTimeout_Nil(t *testing.T) {
	t.Parallel()
	if IsTimeout(nil) {
		t.Fatalf("Expected IsTimeout(nil) to be false")
	}
}

func TestIsTimeout_DeadlineExceeded(t *testing.T) {
	t.Parallel()
	if !IsTimeout(context.DeadlineExceeded) {
		t.Fatalf("Expected IsTimeout(context.DeadlineExceeded) to be true")
	}
}

func TestIsTimeout_TimeoutError(t *testing.T) {
	t.Parallel()
	te := NewTimeoutError("CallObject", "obj-1", context.DeadlineExceeded)
	if !IsTimeout(te) {
		t.Fatalf("Expected IsTimeout(TimeoutError) to be true")
	}
}

func TestIsTimeout_WrappedDeadlineExceeded(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("rpc failed: %w", context.DeadlineExceeded)
	if !IsTimeout(wrapped) {
		t.Fatalf("Expected IsTimeout on wrapped DeadlineExceeded to be true")
	}
}

func TestIsTimeout_WrappedTimeoutError(t *testing.T) {
	t.Parallel()
	te := NewTimeoutError("CallObject", "obj-1", context.DeadlineExceeded)
	wrapped := fmt.Errorf("outer: %w", te)
	if !IsTimeout(wrapped) {
		t.Fatalf("Expected IsTimeout on wrapped TimeoutError to be true")
	}
}

func TestIsTimeout_RegularError(t *testing.T) {
	t.Parallel()
	if IsTimeout(fmt.Errorf("some other error")) {
		t.Fatalf("Expected IsTimeout on regular error to be false")
	}
}

func TestIsTimeout_ContextCanceled(t *testing.T) {
	t.Parallel()
	if IsTimeout(context.Canceled) {
		t.Fatalf("Expected IsTimeout(context.Canceled) to be false")
	}
}

func TestNewTimeoutError(t *testing.T) {
	t.Parallel()
	te := NewTimeoutError("CreateObject", "my-obj", context.DeadlineExceeded)

	if te.Operation != "CreateObject" {
		t.Fatalf("Expected Operation to be CreateObject, got %s", te.Operation)
	}
	if te.ObjectID != "my-obj" {
		t.Fatalf("Expected ObjectID to be my-obj, got %s", te.ObjectID)
	}
	if te.Err != context.DeadlineExceeded {
		t.Fatalf("Expected Err to be context.DeadlineExceeded, got %v", te.Err)
	}
}
