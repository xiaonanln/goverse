package node

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/node/inspectormanager"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/keylock"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"google.golang.org/protobuf/proto"
)

type Object = object.Object

// Node represents a node in the distributed system that manages objects and clients
//
// Locking Strategy:
// The Node uses a three-level locking hierarchy to ensure thread safety:
//
// 1. stopMu (RWMutex): Coordinates Stop() with in-flight operations
//   - Operations acquire stopMu.RLock() to prevent Stop during execution
//   - Stop() acquires stopMu.Lock() to wait for operations to complete
//
// 2. keyLock (per-object ID): Prevents concurrent create/delete on same object
//   - Create/Delete operations acquire keyLock.Lock(id) for exclusive access
//   - Call/Save operations acquire keyLock.RLock(id) for shared access
//   - Automatically cleaned up via reference counting when no longer in use
//
// 3. objectsMu (RWMutex): Protects the objects map
//   - Brief locks for map read/write operations only
//
// Lock Ordering Rule (MUST be followed to avoid deadlocks):
//
//	stopMu.RLock() → keyLock.Lock/RLock(id) → objectsMu.Lock/RLock()
//
// This ensures:
// - No concurrent create/delete on the same object ID
// - Calls and saves can proceed concurrently on the same object
// - Deletes wait for all calls/saves to complete before removing object
// - Creates prevent any calls/saves/deletes until object is fully initialized
type Node struct {
	advertiseAddress      string
	objectTypes           map[string]reflect.Type
	objectTypesMu         sync.RWMutex
	objects               map[string]Object
	objectsMu             sync.RWMutex
	keyLock               *keylock.KeyLock     // Per-object ID locking for create/delete/call/save coordination
	shardLock             *shardlock.ShardLock // Shard-level locking for ownership transitions (set by cluster during initialization)
	inspectorManager      *inspectormanager.InspectorManager
	logger                *logger.Logger
	startupTime           time.Time
	persistenceProvider   object.PersistenceProvider
	persistenceProviderMu sync.RWMutex
	persistenceInterval   time.Duration
	persistenceCtx        context.Context
	persistenceCancel     context.CancelFunc
	persistenceDone       chan struct{}
	stopped               atomic.Bool  // Atomic flag to indicate node is stopping/stopped
	stopMu                sync.RWMutex // RWMutex to coordinate Stop with in-flight operations
}

// NewNode creates a new Node instance
func NewNode(advertiseAddress string) *Node {
	node := &Node{
		advertiseAddress:    advertiseAddress,
		objectTypes:         make(map[string]reflect.Type),
		objects:             make(map[string]Object),
		keyLock:             keylock.NewKeyLock(),
		inspectorManager:    inspectormanager.NewInspectorManager(advertiseAddress),
		logger:              logger.NewLogger(fmt.Sprintf("Node@%s", advertiseAddress)),
		persistenceInterval: 5 * time.Minute, // Default to 5 minutes
	}

	return node
}

// Start starts the node and connects it to the inspector
// numShards is optional - if -1, existing objects will not be registered with inspector
func (node *Node) Start(ctx context.Context, numShards int) error {
	node.startupTime = time.Now()

	// Start periodic persistence if provider is configured
	if node.persistenceProvider != nil {
		node.StartPeriodicPersistence(ctx)
	}

	// Notify inspector manager about existing objects if numShards is provided
	if numShards >= 0 {
		node.objectsMu.RLock()
		for _, obj := range node.objects {
			shardID := sharding.GetShardID(obj.Id(), numShards)
			node.inspectorManager.NotifyObjectAdded(obj.Id(), obj.Type(), shardID)
		}
		node.objectsMu.RUnlock()
	}

	// Start the inspector manager
	return node.inspectorManager.Start(ctx)
}

func (node *Node) IsStarted() bool {
	return !node.startupTime.IsZero()
}

