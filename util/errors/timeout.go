package errors

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TimeoutError represents a timeout during an operation on an object.
type TimeoutError struct {
	Operation string
	ObjectID  string
	Err       error
}

// Error returns a human-readable error message.
func (e *TimeoutError) Error() string {
	if e.ObjectID != "" {
		return fmt.Sprintf("timeout: %s on %s: %v", e.Operation, e.ObjectID, e.Err)
	}
	return fmt.Sprintf("timeout: %s: %v", e.Operation, e.Err)
}

// Unwrap returns the underlying error.
func (e *TimeoutError) Unwrap() error {
	return e.Err
}

// NewTimeoutError creates a new TimeoutError.
func NewTimeoutError(operation, objectID string, err error) *TimeoutError {
	return &TimeoutError{
		Operation: operation,
		ObjectID:  objectID,
		Err:       err,
	}
}

// IsTimeout reports whether err is a timeout error. It checks for TimeoutError,
// context.DeadlineExceeded, and gRPC DeadlineExceeded status codes.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}

	var te *TimeoutError
	if errors.As(err, &te) {
		return true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	if s, ok := status.FromError(err); ok {
		return s.Code() == codes.DeadlineExceeded
	}

	return false
}
