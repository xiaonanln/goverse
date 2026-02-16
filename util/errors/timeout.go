package errors

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TimeoutError represents an operation that failed due to a timeout.
type TimeoutError struct {
	// Operation is the type of operation that timed out (e.g., "CallObject", "CreateObject").
	Operation string
	// ObjectID is the ID of the object involved, if applicable.
	ObjectID string
	// Err is the underlying error (typically context.DeadlineExceeded).
	Err error
}

func (e *TimeoutError) Error() string {
	if e.ObjectID != "" {
		return fmt.Sprintf("timeout: %s for object %s: %v", e.Operation, e.ObjectID, e.Err)
	}
	return fmt.Sprintf("timeout: %s: %v", e.Operation, e.Err)
}

func (e *TimeoutError) Unwrap() error {
	return e.Err
}

// IsTimeout returns true if the error is a timeout error (either a TimeoutError
// or a context.DeadlineExceeded).
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
	// gRPC wraps deadline exceeded as a status error that doesn't satisfy
	// errors.Is(context.DeadlineExceeded), so check the gRPC status code.
	if s, ok := status.FromError(err); ok && s.Code() == codes.DeadlineExceeded {
		return true
	}
	return false
}

// NewTimeoutError creates a new TimeoutError for the given operation and object.
func NewTimeoutError(operation, objectID string, err error) *TimeoutError {
	return &TimeoutError{
		Operation: operation,
		ObjectID:  objectID,
		Err:       err,
	}
}