// Stop stops the node and unregisters it from the inspector
func (node *Node) Stop(ctx context.Context) error {
	node.logger.Infof("Node stopping")

	// Set the stopped flag atomically to signal that no new operations should start
	node.stopped.Store(true)

	// Acquire write lock to wait for all in-flight operations to complete
	// This ensures that all operations that checked the stopped flag before it was set
	// will complete before we proceed with final persistence and cleanup
	node.stopMu.Lock()
	defer node.stopMu.Unlock()

	// Stop periodic persistence and save all objects one final time
	node.persistenceProviderMu.RLock()
	hasProvider := node.persistenceProvider != nil
	node.persistenceProviderMu.RUnlock()

	if hasProvider {
		node.StopPeriodicPersistence()

		// Save all objects before shutting down
		// Use internal version that doesn't check stopped flag since we're already stopping
		node.logger.Infof("Saving all objects before shutdown...")
		if err := node.saveAllObjectsInternal(ctx); err != nil {
			node.logger.Errorf("Failed to save all objects during shutdown: %v", err)
		}
	}

	// Clear all objects from memory after saving
	node.objectsMu.Lock()
	objectCount := len(node.objects)
	node.objects = make(map[string]Object)
	node.objectsMu.Unlock()
	node.logger.Infof("Cleared %d objects from memory", objectCount)

	// Stop the inspector manager and unregister from inspector
	return node.inspectorManager.Stop()
}

// String returns a string representation of the node
func (node *Node) String() string {
	return fmt.Sprintf("Node@%s", node.advertiseAddress)
}

// GetAdvertiseAddress returns the advertise address of the node
func (node *Node) GetAdvertiseAddress() string {
	return node.advertiseAddress
}

// SetShardLock sets the cluster's ShardLock instance for this node
// This must be called during initialization before the node is used concurrently
func (node *Node) SetShardLock(sl *shardlock.ShardLock) {
	node.shardLock = sl
}

// RegisterObjectType registers a new object type with the node
func (node *Node) RegisterObjectType(obj Object) {
	objType := reflect.TypeOf(obj)

	if objType.Kind() != reflect.Ptr {
		panic(fmt.Errorf("must register with a nil pointer"))
	}

	objType = objType.Elem()
	objTypeName := objType.Name()

	if _, ok := node.objectTypes[objTypeName]; ok {
		panic(fmt.Errorf("duplicate object type: %s", objTypeName))
	}

	node.objectTypesMu.Lock()
	defer node.objectTypesMu.Unlock()
	node.objectTypes[objTypeName] = objType
	node.logger.Infof("Registered object type %s = %v", objTypeName, node.objectTypes[objTypeName])
}

// Check if the type is a concrete implementation of proto.Message
func isConcreteProtoMessage(t reflect.Type) bool {
	if t.Kind() != reflect.Ptr {
		return false
	}
	if t.Elem().Kind() != reflect.Struct {
		return false
	}
	protoMessageType := reflect.TypeOf((*proto.Message)(nil)).Elem()
	return t.Implements(protoMessageType)
}

