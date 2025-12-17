package object

import (
	"context"
	"time"
)

// EvictionPolicy defines the policy for evicting objects from memory.
// Objects can implement this to opt-in to automatic eviction.
type EvictionPolicy interface {
	// ShouldEvict determines if an object should be evicted based on the policy.
	// The policy receives access time and creation time information to make the decision.
	//
	// Parameters:
	//   - lastAccessTime: The last time the object was accessed (zero if never accessed)
	//   - creationTime: The time the object was created
	//
	// Returns:
	//   - bool: true if the object should be evicted, false otherwise
	ShouldEvict(lastAccessTime time.Time, creationTime time.Time) bool

	// OnEvict is called before the object is evicted from memory.
	// This hook allows the object to perform cleanup operations.
	// If this method returns an error, the eviction is aborted.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeouts
	//   - obj: The object being evicted
	//
	// Returns:
	//   - error: nil to allow eviction, or an error to prevent it
	OnEvict(ctx context.Context, obj Object) error
}

// IdleTimeoutPolicy evicts objects that have not been accessed for a specified duration.
// This is useful for session objects or cached data that should be removed after inactivity.
type IdleTimeoutPolicy struct {
	// IdleTimeout is the duration of inactivity after which the object should be evicted.
	IdleTimeout time.Duration
}

// ShouldEvict returns true if the object has not been accessed for longer than IdleTimeout.
func (p *IdleTimeoutPolicy) ShouldEvict(lastAccessTime time.Time, creationTime time.Time) bool {
	// If never accessed, use creation time as reference
	referenceTime := lastAccessTime
	if referenceTime.IsZero() {
		referenceTime = creationTime
	}
	
	// If both times are zero, object is not tracked properly - don't evict
	if referenceTime.IsZero() {
		return false
	}
	
	// Check if idle timeout has elapsed
	idleDuration := time.Since(referenceTime)
	return idleDuration >= p.IdleTimeout
}

// OnEvict is a no-op for IdleTimeoutPolicy (no special cleanup needed).
func (p *IdleTimeoutPolicy) OnEvict(ctx context.Context, obj Object) error {
	return nil
}

// TTLPolicy evicts objects after a specified time-to-live from creation.
// This is useful for cache objects or temporary data that should expire after a fixed duration.
type TTLPolicy struct {
	// TTL is the time-to-live duration after which the object should be evicted.
	TTL time.Duration
}

// ShouldEvict returns true if the object has existed for longer than TTL.
func (p *TTLPolicy) ShouldEvict(lastAccessTime time.Time, creationTime time.Time) bool {
	// If creation time is zero, object is not tracked properly - don't evict
	if creationTime.IsZero() {
		return false
	}
	age := time.Since(creationTime)
	return age >= p.TTL
}

// OnEvict is a no-op for TTLPolicy (no special cleanup needed).
func (p *TTLPolicy) OnEvict(ctx context.Context, obj Object) error {
	return nil
}
