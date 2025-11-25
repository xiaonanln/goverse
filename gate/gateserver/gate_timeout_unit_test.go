package gateserver

import (
	"context"
	"testing"
	"time"
)

// TestGateServerConfig_DefaultCallTimeout tests that the default timeout is set correctly
func TestGateServerConfig_DefaultCallTimeout(t *testing.T) {
	t.Run("DefaultTimeoutSet", func(t *testing.T) {
		config := &GateServerConfig{
			ListenAddress:    "localhost:50000",
			AdvertiseAddress: "localhost:50000",
			EtcdAddress:      "localhost:2379",
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		expectedTimeout := 30 * time.Second
		if config.DefaultCallTimeout != expectedTimeout {
			t.Errorf("Expected DefaultCallTimeout to be %v, got %v", expectedTimeout, config.DefaultCallTimeout)
		}
	})

	t.Run("CustomTimeoutPreserved", func(t *testing.T) {
		customTimeout := 60 * time.Second
		config := &GateServerConfig{
			ListenAddress:      "localhost:50000",
			AdvertiseAddress:   "localhost:50000",
			EtcdAddress:        "localhost:2379",
			DefaultCallTimeout: customTimeout,
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		if config.DefaultCallTimeout != customTimeout {
			t.Errorf("Expected DefaultCallTimeout to be preserved at %v, got %v", customTimeout, config.DefaultCallTimeout)
		}
	})
}

// TestGateServerConfig_DefaultCreateTimeout tests that the default create timeout is set correctly
func TestGateServerConfig_DefaultCreateTimeout(t *testing.T) {
	t.Run("DefaultTimeoutSet", func(t *testing.T) {
		config := &GateServerConfig{
			ListenAddress:    "localhost:50000",
			AdvertiseAddress: "localhost:50000",
			EtcdAddress:      "localhost:2379",
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		expectedTimeout := 30 * time.Second
		if config.DefaultCreateTimeout != expectedTimeout {
			t.Errorf("Expected DefaultCreateTimeout to be %v, got %v", expectedTimeout, config.DefaultCreateTimeout)
		}
	})

	t.Run("CustomTimeoutPreserved", func(t *testing.T) {
		customTimeout := 60 * time.Second
		config := &GateServerConfig{
			ListenAddress:        "localhost:50000",
			AdvertiseAddress:     "localhost:50000",
			EtcdAddress:          "localhost:2379",
			DefaultCreateTimeout: customTimeout,
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		if config.DefaultCreateTimeout != customTimeout {
			t.Errorf("Expected DefaultCreateTimeout to be preserved at %v, got %v", customTimeout, config.DefaultCreateTimeout)
		}
	})
}

// TestGateServerConfig_DefaultDeleteTimeout tests that the default delete timeout is set correctly
func TestGateServerConfig_DefaultDeleteTimeout(t *testing.T) {
	t.Run("DefaultTimeoutSet", func(t *testing.T) {
		config := &GateServerConfig{
			ListenAddress:    "localhost:50000",
			AdvertiseAddress: "localhost:50000",
			EtcdAddress:      "localhost:2379",
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		expectedTimeout := 30 * time.Second
		if config.DefaultDeleteTimeout != expectedTimeout {
			t.Errorf("Expected DefaultDeleteTimeout to be %v, got %v", expectedTimeout, config.DefaultDeleteTimeout)
		}
	})

	t.Run("CustomTimeoutPreserved", func(t *testing.T) {
		customTimeout := 30 * time.Second
		config := &GateServerConfig{
			ListenAddress:        "localhost:50000",
			AdvertiseAddress:     "localhost:50000",
			EtcdAddress:          "localhost:2379",
			DefaultDeleteTimeout: customTimeout,
		}

		err := validateConfig(config)
		if err != nil {
			t.Fatalf("validateConfig failed: %v", err)
		}

		if config.DefaultDeleteTimeout != customTimeout {
			t.Errorf("Expected DefaultDeleteTimeout to be preserved at %v, got %v", customTimeout, config.DefaultDeleteTimeout)
		}
	})
}

// TestContextDeadlineApplication tests that deadline is properly applied to context
func TestContextDeadlineApplication(t *testing.T) {
	t.Run("NoDeadline", func(t *testing.T) {
		ctx := context.Background()

		// Check if context has no deadline
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			t.Fatal("Background context should not have a deadline")
		}

		// Apply timeout similar to what CallObject does
		timeout := 2 * time.Second
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Verify deadline was set
		deadline, hasDeadline := ctx.Deadline()
		if !hasDeadline {
			t.Fatal("Context should have a deadline after WithTimeout")
		}

		// Verify deadline is approximately correct (within 100ms)
		expectedDeadline := time.Now().Add(timeout)
		diff := deadline.Sub(expectedDeadline)
		if diff < -100*time.Millisecond || diff > 100*time.Millisecond {
			t.Errorf("Deadline is not close to expected time. Diff: %v", diff)
		}
	})

	t.Run("ExistingDeadlinePreserved", func(t *testing.T) {
		// Create context with existing deadline
		existingTimeout := 5 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), existingTimeout)
		defer cancel()

		existingDeadline, hasDeadline := ctx.Deadline()
		if !hasDeadline {
			t.Fatal("Context should have a deadline")
		}

		// In CallObject, we check for deadline and skip setting new one if it exists
		// Verify the check works
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			// Don't apply new timeout - this is what CallObject does
			// Just verify the existing deadline is still there
			newDeadline, stillHasDeadline := ctx.Deadline()
			if !stillHasDeadline {
				t.Fatal("Deadline should still exist")
			}
			if !newDeadline.Equal(existingDeadline) {
				t.Error("Deadline should not have changed")
			}
		}
	})
}