// CallObject implements the Goverse gRPC service CallObject method
func (node *Node) CallObject(ctx context.Context, typ string, id string, method string, request proto.Message) (proto.Message, error) {
	// Start timing for metrics
	startTime := time.Now()
	var callErr error

	// Defer metrics recording to ensure it happens on all return paths
	defer func() {
		duration := time.Since(startTime).Seconds()
		status := "success"
		if callErr != nil {
			status = "failure"
		}
		metrics.RecordMethodCall(node.advertiseAddress, typ, method, status)
		metrics.RecordMethodCallDuration(node.advertiseAddress, typ, method, status, duration)
	}()

	// Lock ordering: stopMu.RLock → per-key RLock → objectsMu
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	if node.stopped.Load() {
		callErr = fmt.Errorf("node is stopped")
		return nil, callErr
	}

	node.logger.Infof("CallObject received: type=%s, id=%s, method=%s", typ, id, method)

	// Auto-create object without shard ID (not available in this context)
	err := node.createObject(ctx, typ, id, -1)
	if err != nil {
		callErr = fmt.Errorf("failed to auto-create object %s: %w", id, err)
		return nil, callErr
	}

	// Now acquire per-key read lock to prevent concurrent delete during method call
	unlockKey := node.keyLock.RLock(id)
	defer unlockKey()

	// Fetch the object while holding the lock
	node.objectsMu.RLock()
	obj, ok := node.objects[id]
	node.objectsMu.RUnlock()

	if !ok {
		// Generally, this should not happen since we just created it if it didn't exist. However, in extreme cases of concurrent deletes, it might.
		callErr = fmt.Errorf("object %s was not found [RETRY]", id)
		return nil, callErr
	}

	// Validate that the provided type matches the object's actual type
	if obj.Type() != typ {
		callErr = fmt.Errorf("object type mismatch: expected %s, got %s for object %s", typ, obj.Type(), id)
		return nil, callErr
	}

	objValue := reflect.ValueOf(obj)
	methodValue := objValue.MethodByName(method)
	if !methodValue.IsValid() {
		callErr = fmt.Errorf("method not found in class %s: %s", obj.Type(), method)
		return nil, callErr
	}

	methodType := methodValue.Type()
	if methodType.NumIn() != 2 ||
		!methodType.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) ||
		!isConcreteProtoMessage(methodType.In(1)) {
		callErr = fmt.Errorf("method %s has invalid argument types (expected: context.Context, *Message; got: %s, %s)", method, methodType.In(0), methodType.In(1))
		return nil, callErr
	}

	// Check method return types: (proto.Message, error)
	if methodType.NumOut() != 2 ||
		!isConcreteProtoMessage(methodType.Out(0)) ||
		!methodType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		callErr = fmt.Errorf("method %s has invalid return types (expected: *Message, error; got: %s, %s)", method, methodType.Out(0), methodType.Out(1))
		return nil, callErr
	}

	// Unmarshal request to the expected concrete proto.Message type
	expectedReqType := methodType.In(1)
	node.logger.Infof("Request value: %+v", request)

	if reflect.TypeOf(request) != expectedReqType {
		callErr = fmt.Errorf("request type mismatch: expected %s, got %s", expectedReqType, reflect.TypeOf(request))
		return nil, callErr
	}

	// Call the method with the unmarshaled request as argument
	// At this point the per-key read lock is still held.
	// This guarantees that no concurrent Delete/Create can remove or replace the object while the user method executes.
	results := methodValue.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(request)})

	if len(results) != 2 {
		callErr = fmt.Errorf("method %s has invalid signature", method)
		return nil, callErr
	}

	// Return the actual result from the method
	resp, errVal := results[0], results[1]

	if !errVal.IsNil() {
		callErr = errVal.Interface().(error)
		return nil, callErr
	}

	node.logger.Infof("Response type: %T, value: %+v", resp.Interface(), resp.Interface())
	return resp.Interface().(proto.Message), nil
}

// CreateObject implements the Goverse gRPC service CreateObject method
// shardID is optional - if -1, metrics and inspector notifications will not include shard info
func (node *Node) CreateObject(ctx context.Context, typ string, id string, shardID int) (string, error) {
	// Lock ordering: stopMu.RLock → per-key Lock → objectsMu
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	if node.stopped.Load() {
		return "", fmt.Errorf("node is stopped")
	}

	node.logger.Infof("CreateObject received: type=%s, id=%s", typ, id)

	err := node.createObject(ctx, typ, id, shardID)
	if err != nil {
		node.logger.Errorf("Failed to create object: %v", err)
		return "", err
	}
	return id, nil
}

