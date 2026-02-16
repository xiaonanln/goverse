package cluster

import (
	"context"
	"testing"
	"time"
)

func TestDefaultTimeoutConstants(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"DefaultCallTimeout", DefaultCallTimeout},
		{"DefaultCreateTimeout", DefaultCreateTimeout},
		{"DefaultDeleteTimeout", DefaultDeleteTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout != 30*time.Second {
				t.Fatalf("expected %s to be 30s, got %v", tt.name, tt.timeout)
			}
		})
	}
}

func TestDefaultTimeoutCapsLongerDeadline(t *testing.T) {
	// When the parent has a longer deadline, context.WithTimeout caps it
	// to the shorter timeout value.
	parent, parentCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer parentCancel()

	child, childCancel := context.WithTimeout(parent, DefaultCallTimeout)
	defer childCancel()

	deadline, ok := child.Deadline()
	if !ok {
		t.Fatal("child context should have a deadline")
	}

	remaining := time.Until(deadline)
	if remaining > DefaultCallTimeout+time.Second {
		t.Fatalf("expected deadline ≤ ~30s from now, got %v remaining", remaining)
	}
}

func TestDefaultTimeoutPreservesShorterDeadline(t *testing.T) {
	// When the parent already has a shorter deadline, WithTimeout keeps it.
	shortTimeout := 2 * time.Second
	parent, parentCancel := context.WithTimeout(context.Background(), shortTimeout)
	defer parentCancel()

	parentDeadline, _ := parent.Deadline()

	child, childCancel := context.WithTimeout(parent, DefaultCallTimeout)
	defer childCancel()

	childDeadline, ok := child.Deadline()
	if !ok {
		t.Fatal("child context should have a deadline")
	}

	if !childDeadline.Equal(parentDeadline) {
		t.Fatalf("expected child deadline to match parent's shorter deadline; parent=%v child=%v",
			parentDeadline, childDeadline)
	}
}
