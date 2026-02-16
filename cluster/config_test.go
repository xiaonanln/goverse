package cluster

import (
	"testing"
	"time"
)

func TestDefaultConfigTimeouts(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DefaultCallTimeout != 30*time.Second {
		t.Fatalf("expected DefaultCallTimeout 30s, got %v", cfg.DefaultCallTimeout)
	}
	if cfg.DefaultCreateTimeout != 30*time.Second {
		t.Fatalf("expected DefaultCreateTimeout 30s, got %v", cfg.DefaultCreateTimeout)
	}
	if cfg.DefaultDeleteTimeout != 30*time.Second {
		t.Fatalf("expected DefaultDeleteTimeout 30s, got %v", cfg.DefaultDeleteTimeout)
	}
}

func TestEffectiveTimeout(t *testing.T) {
	fallback := 30 * time.Second

	tests := []struct {
		name       string
		configured time.Duration
		expected   time.Duration
	}{
		{"zero uses fallback", 0, fallback},
		{"negative uses fallback", -5 * time.Second, fallback},
		{"positive uses configured", 10 * time.Second, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveTimeout(tt.configured, fallback)
			if got != tt.expected {
				t.Fatalf("effectiveTimeout(%v, %v) = %v, want %v", tt.configured, fallback, got, tt.expected)
			}
		})
	}
}