// createObject creates a new object of the specified type and ID
// createObject MUST not return the object because the keylock is not held after return
// shardID is optional - if -1, metrics and inspector notifications will not include shard info
func (node *Node) createObject(ctx context.Context, typ string, id string, shardID int) error {
	// ID must be specified to ensure proper shard mapping
	if id == "" {
		return fmt.Errorf("object ID must be specified")
	}

	// Acquire shard read lock to prevent concurrent ownership transitions
	// This ensures the shard mapping remains stable during object creation
	// Lock ordering: shard RLock → per-key Lock → objectsMu
	if node.shardLock != nil {
		unlockShard := node.shardLock.AcquireRead(id)
		defer unlockShard()
	}

	// Check if object already exists first (with just objectsMu read lock)
	node.objectsMu.RLock()
	existingObj := node.objects[id]
	node.objectsMu.RUnlock()
	if existingObj != nil {
		// If object exists and has the same type, return success
		if existingObj.Type() == typ {
			node.logger.Infof("Object %s of type %s already exists, returning existing object", id, typ)
			return nil
		}
		// Type mismatch - this is an error
		return fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
	}

	// Lock ordering: per-key Lock → objectsMu
	// Acquire per-key exclusive lock to prevent concurrent create/delete/call on this object
	unlockKey := node.keyLock.Lock(id)
	defer unlockKey()

	// Check if object already exists first (with just objectsMu read lock)
	node.objectsMu.RLock()
	existingObj = node.objects[id]
	node.objectsMu.RUnlock()

	if existingObj != nil {
		// If object exists and has the same type, return success
		if existingObj.Type() == typ {
			node.logger.Infof("Object %s of type %s already exists, returning existing object", id, typ)
			return nil
		}
		// Type mismatch - this is an error
		return fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
	}

	node.objectTypesMu.RLock()
	objectType, ok := node.objectTypes[typ]
	node.objectTypesMu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown object type: %s", typ)
	}

	// Create a new instance of the object
	objectValue := reflect.New(objectType)
	obj, ok := objectValue.Interface().(Object)
	if !ok {
		return fmt.Errorf("type %s does not implement Object interface", typ)
	}

	// Initialize the object first (without data)
	obj.OnInit(obj, id)

	// Now handle data initialization - either from persistence or nil
	var dataToRestore proto.Message
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider != nil {
		// Try to get a template proto.Message to load into
		protoMsg, err := obj.ToData()
		if err == nil {
			// Object supports persistence, try to load
			err = object.LoadObject(ctx, provider, id, protoMsg)
			if err == nil {
				// Successfully loaded from persistence
				node.logger.Infof("Loaded object %s from persistence", id)
				dataToRestore = protoMsg
			} else if errors.Is(err, object.ErrObjectNotFound) {
				// Object not found in storage, will call FromData(nil) to indicate new creation
				node.logger.Infof("Object %s not found in persistence, creating new object", id)
				dataToRestore = nil
			} else {
				// Other error loading from persistence -> treat as serious error
				node.logger.Errorf("Failed to load object %s from persistence: %v", id, err)
				return fmt.Errorf("failed to load object %s from persistence: %w", id, err)
			}
		} else if errors.Is(err, object.ErrNotPersistent) {
			// Object type doesn't support persistence, call FromData(nil)
			node.logger.Infof("Object type %s is not persistent, creating new object", typ)
			dataToRestore = nil
		}
	}

	// Always call FromData (even with nil data) to ensure consistent initialization
	err := obj.FromData(dataToRestore)
	if err != nil {
		node.logger.Errorf("Failed to restore object %s from data: %v", id, err)
		return fmt.Errorf("failed to restore object %s from data: %w", id, err)
	}

	// Now add the object to the map after OnCreated has completed
	// With per-key locking, no other goroutine can create this same ID concurrently
	// so we don't need to check again
	node.objectsMu.Lock()
	node.objects[id] = obj
	node.objectsMu.Unlock()

	node.logger.Infof("Created object %s of type %s", id, typ)
	obj.OnCreated()

	// Notify inspector manager and record metrics if shard ID was provided
	if shardID >= 0 {
		node.inspectorManager.NotifyObjectAdded(id, typ, shardID)
		metrics.RecordObjectCreated(node.advertiseAddress, typ, shardID)
	}

	return nil
}

func (node *Node) destroyObject(id string, shardID int) {
	// Get object type before deletion for metrics
	node.objectsMu.Lock()
	obj, exists := node.objects[id]
	var objectType string
	if exists {
		objectType = obj.Type()
		delete(node.objects, id)
	}
	node.objectsMu.Unlock()
	node.logger.Infof("Destroyed object %s", id)

	// Notify inspector manager about object removal
	node.inspectorManager.NotifyObjectRemoved(id)

	// Record metrics if object existed and shard ID was provided
	if exists && shardID >= 0 {
		metrics.RecordObjectDeleted(node.advertiseAddress, objectType, shardID)
	}
}

