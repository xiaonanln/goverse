package backoff

import (
	"context"
	"testing"
	"time"
)

func TestBackoff_Wait(t *testing.T) {
	t.Run("exponential growth", func(t *testing.T) {
		b := New(100*time.Millisecond, 1*time.Second, 2.0)

		// First wait should use initial delay
		if b.CurrentDelay() != 100*time.Millisecond {
			t.Errorf("Expected initial delay 100ms, got %v", b.CurrentDelay())
		}

		ctx := context.Background()
		start := time.Now()
		err := b.Wait(ctx)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}

		// Should have waited approximately 100ms
		if elapsed < 90*time.Millisecond || elapsed > 200*time.Millisecond {
			t.Errorf("Expected wait around 100ms, got %v", elapsed)
		}

		// Second wait should double to 200ms
		if b.CurrentDelay() != 200*time.Millisecond {
			t.Errorf("Expected delay 200ms after first wait, got %v", b.CurrentDelay())
		}

		start = time.Now()
		err = b.Wait(ctx)
		elapsed = time.Since(start)

		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}

		// Should have waited approximately 200ms
		if elapsed < 180*time.Millisecond || elapsed > 300*time.Millisecond {
			t.Errorf("Expected wait around 200ms, got %v", elapsed)
		}

		// Third wait should double to 400ms
		if b.CurrentDelay() != 400*time.Millisecond {
			t.Errorf("Expected delay 400ms after second wait, got %v", b.CurrentDelay())
		}
	})

	t.Run("max delay capping", func(t *testing.T) {
		b := New(500*time.Millisecond, 800*time.Millisecond, 2.0)

		ctx := context.Background()

		// First wait: 500ms
		err := b.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}

		// After first wait, should be at max (1000ms would exceed 800ms max)
		// 500ms * 2 = 1000ms, but capped at 800ms
		if b.CurrentDelay() > 800*time.Millisecond {
			t.Errorf("Expected delay capped at 800ms, got %v", b.CurrentDelay())
		}

		// Second wait should also be capped
		err = b.Wait(ctx)
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}

		if b.CurrentDelay() != 800*time.Millisecond {
			t.Errorf("Expected delay to remain at max 800ms, got %v", b.CurrentDelay())
		}
	})

	t.Run("context cancellation during wait", func(t *testing.T) {
		b := New(1*time.Second, 10*time.Second, 2.0)

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel context after 100ms
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		err := b.Wait(ctx)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("Expected error from cancelled context")
		}

		if err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}

		// Should have stopped early (around 100ms instead of 1s)
		if elapsed > 500*time.Millisecond {
			t.Errorf("Expected early cancellation around 100ms, got %v", elapsed)
		}
	})

	t.Run("context deadline exceeded", func(t *testing.T) {
		b := New(1*time.Second, 10*time.Second, 2.0)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := b.Wait(ctx)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("Expected error from deadline exceeded")
		}

		if err != context.DeadlineExceeded {
			t.Errorf("Expected context.DeadlineExceeded error, got %v", err)
		}

		// Should have stopped early
		if elapsed > 500*time.Millisecond {
			t.Errorf("Expected early termination around 100ms, got %v", elapsed)
		}
	})
}

func TestBackoff_Reset(t *testing.T) {
	b := New(100*time.Millisecond, 1*time.Second, 2.0)

	ctx := context.Background()

	// Do a few waits to increase the delay
	b.Wait(ctx) // 100ms -> 200ms
	b.Wait(ctx) // 200ms -> 400ms

	if b.CurrentDelay() != 400*time.Millisecond {
		t.Errorf("Expected delay 400ms before reset, got %v", b.CurrentDelay())
	}

	// Reset should bring it back to initial delay
	b.Reset()

	if b.CurrentDelay() != 100*time.Millisecond {
		t.Errorf("Expected delay 100ms after reset, got %v", b.CurrentDelay())
	}
}

func TestBackoff_CustomMultiplier(t *testing.T) {
	// Test with different multiplier (3x instead of 2x)
	b := New(100*time.Millisecond, 5*time.Second, 3.0)

	ctx := context.Background()

	// First delay: 100ms
	if b.CurrentDelay() != 100*time.Millisecond {
		t.Errorf("Expected initial delay 100ms, got %v", b.CurrentDelay())
	}

	// After first wait: 100ms * 3 = 300ms
	b.Wait(ctx)
	if b.CurrentDelay() != 300*time.Millisecond {
		t.Errorf("Expected delay 300ms after first wait, got %v", b.CurrentDelay())
	}

	// After second wait: 300ms * 3 = 900ms
	b.Wait(ctx)
	if b.CurrentDelay() != 900*time.Millisecond {
		t.Errorf("Expected delay 900ms after second wait, got %v", b.CurrentDelay())
	}
}

func TestBackoff_CurrentDelay(t *testing.T) {
	b := New(250*time.Millisecond, 2*time.Second, 2.0)

	// Check initial delay
	if b.CurrentDelay() != 250*time.Millisecond {
		t.Errorf("Expected CurrentDelay to return 250ms initially, got %v", b.CurrentDelay())
	}

	ctx := context.Background()

	// After wait, delay should have doubled
	b.Wait(ctx)
	if b.CurrentDelay() != 500*time.Millisecond {
		t.Errorf("Expected CurrentDelay to return 500ms after first wait, got %v", b.CurrentDelay())
	}
}
