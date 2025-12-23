package node

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/goverse/object"
)

// TestObjectNoEviction is a test object with no eviction policy (default behavior)
type TestObjectNoEviction struct {
	object.BaseObject
}

func (obj *TestObjectNoEviction) OnCreated() {}

// TestObjectIdleTimeout is a test object with idle timeout eviction policy
type TestObjectIdleTimeout struct {
	object.BaseObject
	idleTimeout time.Duration
}

func (obj *TestObjectIdleTimeout) OnCreated() {}

func (obj *TestObjectIdleTimeout) EvictionPolicy() object.EvictionPolicy {
	return &object.IdleTimeoutPolicy{
		IdleTimeout: obj.idleTimeout,
	}
}

// TestObjectTTL is a test object with TTL eviction policy
type TestObjectTTL struct {
	object.BaseObject
	ttl time.Duration
}

func (obj *TestObjectTTL) OnCreated() {}

func (obj *TestObjectTTL) EvictionPolicy() object.EvictionPolicy {
	return &object.TTLPolicy{
		TTL: obj.ttl,
	}
}

// TestObjectCustomPolicy is a test object with a custom eviction policy
type TestObjectCustomPolicy struct {
	object.BaseObject
	onEvictCalled *atomic.Bool
	onEvictError  error
}

func (obj *TestObjectCustomPolicy) OnCreated() {}

type customPolicy struct {
	shouldEvict   bool
	onEvictCalled *atomic.Bool
	onEvictError  error
}

func (p *customPolicy) ShouldEvict(lastAccessTime time.Time, creationTime time.Time) bool {
	return p.shouldEvict
}

func (p *customPolicy) OnEvict(ctx context.Context, obj object.Object) error {
	if p.onEvictCalled != nil {
		p.onEvictCalled.Store(true)
	}
	return p.onEvictError
}

func (obj *TestObjectCustomPolicy) EvictionPolicy() object.EvictionPolicy {
	return &customPolicy{
		shouldEvict:   true,
		onEvictCalled: obj.onEvictCalled,
		onEvictError:  obj.onEvictError,
	}
}

// TestEviction_NoPolicy tests that objects with nil policy are never evicted
func TestEviction_NoPolicy(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectNoEviction)(nil))

	// Start the node to initialize eviction manager
	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with no eviction policy
	objID := "no-eviction-obj"
	_, err := node.CreateObject(ctx, "TestObjectNoEviction", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Verify object exists
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object, got %d", node.NumObjects())
	}

	// Manually trigger eviction check (wait a bit to ensure it would evict if it could)
	time.Sleep(100 * time.Millisecond)
	node.evictionManager.checkAndEvictObjects()

	// Object should still exist (no eviction policy)
	if node.NumObjects() != 1 {
		t.Fatalf("Object with no eviction policy was evicted")
	}

	// Verify object is still in the map
	node.objectsMu.RLock()
	_, exists := node.objects[objID]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("Object should still exist after eviction check")
	}
}

// TestEviction_IdleTimeout tests that objects are evicted after idle timeout
func TestEviction_IdleTimeout(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	
	// Register a factory function to create objects with specific idle timeout
	node.RegisterObjectType((*TestObjectIdleTimeout)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with a 200ms idle timeout
	objID := "idle-timeout-obj"
	_, err := node.CreateObject(ctx, "TestObjectIdleTimeout", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	// Set the idle timeout on the created object
	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectIdleTimeout)
	node.objectsMu.RUnlock()
	obj.idleTimeout = 200 * time.Millisecond

	// Verify object exists
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object, got %d", node.NumObjects())
	}

	// Wait for idle timeout to elapse
	time.Sleep(250 * time.Millisecond)

	// Manually trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// Object should be evicted
	if node.NumObjects() != 0 {
		t.Fatalf("Expected object to be evicted after idle timeout, but %d objects remain", node.NumObjects())
	}
}