// DeleteObject removes an object from the node and deletes it from persistence if configured.
// This is a public method that properly handles both memory cleanup and persistence deletion.
// This operation is idempotent - if the object doesn't exist, no error is returned.
// If the node is stopped, this operation succeeds since all objects are already cleared.
// Returns error only if persistence deletion fails.
// shardID is optional - if -1, metrics will not include shard info
func (node *Node) DeleteObject(ctx context.Context, id string, shardID int) error {
	// Lock ordering: stopMu.RLock → per-key Lock → objectsMu
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	if node.stopped.Load() {
		// Node is stopped - all objects are already cleared, so deletion succeeds (idempotent)
		node.logger.Infof("Node is stopped, object %s already cleared", id)
		return nil
	}

	// Acquire per-key exclusive lock to prevent concurrent create/delete/call on this object
	unlockKey := node.keyLock.Lock(id)
	defer unlockKey()

	// Check if object exists (must hold objectsMu for map access)
	node.objectsMu.RLock()
	obj, exists := node.objects[id]
	node.objectsMu.RUnlock()

	if !exists {
		// Object doesn't exist - deletion is idempotent, this is not an error
		node.logger.Infof("Object %s does not exist, nothing to delete", id)
		return nil
	}

	// If persistence provider is configured, delete from persistence while holding the lock
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider != nil {
		// Check if object supports persistence
		_, err := obj.ToData()
		if err == nil {
			// Object is persistent, delete from storage (while holding objectsMu.Lock())
			node.logger.Infof("Deleting object %s from persistence", id)
			err = provider.DeleteObject(ctx, id)
			if err != nil {
				node.logger.Errorf("Failed to delete object %s from persistence: %v", id, err)
				return fmt.Errorf("failed to delete object from persistence: %w", err)
			}
			node.logger.Infof("Successfully deleted object %s from persistence", id)
		} else if errors.Is(err, object.ErrNotPersistent) {
			// Object is not persistent, skip persistence deletion
			node.logger.Infof("Object %s is not persistent, skipping persistence deletion", id)
		} else {
			// Some other error occurred
			node.logger.Warnf("Could not check persistence for object %s: %v", id, err)
		}
	}

	node.destroyObject(id, shardID)
	node.logger.Infof("Deleted object %s from node", id)
	return nil
}

func (node *Node) NumObjects() int {
	node.objectsMu.RLock()
	defer node.objectsMu.RUnlock()
	return len(node.objects)
}

func (node *Node) UptimeSeconds() int64 {
	return int64(time.Since(node.startupTime).Seconds())
}

// ListObjects returns information about all objects on this node
func (node *Node) ListObjects() []ObjectInfo {
	node.objectsMu.RLock()
	defer node.objectsMu.RUnlock()

	objects := make([]ObjectInfo, 0, len(node.objects))
	for _, obj := range node.objects {
		objects = append(objects, ObjectInfo{
			Type:         obj.Type(),
			Id:           obj.Id(),
			CreationTime: obj.CreationTime(),
		})
	}
	return objects
}

// ListObjectIDs returns a list of all object IDs on this node
// Returns nil if there are no objects
func (node *Node) ListObjectIDs() []string {
	node.objectsMu.RLock()
	defer node.objectsMu.RUnlock()

	if len(node.objects) == 0 {
		return nil
	}

	objectIDs := make([]string, 0, len(node.objects))
	for _, obj := range node.objects {
		objectIDs = append(objectIDs, obj.Id())
	}
	return objectIDs
}

// ObjectInfo represents information about an object
type ObjectInfo struct {
	Type         string
	Id           string
	CreationTime time.Time
}

// SetPersistenceProvider configures the persistence provider for this node
// Must be called before Start() to enable periodic persistence
func (node *Node) SetPersistenceProvider(provider object.PersistenceProvider) {
	node.persistenceProviderMu.Lock()
	defer node.persistenceProviderMu.Unlock()
	node.persistenceProvider = provider
}

// SetPersistenceInterval configures how often objects are persisted
// Must be called before Start() to take effect
func (node *Node) SetPersistenceInterval(interval time.Duration) {
	node.persistenceInterval = interval
}

