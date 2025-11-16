package consensusmanager

import (
	"sync"
	"sync/atomic"
)

// cowMap implements a copy-on-write map with reference counting for efficient cloning.
// It uses a shared data pointer with reference counting to avoid deep copies until
// a modification occurs. Thread-safe for concurrent reads and writes.
type cowMap[K comparable, V any] struct {
	mu   sync.RWMutex
	data *cowMapData[K, V]
}

// cowMapData holds the actual map data and reference count
type cowMapData[K comparable, V any] struct {
	m       map[K]V
	refCount atomic.Int32
}

// newCowMap creates a new copy-on-write map
func newCowMap[K comparable, V any]() *cowMap[K, V] {
	data := &cowMapData[K, V]{
		m: make(map[K]V),
	}
	data.refCount.Store(1)
	return &cowMap[K, V]{
		data: data,
	}
}

// clone creates a shallow copy of the cowMap that shares the underlying data
// until a write operation occurs on either copy
func (cm *cowMap[K, V]) clone() *cowMap[K, V] {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	// Increment reference count
	cm.data.refCount.Add(1)
	
	// Return a new cowMap instance sharing the same data
	return &cowMap[K, V]{
		data: cm.data,
	}
}

// get retrieves a value from the map
func (cm *cowMap[K, V]) get(key K) (V, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	v, ok := cm.data.m[key]
	return v, ok
}

// set adds or updates a key-value pair in the map
// If the data is shared (refCount > 1), it creates a copy first (copy-on-write)
func (cm *cowMap[K, V]) set(key K, value V) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.ensureUnique()
	cm.data.m[key] = value
}

// delete removes a key from the map
// If the data is shared (refCount > 1), it creates a copy first (copy-on-write)
func (cm *cowMap[K, V]) delete(key K) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	
	cm.ensureUnique()
	delete(cm.data.m, key)
}

// len returns the number of elements in the map
func (cm *cowMap[K, V]) len() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	return len(cm.data.m)
}

// copyTo copies all key-value pairs to the provided destination map
func (cm *cowMap[K, V]) copyTo(dest map[K]V) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	
	for k, v := range cm.data.m {
		dest[k] = v
	}
}

// ensureUnique ensures that this cowMap has a unique copy of the data
// Must be called with write lock held
func (cm *cowMap[K, V]) ensureUnique() {
	// If reference count is 1, we're the only owner - no copy needed
	if cm.data.refCount.Load() == 1 {
		return
	}
	
	// Create a new data copy
	newData := &cowMapData[K, V]{
		m: make(map[K]V, len(cm.data.m)),
	}
	newData.refCount.Store(1)
	
	// Copy all elements
	for k, v := range cm.data.m {
		newData.m[k] = v
	}
	
	// Decrement old data's reference count
	cm.data.refCount.Add(-1)
	
	// Switch to new data
	cm.data = newData
}
