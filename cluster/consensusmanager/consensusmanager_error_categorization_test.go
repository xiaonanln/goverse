package consensusmanager

import (
	"context"
	"errors"
	"testing"
)

func TestCategorizeEtcdError_Timeout(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"DeadlineExceeded", context.DeadlineExceeded, "timeout"},
		{"Canceled", context.Canceled, "timeout"},
		{"deadline in message", errors.New("context deadline exceeded"), "timeout"},
		{"deadline exceeded", errors.New("deadline exceeded"), "timeout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categorizeEtcdError(tt.err)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCategorizeEtcdError_ConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"connection refused", errors.New("connection refused"), "connection_error"},
		{"connection reset", errors.New("connection reset by peer"), "connection_error"},
		{"broken pipe", errors.New("broken pipe"), "connection_error"},
		{"no such host", errors.New("no such host"), "connection_error"},
		{"network unreachable", errors.New("network is unreachable"), "connection_error"},
		{"etcd not connected", errors.New("etcd client not connected"), "connection_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categorizeEtcdError(tt.err)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCategorizeEtcdError_Other(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"nil error", nil, "other"},
		{"random error", errors.New("some random error"), "other"},
		{"permission denied", errors.New("permission denied"), "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categorizeEtcdError(tt.err)
			if result != tt.expected {
				t.Fatalf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