// StartPeriodicPersistence starts the background goroutine that periodically saves objects
func (node *Node) StartPeriodicPersistence(ctx context.Context) {
	node.persistenceProviderMu.RLock()
	hasProvider := node.persistenceProvider != nil
	node.persistenceProviderMu.RUnlock()

	if !hasProvider {
		node.logger.Warnf("Cannot start periodic persistence: no persistence provider configured")
		return
	}

	node.persistenceCtx, node.persistenceCancel = context.WithCancel(ctx)
	node.persistenceDone = make(chan struct{})

	node.logger.Infof("Starting periodic persistence (interval: %v)", node.persistenceInterval)

	go node.periodicPersistenceLoop()
}

// StopPeriodicPersistence stops the periodic persistence goroutine
func (node *Node) StopPeriodicPersistence() {
	if node.persistenceCancel == nil {
		return
	}

	node.logger.Infof("Stopping periodic persistence...")
	node.persistenceCancel()

	// Wait for the goroutine to finish
	if node.persistenceDone != nil {
		<-node.persistenceDone
	}

	node.logger.Infof("Periodic persistence stopped")
}

// periodicPersistenceLoop is the background goroutine that saves objects periodically
func (node *Node) periodicPersistenceLoop() {
	defer close(node.persistenceDone)

	ticker := time.NewTicker(node.persistenceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-node.persistenceCtx.Done():
			node.logger.Infof("Periodic persistence loop stopped")
			return
		case <-ticker.C:
			node.logger.Infof("Running periodic persistence...")
			if err := node.SaveAllObjects(node.persistenceCtx); err != nil {
				node.logger.Errorf("Periodic persistence failed: %v", err)
			} else {
				node.logger.Infof("Periodic persistence completed successfully")
			}
		}
	}
}

// SaveAllObjects saves all persistent objects to storage
// Non-persistent objects are automatically skipped
func (node *Node) SaveAllObjects(ctx context.Context) error {
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	// Note: If called from periodic persistence while stopping, this will return early.
	// The final save will be done by Stop() itself using saveAllObjectsInternal().
	if node.stopped.Load() {
		return fmt.Errorf("node is stopped")
	}

	return node.saveAllObjectsInternal(ctx)
}

// saveAllObjectsInternal performs the actual save operation without lock coordination
// This is used internally by Stop() which already holds the write lock
func (node *Node) saveAllObjectsInternal(ctx context.Context) error {
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider == nil {
		return fmt.Errorf("no persistence provider configured")
	}

	// Get a snapshot of object IDs to save
	node.objectsMu.RLock()
	objectIDs := make([]string, 0, len(node.objects))
	for id := range node.objects {
		objectIDs = append(objectIDs, id)
	}
	node.objectsMu.RUnlock()

	savedCount := 0
	nonPersistentCount := 0
	errorCount := 0

	// Save each object with per-key RLock to prevent concurrent delete/create
	// Lock ordering: per-key RLock → objectsMu.RLock (to get object)
	for _, id := range objectIDs {
		// Acquire per-key read lock to prevent concurrent delete on this object
		unlockKey := node.keyLock.RLock(id)

		// Get the object
		node.objectsMu.RLock()
		obj, exists := node.objects[id]
		node.objectsMu.RUnlock()

		if !exists {
			// Object was deleted between snapshot and now, skip it
			unlockKey()
			continue
		}

		// Get object data
		data, err := obj.ToData()
		if err == object.ErrNotPersistent {
			// Object is not persistent, skip silently
			node.logger.Infof("Object %s is not persistent", obj)
			nonPersistentCount++
			unlockKey()
			continue
		}
		if err != nil {
			node.logger.Errorf("Failed to get data for object %s: %v", obj, err)
			errorCount++
			unlockKey()
			continue
		}

		// Save the object data (while holding per-key RLock)
		err = object.SaveObject(ctx, provider, obj.Id(), obj.Type(), data)
		if err != nil {
			node.logger.Errorf("Failed to save object %s: %v", obj, err)
			errorCount++
		} else {
			savedCount++
		}

		unlockKey()
	}

	node.logger.Infof("Persistence summary: saved=%d, non-persistent=%d, errors=%d", savedCount, nonPersistentCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("Failed to save %d objects", errorCount)
	}

	return nil
}
