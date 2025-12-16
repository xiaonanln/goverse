package node

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/cluster/shardlock"
	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/node/inspectormanager"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/clusterinfo"
	"github.com/xiaonanln/goverse/util/keylock"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"github.com/xiaonanln/goverse/util/objectid"
	"github.com/xiaonanln/goverse/util/protohelper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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
// 2. objectLifecycleLock (per-object ID): Prevents concurrent create/delete on same object
//   - Create/Delete operations acquire objectLifecycleLock.Lock(id) for exclusive access
//   - Call/Save operations acquire objectLifecycleLock.RLock(id) for shared access
//   - Automatically cleaned up via reference counting when no longer in use
//
// 3. objectsMu (RWMutex): Protects the objects map
//   - Brief locks for map read/write operations only
//
// Lock Ordering Rule (MUST be followed to avoid deadlocks):
//
//	stopMu.RLock() → objectLifecycleLock.Lock/RLock(id) → objectsMu.Lock/RLock()
//
// This ensures:
// - No concurrent create/delete on the same object ID
// - Calls and saves can proceed concurrently on the same object
// - Deletes wait for all calls/saves to complete before removing object
// - Creates prevent any calls/saves/deletes until object is fully initialized
type Node struct {
	advertiseAddress      string
	numShards             int // Number of shards in the cluster for shard ID calculation
	objectTypes           map[string]reflect.Type
	objectTypesMu         sync.RWMutex
	objects               map[string]Object
	objectsMu             sync.RWMutex
	objectLifecycleLock   *keylock.KeyLock     // Per-object ID locking for create/delete/call/save coordination
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
	stopped               atomic.Bool                // Atomic flag to indicate node is stopping/stopped
	stopMu                sync.RWMutex               // RWMutex to coordinate Stop with in-flight operations
	lifecycleValidator    *config.LifecycleValidator // Optional: lifecycle validator for CREATE/DELETE control
}

// NewNode creates a new Node instance
func NewNode(advertiseAddress string, numShards int) *Node {
	node := &Node{
		advertiseAddress:    advertiseAddress,
		numShards:           numShards,
		objectTypes:         make(map[string]reflect.Type),
		objects:             make(map[string]Object),
		objectLifecycleLock: keylock.NewKeyLock(),
		inspectorManager:    inspectormanager.NewInspectorManager(advertiseAddress, ""),
		logger:              logger.NewLogger(fmt.Sprintf("Node@%s", advertiseAddress)),
		persistenceInterval: 5 * time.Minute, // Default to 5 minutes
	}

	return node
}

// SetInspectorAddress sets the inspector service address.
// This must be called before Start() to take effect.
// If address is empty or not set, the inspector manager will be disabled.
func (node *Node) SetInspectorAddress(address string) {
	node.logger.Infof("Setting inspector address to: %s", address)
	node.inspectorManager.SetInspectorAddress(address)
}