// TestEviction_IdleTimeout_AccessPreventsEviction tests that accessing an object resets idle timer
func TestEviction_IdleTimeout_AccessPreventsEviction(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectIdleTimeout)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with 300ms idle timeout
	objID := "idle-timeout-access-obj"
	_, err := node.CreateObject(ctx, "TestObjectIdleTimeout", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectIdleTimeout)
	node.objectsMu.RUnlock()
	obj.idleTimeout = 300 * time.Millisecond

	// Wait 200ms (less than idle timeout)
	time.Sleep(200 * time.Millisecond)

	// Access the object to reset idle timer
	node.evictionManager.TrackAccess(objID)

	// Wait another 200ms (total 400ms from creation, but only 200ms from last access)
	time.Sleep(200 * time.Millisecond)

	// Manually trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// Object should still exist (accessed recently)
	if node.NumObjects() != 1 {
		t.Fatalf("Expected object to remain after access, but it was evicted")
	}

	// Now wait for idle timeout from last access
	time.Sleep(150 * time.Millisecond) // Total 350ms from last access

	// Trigger eviction check again
	node.evictionManager.checkAndEvictObjects()

	// Now object should be evicted
	if node.NumObjects() != 0 {
		t.Fatalf("Expected object to be evicted after idle timeout from last access")
	}
}

// TestEviction_TTL tests that objects are evicted after TTL expires
func TestEviction_TTL(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectTTL)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with 200ms TTL
	objID := "ttl-obj"
	_, err := node.CreateObject(ctx, "TestObjectTTL", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectTTL)
	node.objectsMu.RUnlock()
	obj.ttl = 200 * time.Millisecond

	// Verify object exists
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object, got %d", node.NumObjects())
	}

	// Wait for TTL to elapse
	time.Sleep(250 * time.Millisecond)

	// Manually trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// Object should be evicted
	if node.NumObjects() != 0 {
		t.Fatalf("Expected object to be evicted after TTL, but %d objects remain", node.NumObjects())
	}
}

// TestEviction_OnEvictHook tests that the OnEvict hook is called before eviction
func TestEviction_OnEvictHook(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectCustomPolicy)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with custom policy
	objID := "custom-policy-obj"
	onEvictCalled := &atomic.Bool{}
	_, err := node.CreateObject(ctx, "TestObjectCustomPolicy", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectCustomPolicy)
	node.objectsMu.RUnlock()
	obj.onEvictCalled = onEvictCalled

	// Trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// OnEvict should have been called
	if !onEvictCalled.Load() {
		t.Fatal("OnEvict hook was not called before eviction")
	}

	// Object should be evicted
	if node.NumObjects() != 0 {
		t.Fatalf("Expected object to be evicted, but %d objects remain", node.NumObjects())
	}
}

// TestEviction_MultipleObjects tests that multiple objects can be evicted in one cycle
func TestEviction_MultipleObjects(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectTTL)(nil))
	node.RegisterObjectType((*TestObjectIdleTimeout)(nil))
	node.RegisterObjectType((*TestObjectNoEviction)(nil))

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create multiple objects with different policies
	ttlObj1ID := "ttl-obj-1"
	_, err := node.CreateObject(ctx, "TestObjectTTL", ttlObj1ID)
	if err != nil {
		t.Fatalf("Failed to create TTL object 1: %v", err)
	}
	node.objectsMu.RLock()
	node.objects[ttlObj1ID].(*TestObjectTTL).ttl = 200 * time.Millisecond
	node.objectsMu.RUnlock()

	ttlObj2ID := "ttl-obj-2"
	_, err = node.CreateObject(ctx, "TestObjectTTL", ttlObj2ID)
	if err != nil {
		t.Fatalf("Failed to create TTL object 2: %v", err)
	}
	node.objectsMu.RLock()
	node.objects[ttlObj2ID].(*TestObjectTTL).ttl = 200 * time.Millisecond
	node.objectsMu.RUnlock()

	idleObj1ID := "idle-obj-1"
	_, err = node.CreateObject(ctx, "TestObjectIdleTimeout", idleObj1ID)
	if err != nil {
		t.Fatalf("Failed to create idle object: %v", err)
	}
	node.objectsMu.RLock()
	node.objects[idleObj1ID].(*TestObjectIdleTimeout).idleTimeout = 200 * time.Millisecond
	node.objectsMu.RUnlock()

	noEvictObjID := "no-evict-obj"
	_, err = node.CreateObject(ctx, "TestObjectNoEviction", noEvictObjID)
	if err != nil {
		t.Fatalf("Failed to create no-eviction object: %v", err)
	}

	// Verify all objects exist
	if node.NumObjects() != 4 {
		t.Fatalf("Expected 4 objects, got %d", node.NumObjects())
	}

	// Wait for eviction policies to trigger
	time.Sleep(250 * time.Millisecond)

	// Trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// Should have 1 object remaining (no-eviction object)
	if node.NumObjects() != 1 {
		t.Fatalf("Expected 1 object remaining, got %d", node.NumObjects())
	}

	// Verify the correct object remains
	node.objectsMu.RLock()
	_, exists := node.objects[noEvictObjID]
	node.objectsMu.RUnlock()
	if !exists {
		t.Fatal("No-eviction object should still exist")
	}
}

