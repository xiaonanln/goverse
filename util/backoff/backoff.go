package backoff

import (
	"context"
	"time"
)

// Backoff implements exponential backoff with configurable parameters.
// It provides a simple way to retry operations with increasing delays.
type Backoff struct {
	initialDelay time.Duration
	maxDelay     time.Duration
	multiplier   float64
	currentDelay time.Duration
}

// New creates a new Backoff with the specified parameters.
// initialDelay is the delay before the first retry.
// maxDelay is the maximum delay between retries.
// multiplier is the factor by which the delay increases after each retry.
func New(initialDelay, maxDelay time.Duration, multiplier float64) *Backoff {
	return &Backoff{
		initialDelay: initialDelay,
		maxDelay:     maxDelay,
		multiplier:   multiplier,
		currentDelay: initialDelay,
	}
}

// Wait waits for the current backoff duration, respecting context cancellation.
// Returns nil if the wait completed successfully, or ctx.Err() if the context was cancelled.
// After a successful wait, the backoff duration is increased for the next call.
func (b *Backoff) Wait(ctx context.Context) error {
	select {
	case <-time.After(b.currentDelay):
		// Increase delay for next retry
		b.currentDelay = time.Duration(float64(b.currentDelay) * b.multiplier)
		if b.currentDelay > b.maxDelay {
			b.currentDelay = b.maxDelay
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reset resets the backoff to its initial delay.
// This is useful when starting a new retry sequence.
func (b *Backoff) Reset() {
	b.currentDelay = b.initialDelay
}

// CurrentDelay returns the current backoff delay.
func (b *Backoff) CurrentDelay() time.Duration {
	return b.currentDelay
}