// Start starts the node and connects it to the inspector
func (node *Node) Start(ctx context.Context) error {
	node.startupTime = time.Now()

	// Start periodic persistence if provider is configured
	if node.persistenceProvider != nil {
		node.StartPeriodicPersistence(ctx)
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

	// Stop periodic persistence BEFORE acquiring write lock to avoid deadlock.
	// The periodicPersistenceLoop may be calling SaveAllObjects which needs stopMu.RLock.
	// If we hold stopMu.Lock while waiting for the goroutine, it would deadlock.
	node.persistenceProviderMu.RLock()
	hasProvider := node.persistenceProvider != nil
	node.persistenceProviderMu.RUnlock()

	if hasProvider {
		node.StopPeriodicPersistence()
	}

	// Acquire write lock to wait for all in-flight operations to complete
	// This ensures that all operations that checked the stopped flag before it was set
	// will complete before we proceed with final persistence and cleanup
	node.stopMu.Lock()
	defer node.stopMu.Unlock()

	// Save all objects one final time
	if hasProvider {
		// Save all objects before shutting down
		// Use locked version since we already hold stopMu write lock
		node.logger.Infof("Saving all objects before shutdown...")
		if err := node.saveAllObjectsLocked(ctx); err != nil {
			node.logger.Errorf("Failed to save all objects during shutdown: %v", err)
		}
	}

	// Clear all objects from memory after saving
	node.objectsMu.Lock()
	objectCount := len(node.objects)
	objects := node.objects
	node.objects = make(map[string]Object)
	node.objectsMu.Unlock()

	// Call Destroy for all objects to allow cleanup
	for _, obj := range objects {
		obj.Destroy()
	}

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

// GetShardID calculates the shard ID for a given object ID using the configured number of shards
func (node *Node) GetShardID(objectID string) int {
	return sharding.GetShardID(objectID, node.numShards)
}

// SetShardLock sets the cluster's ShardLock instance for this node
// This must be called during initialization before the node is used concurrently
func (node *Node) SetShardLock(sl *shardlock.ShardLock) {
	node.shardLock = sl
}

// SetLifecycleValidator sets the lifecycle validator for this node
// This must be called during initialization before the node is used concurrently
func (node *Node) SetLifecycleValidator(lv *config.LifecycleValidator) {
	node.lifecycleValidator = lv
}

// SetClusterInfoProvider sets the consolidated cluster info provider for the node.
// This is the preferred way to provide cluster information to the node's InspectorManager.
// Must be called during initialization before the node is used concurrently.
func (node *Node) SetClusterInfoProvider(provider clusterinfo.ClusterInfoProvider) {
	node.inspectorManager.SetClusterInfoProvider(provider)
}

// NotifyConnectedNodesChanged notifies the inspector that the node's connections have changed.
// This should be called whenever nodes are connected or disconnected.
func (node *Node) NotifyConnectedNodesChanged() {
	node.inspectorManager.UpdateConnectedNodes()
}

// NotifyRegisteredGatesChanged notifies the inspector that the node's registered gates have changed.
// This should be called whenever gates are registered or unregistered.
func (node *Node) NotifyRegisteredGatesChanged() {
	node.inspectorManager.UpdateRegisteredGates()
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

// CallObject implements the Goverse gRPC service CallObject method
func (node *Node) CallObject(ctx context.Context, typ string, id string, method string, request proto.Message) (proto.Message, error) {
	// Start timing for metrics
	startTime := time.Now()
	var callErr error

	// Calculate shard ID for shard-level metrics
	shardID := node.GetShardID(id)

	// Defer metrics recording to ensure it happens on all return paths
	defer func() {
		duration := time.Since(startTime).Seconds()
		status := "success"
		if callErr != nil {
			status = "failure"
		}
		metrics.RecordMethodCall(node.advertiseAddress, typ, method, status)
		metrics.RecordMethodCallDuration(node.advertiseAddress, typ, method, status, duration)
		metrics.RecordShardMethodCall(shardID)
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

	// Auto-create object
	err := node.createObject(ctx, typ, id)
	if err != nil {
		callErr = fmt.Errorf("failed to auto-create object %s: %w", id, err)
		return nil, callErr
	}

	// Now acquire per-key read lock to prevent concurrent delete during method call
	unlockKey := node.objectLifecycleLock.RLock(id)
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

	// Call the method via reflection
	resp, err := obj.InvokeMethod(ctx, method, request)
	if err != nil {
		callErr = err
		return nil, callErr
	}

	// Report successful call to inspector (enabled by default, no config flag)
	node.inspectorManager.NotifyObjectCall(id, typ, method)

	node.logger.Infof("Response type: %T, value: %+v", resp, resp)
	return resp, nil
}

// InsertOrGetReliableCall inserts a reliable call into the database or retrieves an existing one.
// Returns the ReliableCall record which contains the status and any cached result/error.
func (node *Node) InsertOrGetReliableCall(ctx context.Context, callID string, objectType string, objectID string, methodName string, request proto.Message) (*object.ReliableCall, error) {
	// Get the persistence provider
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider == nil {
		return nil, fmt.Errorf("persistence provider is not configured")
	}

	// Marshal the request proto.Message to bytes
	requestData, err := protohelper.MsgToBytes(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Insert or get the reliable call from the database
	node.logger.Infof("Inserting reliable call %s for object %s.%s (type: %s)", callID, objectID, methodName, objectType)
	rc, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to insert or get reliable call: %w", err)
	}

	return rc, nil
}

// ReliableCallObject handles a reliable call request for an object.
// It performs INSERT with the object's seqWriteMu to ensure sequential seq allocation,
// then fetches and executes pending reliable calls from the persistence provider.
func (node *Node) ReliableCallObject(
	ctx context.Context,
	callID string,
	objectType string,
	objectID string,
	methodName string,
	requestData []byte,
) (*anypb.Any, error) {
	// Lock ordering:
	// 1. stopMu.RLock (held throughout)
	// 2. objectLifecycleLock.RLock (held briefly to fetch object and acquire seqWriteMu)
	// 3. object.seqWriteMu (held during INSERT, lifecycle lock released before INSERT)
	// 4. objectsMu (brief reads/writes)
	//
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	if node.stopped.Load() {
		return nil, fmt.Errorf("node is stopped")
	}

	node.logger.Infof("ReliableCallObject received: call_id=%s, object_type=%s, object_id=%s, method=%s",
		callID, objectType, objectID, methodName)

	// Validate input parameters
	if callID == "" {
		return nil, fmt.Errorf("callID cannot be empty")
	}
	if objectType == "" {
		return nil, fmt.Errorf("objectType cannot be empty")
	}
	if objectID == "" {
		return nil, fmt.Errorf("objectID cannot be empty")
	}
	if methodName == "" {
		return nil, fmt.Errorf("methodName cannot be empty")
	}

	// Get the persistence provider to access reliable calls
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider == nil {
		// No persistence provider configured - cannot process reliable calls
		node.logger.Warnf("ReliableCallObject: no persistence provider configured")
		return nil, fmt.Errorf("no persistence provider configured")
	}

	// Auto-create object first if it doesn't exist (needed to access object's seqWriteMu)
	err := node.createObject(ctx, objectType, objectID)
	if err != nil {
		node.logger.Errorf("ReliableCallObject: failed to auto-create object %s: %v", objectID, err)
		return nil, fmt.Errorf("failed to auto-create object %s: %w", objectID, err)
	}

	// Acquire per-key read lock for the entire operation.
	// This prevents DeleteObject from removing the object while we're processing.
	// RLock allows concurrent calls/saves, only DeleteObject (which needs exclusive Lock) waits.
	unlockKey := node.objectLifecycleLock.RLock(objectID)
	defer unlockKey()

	// Fetch the object while holding the lock
	node.objectsMu.RLock()
	obj, ok := node.objects[objectID]
	node.objectsMu.RUnlock()

	if !ok {
		node.logger.Errorf("ReliableCallObject: object %s was not found", objectID)
		return nil, fmt.Errorf("object %s was not found", objectID)
	}

	// Validate that the provided type matches the object's actual type
	if obj.Type() != objectType {
		node.logger.Errorf("ReliableCallObject: object type mismatch: expected %s, got %s for object %s", objectType, obj.Type(), objectID)
		return nil, fmt.Errorf("object type mismatch: expected %s, got %s for object %s", objectType, obj.Type(), objectID)
	}

	// Acquire object's sequential write lock for INSERT
	// This serializes all INSERTs for the same object, ensuring sequential seq allocation
	unlockSeqWrite := obj.LockSeqWrite()

	// INSERT or GET the reliable call while holding the object's sequential write lock
	node.logger.Infof("INSERT or GET reliable call %s for object %s.%s (type: %s) with sequential write lock",
		callID, objectID, methodName, objectType)
	rc, err := provider.InsertOrGetReliableCall(ctx, callID, objectID, objectType, methodName, requestData)

	// Release the sequential write lock immediately after INSERT
	unlockSeqWrite()

	if err != nil {
		node.logger.Errorf("Failed to insert or get reliable call %s: %v", callID, err)
		return nil, fmt.Errorf("failed to insert or get reliable call: %w", err)
	}

	// Check if the call was already completed (idempotency)
	switch rc.Status {
	case "success":
		node.logger.Infof("Reliable call %s already succeeded, returning cached result", callID)
		return protohelper.BytesToAny(rc.ResultData)
	case "failed":
		node.logger.Infof("Reliable call %s already failed, returning cached error", callID)
		return nil, fmt.Errorf("reliable call failed: %s", rc.Error)
	}

	// Call is pending - continue with execution
	seq := rc.Seq
	node.logger.Infof("Reliable call %s is pending (seq: %d), processing", callID, seq)

	// Trigger processing of pending reliable calls for this object
	// This will fetch pending calls from the database and execute them sequentially
	resultChan := obj.ProcessPendingReliableCalls(provider, seq)

	// Wait for our call's result with one retry if still pending.
	// Pending on first attempt means our INSERT committed after the processing loop's SELECT.
	// Re-triggering MUST see our committed INSERT, so one retry is sufficient.
	for attempt := 0; attempt < 2; attempt++ {
		// Channel sends the ReliableCall if processed, or closes without sending if:
		// - seq was already processed (seq < nextRcseq)
		// - object was destroyed during processing
		// - processing encountered an error
		call := <-resultChan

		// If not received from channel, fetch from database
		if call == nil {
			var err error
			call, err = provider.GetReliableCallBySeq(ctx, seq)
			if err != nil {
				node.logger.Errorf("Failed to retrieve call seq=%d from database: %v", seq, err)
				return nil, fmt.Errorf("failed to retrieve call from database: %w", err)
			}
		}

		switch call.Status {
		case "success":
			resultAny, err := protohelper.BytesToAny(call.ResultData)
			if err != nil {
				node.logger.Errorf("Failed to wrap result for call seq=%d: %v", seq, err)
				return nil, fmt.Errorf("failed to wrap result: %w", err)
			}
			node.logger.Infof("Successfully retrieved result for call seq=%d (call_id=%s)", seq, callID)
			return resultAny, nil
		case "failed":
			node.logger.Errorf("Reliable call seq=%d (call_id=%s) failed: %s", seq, callID, call.Error)
			return nil, fmt.Errorf("reliable call failed: %s", call.Error)
		}

		// Still pending - re-trigger processing on first attempt only
		if attempt == 0 {
			node.logger.Infof("Reliable call seq=%d (call_id=%s) still pending, re-triggering processing", seq, callID)
			resultChan = obj.ProcessPendingReliableCalls(provider, seq)
		}
	}

	// Still pending after retry - this should never happen since our INSERT is committed
	node.logger.Errorf("BUG: Reliable call seq=%d (call_id=%s) still pending after retry", seq, callID)
	return nil, fmt.Errorf("BUG: reliable call seq=%d still pending after retry", seq)
}

// CreateObject implements the Goverse gRPC service CreateObject method
func (node *Node) CreateObject(ctx context.Context, typ string, id string) (string, error) {
	// Lock ordering: stopMu.RLock → per-key Lock → objectsMu
	// Acquire read lock to prevent Stop from proceeding while this operation is in flight
	node.stopMu.RLock()
	defer node.stopMu.RUnlock()

	// Check if node is stopped after acquiring lock
	if node.stopped.Load() {
		return "", fmt.Errorf("node is stopped")
	}

	node.logger.Infof("CreateObject received: type=%s, id=%s", typ, id)

	err := node.createObject(ctx, typ, id)
	if err != nil {
		node.logger.Errorf("Failed to create object: %v", err)
		return "", err
	}
	return id, nil
}

// createObject creates a new object of the specified type and ID
// createObject MUST not return the object because the keylock is not held after return
func (node *Node) createObject(ctx context.Context, typ string, id string) error {
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
	unlockKey := node.objectLifecycleLock.Lock(id)
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

	// Check lifecycle rules for CREATE if lifecycle validator is configured
	// Note: This applies to both explicit CreateObject and CallObject auto-creation
	if node.lifecycleValidator != nil {
		if err := node.lifecycleValidator.CheckNodeCreate(typ, id); err != nil {
			node.logger.Warnf("Create denied by lifecycle rules: type=%s, id=%s: %v", typ, id, err)
			return fmt.Errorf("create denied for %s/%s", typ, id)
		}
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
			nextRcseq, err := object.LoadObject(ctx, provider, id, protoMsg)
			if err == nil {
				// Successfully loaded from persistence
				node.logger.Infof("Loaded object %s from persistence (next_rcseq=%d)", id, nextRcseq)
				dataToRestore = protoMsg
				obj.SetNextRcseq(nextRcseq)
			} else if errors.Is(err, object.ErrObjectNotFound) {
				// Object not found in storage, will call FromData(nil) to indicate new creation
				node.logger.Infof("Object %s not found in persistence, creating new object", id)
				dataToRestore = nil
				obj.SetNextRcseq(0) // Default to 0 for new objects
			} else {
				// Other error loading from persistence -> treat as serious error
				node.logger.Errorf("Failed to load object %s from persistence: %v", id, err)
				return fmt.Errorf("failed to load object %s from persistence: %w", id, err)
			}
		} else if errors.Is(err, object.ErrNotPersistent) {
			// Object type doesn't support persistence, call FromData(nil)
			node.logger.Infof("Object type %s is not persistent, creating new object", typ)
			dataToRestore = nil
			obj.SetNextRcseq(0) // Default to 0 for non-persistent objects
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

	// Process any pending reliable calls for this object
	// This ensures calls made while the object was inactive are processed upon activation
	if provider != nil {
		// Trigger processing without waiting for result (fire-and-forget)
		// Pass math.MaxInt64 as seq to trigger processing of all pending calls.
		// The seq parameter only determines which specific call result to send on the channel;
		// the processing loop fetches and executes ALL pending calls regardless of the seq value.
		// Since we discard the channel, the specific seq doesn't matter - we just need
		// a value >= nextRcseq to trigger the processing goroutine.
		_ = obj.ProcessPendingReliableCalls(provider, math.MaxInt64)
	}

	// Notify inspector manager with shard ID
	// Fixed-node objects don't belong to any shard, use -1 to indicate this
	shardID := node.GetShardID(id)
	parsed, err := objectid.ParseObjectID(id)
	if err == nil && parsed.IsFixedNodeFormat() {
		shardID = -1
	}
	node.inspectorManager.NotifyObjectAdded(id, typ, shardID)

	return nil
}

func (node *Node) destroyObject(id string) {
	// Delete object from map
	node.objectsMu.Lock()
	obj, exists := node.objects[id]
	if exists {
		delete(node.objects, id)
	}
	node.objectsMu.Unlock()

	// Call Destroy to allow the object to clean up
	if exists && obj != nil {
		obj.Destroy()
	}

	node.logger.Infof("Destroyed object %s", id)

	// Notify inspector manager about object removal
	node.inspectorManager.NotifyObjectRemoved(id)
}

// DeleteObject removes an object from the node and deletes it from persistence if configured.
// This is a public method that properly handles both memory cleanup and persistence deletion.
// This operation is idempotent - if the object doesn't exist, no error is returned.
// If the node is stopped, this operation succeeds since all objects are already cleared.
// Returns error only if persistence deletion fails or lifecycle rules deny the deletion.
func (node *Node) DeleteObject(ctx context.Context, id string) error {
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
	unlockKey := node.objectLifecycleLock.Lock(id)
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

	// Check lifecycle rules for DELETE if lifecycle validator is configured
	// Note: We only check here because we now know the object type
	if node.lifecycleValidator != nil {
		if err := node.lifecycleValidator.CheckNodeDelete(obj.Type(), id); err != nil {
			node.logger.Warnf("Delete denied by lifecycle rules: type=%s, id=%s: %v", obj.Type(), id, err)
			return fmt.Errorf("delete denied for %s/%s", obj.Type(), id)
		}
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

	node.destroyObject(id)
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

// GetPersistenceProvider returns the persistence provider for this node
// Returns nil if no persistence provider is configured
func (node *Node) GetPersistenceProvider() object.PersistenceProvider {
	node.persistenceProviderMu.RLock()
	defer node.persistenceProviderMu.RUnlock()
	return node.persistenceProvider
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
	// The final save will be done by Stop() itself using saveAllObjectsLocked().
	if node.stopped.Load() {
		return fmt.Errorf("node is stopped")
	}

	return node.saveAllObjectsLocked(ctx)
}

// saveAllObjectsLocked performs the actual save operation.
// REQUIRES: caller must hold stopMu (either read or write lock)
func (node *Node) saveAllObjectsLocked(ctx context.Context) error {
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
		unlockKey := node.objectLifecycleLock.RLock(id)

		// Get the object
		node.objectsMu.RLock()
		obj, exists := node.objects[id]
		node.objectsMu.RUnlock()

		if !exists {
			// Object was deleted between snapshot and now, skip it
			unlockKey()
			continue
		}

		// CHANGED: Use atomic snapshot instead of separate calls
		data, nextRcseq, err := obj.ToDataWithSeq()
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

		// Save with consistent (data, nextRcseq) pair
		err = object.SaveObject(ctx, provider, obj.Id(), obj.Type(), data, nextRcseq)
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
		return fmt.Errorf("failed to save %d objects", errorCount)
	}

	return nil
}