// TestEviction_ManagerLifecycle tests that eviction manager starts and stops correctly
func TestEviction_ManagerLifecycle(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)

	// Before start, eviction manager should be nil
	if node.evictionManager != nil {
		t.Fatal("Eviction manager should be nil before Start")
	}

	// Start the node
	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// After start, eviction manager should exist
	if node.evictionManager == nil {
		t.Fatal("Eviction manager should be initialized after Start")
	}

	// Stop the node
	if err := node.Stop(ctx); err != nil {
		t.Fatalf("Failed to stop node: %v", err)
	}

	// Eviction manager should be stopped (done channel closed)
	select {
	case <-node.evictionManager.done:
		// Good, manager stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Eviction manager did not stop within timeout")
	}
}

// TestEviction_PendingReliableCalls tests that objects with pending reliable calls are not evicted
func TestEviction_PendingReliableCalls(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)
	node.RegisterObjectType((*TestObjectTTL)(nil))

	// Create a mock persistence provider that returns pending calls
	provider := &mockPersistenceProviderWithPendingCalls{
		pendingCalls: []*object.ReliableCall{
			{
				Seq:        1,
				CallID:     "call-1",
				ObjectID:   "ttl-obj-pending",
				MethodName: "TestMethod",
				Status:     "pending",
			},
		},
	}
	node.SetPersistenceProvider(provider)

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	// Create an object with short TTL
	objID := "ttl-obj-pending"
	_, err := node.CreateObject(ctx, "TestObjectTTL", objID)
	if err != nil {
		t.Fatalf("Failed to create object: %v", err)
	}

	node.objectsMu.RLock()
	obj := node.objects[objID].(*TestObjectTTL)
	node.objectsMu.RUnlock()
	obj.ttl = 100 * time.Millisecond

	// Wait for TTL to elapse
	time.Sleep(150 * time.Millisecond)

	// Trigger eviction check
	node.evictionManager.checkAndEvictObjects()

	// Object should NOT be evicted because it has pending reliable calls
	if node.NumObjects() != 1 {
		t.Fatalf("Expected object to remain due to pending calls, but got %d objects", node.NumObjects())
	}

	// Now clear pending calls
	provider.pendingCalls = nil

	// Trigger eviction check again
	node.evictionManager.checkAndEvictObjects()

	// Now object should be evicted
	if node.NumObjects() != 0 {
		t.Fatalf("Expected object to be evicted after pending calls cleared")
	}
}

// mockPersistenceProviderWithPendingCalls is a mock that supports pending calls
type mockPersistenceProviderWithPendingCalls struct {
	MockPersistenceProvider
	pendingCalls []*object.ReliableCall
}

func (m *mockPersistenceProviderWithPendingCalls) GetPendingReliableCalls(ctx context.Context, objectID string, nextRcseq int64) ([]*object.ReliableCall, error) {
	return m.pendingCalls, nil
}

// TestEviction_TrackingMethods tests that TrackAccess and TrackCreation work correctly
func TestEviction_TrackingMethods(t *testing.T) {
	ctx := context.Background()
	node := NewNode("test-node:1234", testNumShards)

	if err := node.Start(ctx); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}
	defer node.Stop(ctx)

	objID := "test-tracking-obj"

	// Track creation
	node.evictionManager.TrackCreation(objID)

	// Verify creation time is set
	creationTime := node.evictionManager.GetCreationTime(objID)
	if creationTime.IsZero() {
		t.Fatal("Creation time should be set after TrackCreation")
	}

	// Initially, no access time
	accessTime := node.evictionManager.GetLastAccessTime(objID)
	if !accessTime.IsZero() {
		t.Fatal("Access time should be zero before TrackAccess")
	}

	// Wait a bit to ensure different timestamps
	time.Sleep(10 * time.Millisecond)

	// Track access
	node.evictionManager.TrackAccess(objID)

	// Verify access time is set
	accessTime = node.evictionManager.GetLastAccessTime(objID)
	if accessTime.IsZero() {
		t.Fatal("Access time should be set after TrackAccess")
	}

	// Access time should be after creation time
	if !accessTime.After(creationTime) {
		t.Fatal("Access time should be after creation time")
	}
}
