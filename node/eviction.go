package node

import (
	"context"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/util/logger"
)

// EvictionManager manages automatic eviction of objects based on their eviction policies.
// It runs a background loop that checks objects every minute and evicts them if their
// policy indicates they should be evicted and it's safe to do so.
type EvictionManager struct {
	node             *Node
	checkInterval    time.Duration
	lastAccessTime   map[string]time.Time
	creationTime     map[string]time.Time
	mu               sync.RWMutex // protects lastAccessTime and creationTime maps
	stopCh           chan struct{}
	ticker           *time.Ticker
	logger           *logger.Logger
	done             chan struct{} // signals when the eviction loop has stopped
}

// newEvictionManager creates a new eviction manager for the given node.
func newEvictionManager(node *Node) *EvictionManager {
	return &EvictionManager{
		node:           node,
		checkInterval:  1 * time.Minute, // Fixed 1-minute interval
		lastAccessTime: make(map[string]time.Time),
		creationTime:   make(map[string]time.Time),
		stopCh:         make(chan struct{}),
		logger:         logger.NewLogger("EvictionManager"),
		done:           make(chan struct{}),
	}
}

// start begins the eviction manager's background loop.
func (em *EvictionManager) start() {
	em.logger.Infof("Starting eviction manager with %v check interval", em.checkInterval)
	em.ticker = time.NewTicker(em.checkInterval)
	go em.run()
}

// stop stops the eviction manager's background loop and waits for it to finish.
// This method is idempotent - multiple calls are safe.
func (em *EvictionManager) stop() {
	em.logger.Infof("Stopping eviction manager")
	
	// Close stopCh only once - use select to check if already closed
	select {
	case <-em.stopCh:
		// Already stopped, just wait for done
		em.logger.Infof("Eviction manager already stopped, waiting for cleanup")
	default:
		// Not stopped yet, close the channel
		close(em.stopCh)
	}
	
	if em.ticker != nil {
		em.ticker.Stop()
	}
	<-em.done // Wait for the goroutine to finish
	em.logger.Infof("Eviction manager stopped")
}

// run is the background loop that periodically checks and evicts objects.
func (em *EvictionManager) run() {
	defer close(em.done)
	
	for {
		select {
		case <-em.stopCh:
			return
		case <-em.ticker.C:
			em.checkAndEvictObjects()
		}
	}
}

// checkAndEvictObjects iterates through all objects and evicts those that meet their policy criteria.
func (em *EvictionManager) checkAndEvictObjects() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get snapshot of all objects to check
	em.node.objectsMu.RLock()
	objectsToCheck := make([]Object, 0, len(em.node.objects))
	for _, obj := range em.node.objects {
		objectsToCheck = append(objectsToCheck, obj)
	}
	em.node.objectsMu.RUnlock()

	evictedCount := 0
	skippedCount := 0

	for _, obj := range objectsToCheck {
		objID := obj.Id()
		
		// Check if object should be evicted
		shouldEvict, reason := em.shouldEvictObject(obj)
		if !shouldEvict {
			if reason != "" {
				skippedCount++
			}
			continue
		}

		// Attempt to evict the object
		err := em.evictObject(ctx, obj)
		if err != nil {
			em.logger.Warnf("Failed to evict object %s: %v", objID, err)
			skippedCount++
		} else {
			em.logger.Infof("Successfully evicted object %s", objID)
			evictedCount++
		}
	}

	if evictedCount > 0 || skippedCount > 0 {
		em.logger.Infof("Eviction cycle completed: evicted=%d, skipped=%d, total_checked=%d",
			evictedCount, skippedCount, len(objectsToCheck))
	}
}

// shouldEvictObject checks if an object should be evicted based on its policy.
// Returns (shouldEvict, reason) where reason is non-empty if eviction was skipped.
func (em *EvictionManager) shouldEvictObject(obj Object) (bool, string) {
	objID := obj.Id()

	// Check if object has an eviction policy
	policy := obj.EvictionPolicy()
	if policy == nil {
		return false, "" // No policy, don't evict (not counted as skipped)
	}

	// Get access and creation times
	em.mu.RLock()
	lastAccess := em.lastAccessTime[objID]
	creation := em.creationTime[objID]
	em.mu.RUnlock()

	// If we don't have a creation time tracked, use the object's creation time
	// and update our tracking for future checks
	if creation.IsZero() {
		creation = obj.CreationTime()
		if !creation.IsZero() {
			em.mu.Lock()
			em.creationTime[objID] = creation
			em.mu.Unlock()
		}
	}

	// Check if policy says to evict
	if !policy.ShouldEvict(lastAccess, creation) {
		return false, "" // Policy says no (not counted as skipped)
	}

	// Check for pending reliable calls - never evict if there are pending calls
	// This is a safety check to ensure we don't lose reliable calls
	nextRcseq := obj.GetNextRcseq()
	if nextRcseq > 0 {
		// Check if there are pending reliable calls
		// We check by seeing if there's any processing activity
		// If the object has processed at least one reliable call, we need to be careful
		
		// Get persistence provider to check for pending calls
		em.node.persistenceProviderMu.RLock()
		provider := em.node.persistenceProvider
		em.node.persistenceProviderMu.RUnlock()

		if provider != nil {
			// Query for pending calls - if any exist, don't evict
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			
			pendingCalls, err := provider.GetPendingReliableCalls(ctx, objID, nextRcseq)
			if err != nil {
				em.logger.Warnf("Failed to check pending calls for %s: %v", objID, err)
				return false, "pending_check_error"
			}
			
			if len(pendingCalls) > 0 {
				em.logger.Infof("Skipping eviction of %s: has %d pending reliable calls", objID, len(pendingCalls))
				return false, "pending_calls"
			}
		}
	}

	return true, ""
}

// evictObject evicts a single object by calling its OnEvict hook and then deleting it.
func (em *EvictionManager) evictObject(ctx context.Context, obj Object) error {
	objID := obj.Id()

	// Get the eviction policy
	policy := obj.EvictionPolicy()
	if policy == nil {
		return nil // No policy, nothing to do
	}

	// Call the OnEvict hook
	if err := policy.OnEvict(ctx, obj); err != nil {
		return err
	}

	// Delete the object from the node
	// Note: DeleteObject handles locking properly and is idempotent
	if err := em.node.DeleteObject(ctx, objID); err != nil {
		return err
	}

	// Clean up tracking data
	em.mu.Lock()
	delete(em.lastAccessTime, objID)
	delete(em.creationTime, objID)
	em.mu.Unlock()

	return nil
}

// TrackAccess records that an object was accessed at the current time.
// This should be called whenever an object is accessed (e.g., via CallObject).
func (em *EvictionManager) TrackAccess(objectID string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.lastAccessTime[objectID] = time.Now()
}

// TrackCreation records that an object was created at the current time.
// This should be called when an object is created.
func (em *EvictionManager) TrackCreation(objectID string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.creationTime[objectID] = time.Now()
}

// GetLastAccessTime returns the last access time for an object.
// Returns zero time if the object has never been accessed.
func (em *EvictionManager) GetLastAccessTime(objectID string) time.Time {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.lastAccessTime[objectID]
}

// GetCreationTime returns the creation time for an object.
// Returns zero time if not tracked.
func (em *EvictionManager) GetCreationTime(objectID string) time.Time {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return em.creationTime[objectID]
}
