package node

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/object"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/uniqueid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

type Object = object.Object
type ClientObject = client.ClientObject

// Node represents a node in the distributed system that manages objects and clients
type Node struct {
	advertiseAddress      string
	objectTypes           map[string]reflect.Type
	objectTypesMu         sync.RWMutex
	clientObjectType      string
	objects               map[string]Object
	objectsMu             sync.RWMutex
	inspectorClient       inspector_pb.InspectorServiceClient
	logger                *logger.Logger
	startupTime           time.Time
	persistenceProvider   object.PersistenceProvider
	persistenceProviderMu sync.RWMutex
	persistenceInterval   time.Duration
	persistenceCtx        context.Context
	persistenceCancel     context.CancelFunc
	persistenceDone       chan struct{}
}

// NewNode creates a new Node instance
func NewNode(advertiseAddress string) *Node {
	node := &Node{
		advertiseAddress:    advertiseAddress,
		objectTypes:         make(map[string]reflect.Type),
		objects:             make(map[string]Object),
		logger:              logger.NewLogger(fmt.Sprintf("Node@%s", advertiseAddress)),
		persistenceInterval: 5 * time.Minute, // Default to 5 minutes
	}

	return node
}

// Start starts the node and connects it to the inspector
func (node *Node) Start(ctx context.Context) error {
	node.startupTime = time.Now()

	// Start periodic persistence if provider is configured
	if node.persistenceProvider != nil {
		node.StartPeriodicPersistence(ctx)
	}

	return node.connectToInspector()
}

func (node *Node) IsStarted() bool {
	return !node.startupTime.IsZero()
}

// Stop stops the node and unregisters it from the inspector
func (node *Node) Stop(ctx context.Context) error {
	node.logger.Infof("Node stopping")

	// Stop periodic persistence and save all objects one final time
	node.persistenceProviderMu.RLock()
	hasProvider := node.persistenceProvider != nil
	node.persistenceProviderMu.RUnlock()

	if hasProvider {
		node.StopPeriodicPersistence()

		// Save all objects before shutting down
		node.logger.Infof("Saving all objects before shutdown...")
		if err := node.SaveAllObjects(ctx); err != nil {
			node.logger.Errorf("Failed to save all objects during shutdown: %v", err)
		}
	}

	// Clear all objects from memory after saving
	node.objectsMu.Lock()
	objectCount := len(node.objects)
	node.objects = make(map[string]Object)
	node.objectsMu.Unlock()
	node.logger.Infof("Cleared %d objects from memory", objectCount)

	return node.unregisterFromInspector()
}

// Connect to inspector service at localhost:8081
func (node *Node) connectToInspector() error {
	conn, err := grpc.NewClient("localhost:8081", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		node.logger.Warnf("Failed to connect to inspector service: %v (continuing without inspector)", err)
		return nil
	}

	inspectorClient := inspector_pb.NewInspectorServiceClient(conn)
	node.logger.Infof("Connected to inspector service at localhost:8081")
	node.inspectorClient = inspectorClient

	// Register this node with the inspector
	registerReq := &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: node.advertiseAddress,
	}

	node.objectsMu.RLock()
	for _, obj := range node.objects {
		registerReq.Objects = append(registerReq.Objects, &inspector_pb.Object{
			Id:    obj.Id(),
			Class: obj.Type(),
		})
	}
	node.objectsMu.RUnlock()

	_, err = inspectorClient.RegisterNode(context.Background(), registerReq)
	if err != nil {
		node.logger.Warnf("Failed to register node with inspector: %v (continuing without inspector)", err)
		return nil
	}

	node.logger.Infof("Successfully registered node %s with inspector", node.advertiseAddress)
	return nil
}

// Unregister this node from the inspector service
func (node *Node) unregisterFromInspector() error {
	if node.inspectorClient == nil {
		return nil
	}

	unregisterReq := &inspector_pb.UnregisterNodeRequest{
		AdvertiseAddress: node.advertiseAddress,
	}

	_, err := node.inspectorClient.UnregisterNode(context.Background(), unregisterReq)
	if err != nil {
		node.logger.Warnf("Failed to unregister node from inspector: %v", err)
		return nil
	}

	node.logger.Infof("Successfully unregistered node %s from inspector", node.advertiseAddress)
	return nil
}

// String returns a string representation of the node
func (node *Node) String() string {
	return fmt.Sprintf("Node@%s", node.advertiseAddress)
}

// GetAdvertiseAddress returns the advertise address of the node
func (node *Node) GetAdvertiseAddress() string {
	return node.advertiseAddress
}

func (node *Node) RegisterClientType(clientObj ClientObject) {
	if node.clientObjectType != "" {
		panic(fmt.Errorf("client object type already registered: %s", node.clientObjectType))
	}
	node.clientObjectType = reflect.TypeOf(clientObj).Elem().Name()
	node.RegisterObjectType(clientObj)
	node.logger.Infof("Registered client type %s", node.clientObjectType)
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

// RegisterClient creates a new client object
func (node *Node) RegisterClient(ctx context.Context) (ClientObject, error) {
	// Placeholder for client registration logic
	clientObj, err := node.newClientObject(ctx)
	if err != nil {
		return nil, err
	}

	return clientObj.(ClientObject), nil
}

func (node *Node) newClientObject(ctx context.Context) (Object, error) {
	if node.clientObjectType == "" {
		return nil, fmt.Errorf("client object type not registered")
	}

	clientId := node.advertiseAddress + "/" + uniqueid.UniqueId()
	clientObj, err := node.createObject(ctx, node.clientObjectType, clientId)
	if err != nil {
		return nil, fmt.Errorf("failed to create ClientProxy object: %w", err)
	}

	node.logger.Infof("Registered new client: %s", clientObj.String())
	return clientObj, nil
}

// UnregisterClient removes a client by its ID
func (node *Node) UnregisterClient(clientId string) {
	node.destroyObject(clientId)
	node.logger.Infof("Unregistered client: %s", clientId)
}

func (node *Node) CallClient(ctx context.Context, clientId, method string, requestAny *anypb.Any) (*anypb.Any, error) {
	request, err := requestAny.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal request: %w", err)
	}

	resp, err := node.CallObject(ctx, node.clientObjectType, clientId, method, request)
	if err != nil {
		return nil, err
	}

	var anyResp anypb.Any
	err = anyResp.MarshalFrom(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return &anyResp, nil
}

// CallObject implements the Goverse gRPC service CallObject method
func (node *Node) CallObject(ctx context.Context, typ string, id string, method string, request proto.Message) (proto.Message, error) {
	node.logger.Infof("CallObject received: type=%s, id=%s, method=%s", typ, id, method)
	node.objectsMu.RLock()
	obj, ok := node.objects[id]
	node.objectsMu.RUnlock()
	if !ok {
		// Object doesn't exist - create it automatically
		// This will handle persistence: load if available, otherwise FromData(nil)
		node.logger.Infof("Object %s not found, creating automatically", id)
		var err error
		obj, err = node.createObject(ctx, typ, id)
		if err != nil {
			return nil, fmt.Errorf("failed to auto-create object %s: %w", id, err)
		}
	}

	// Validate that the provided type matches the object's actual type
	if obj.Type() != typ {
		return nil, fmt.Errorf("object type mismatch: expected %s, got %s for object %s", typ, obj.Type(), id)
	}

	objValue := reflect.ValueOf(obj)
	methodValue := objValue.MethodByName(method)
	if !methodValue.IsValid() {
		return nil, fmt.Errorf("method not found in class %s: %s", obj.Type(), method)
	}

	methodType := methodValue.Type()
	if methodType.NumIn() != 2 ||
		!methodType.In(0).Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) ||
		!isConcreteProtoMessage(methodType.In(1)) {
		return nil, fmt.Errorf("method %s has invalid argument types (expected: context.Context, *Message; got: %s, %s)", method, methodType.In(0), methodType.In(1))
	}

	// Check method return types: (proto.Message, error)
	if methodType.NumOut() != 2 ||
		!isConcreteProtoMessage(methodType.Out(0)) ||
		!methodType.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return nil, fmt.Errorf("method %s has invalid return types (expected: *Message, error; got: %s, %s)", method, methodType.Out(0), methodType.Out(1))
	}

	// Unmarshal request to the expected concrete proto.Message type
	expectedReqType := methodType.In(1)
	node.logger.Infof("Request value: %+v", request)

	if reflect.TypeOf(request) != expectedReqType {
		return nil, fmt.Errorf("request type mismatch: expected %s, got %s", expectedReqType, reflect.TypeOf(request))
	}

	// Call the method with the unmarshaled request as argument
	results := methodValue.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(request)})

	if len(results) != 2 {
		return nil, fmt.Errorf("method %s has invalid signature", method)
	}

	// Return the actual result from the method
	resp, errVal := results[0], results[1]
	if !errVal.IsNil() {
		err := errVal.Interface().(error)
		return nil, err
	}

	node.logger.Infof("Response type: %T, value: %+v", resp.Interface(), resp.Interface())
	return resp.Interface().(proto.Message), nil
}

// CreateObject implements the Goverse gRPC service CreateObject method
func (node *Node) CreateObject(ctx context.Context, typ string, id string) (string, error) {
	node.logger.Infof("CreateObject received: type=%s, id=%s", typ, id)

	// ID must be specified to ensure it belongs to this node according to shard mapping
	if id == "" {
		return "", fmt.Errorf("object ID must be specified")
	}

	obj, err := node.createObject(ctx, typ, id)
	if err != nil {
		node.logger.Errorf("Failed to create object: %v", err)
		return "", err
	}
	return obj.Id(), nil
}

func (node *Node) createObject(ctx context.Context, typ string, id string) (Object, error) {
	// ID must be specified to ensure proper shard mapping
	if id == "" {
		return nil, fmt.Errorf("object ID must be specified")
	}

	// Check if object already exists
	node.objectsMu.RLock()
	existingObj := node.objects[id]
	node.objectsMu.RUnlock()
	if existingObj != nil {
		// If object exists and has the same type, return it successfully
		if existingObj.Type() == typ {
			node.logger.Infof("Object %s of type %s already exists, returning existing object", id, typ)
			return existingObj, nil
		}
		// Type mismatch - this is an error
		return nil, fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
	}

	node.objectTypesMu.RLock()
	objectType, ok := node.objectTypes[typ]
	node.objectTypesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown object type: %s", typ)
	}

	// Create a new instance of the object
	objectValue := reflect.New(objectType)
	obj, ok := objectValue.Interface().(Object)
	if !ok {
		return nil, fmt.Errorf("type %s does not implement Object interface", typ)
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
				return nil, fmt.Errorf("failed to load object %s from persistence: %w", id, err)
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
		return nil, fmt.Errorf("failed to restore object %s from data: %w", id, err)
	}

	// Lock and store the object in the registry
	// IMPORTANT: Check again after acquiring write lock to prevent race condition
	node.objectsMu.Lock()
	if existingObj := node.objects[id]; existingObj != nil {
		node.objectsMu.Unlock()
		// Another goroutine created the object while we were initializing
		if existingObj.Type() == typ {
			node.logger.Infof("Object %s already created by another goroutine, returning existing object", id)
			return existingObj, nil
		}
		return nil, fmt.Errorf("object with id %s already exists but with different type: expected %s, got %s", id, typ, existingObj.Type())
	}
	node.objects[id] = obj
	node.objectsMu.Unlock()

	node.logger.Infof("Created object %s of type %s", id, typ)
	obj.OnCreated()

	if node.IsStarted() {
		err := node.registerObjectWithInspector(obj)
		if err != nil && !errors.Is(err, context.Canceled) {
			node.logger.Errorf("Failed to register object with inspector: %v", err)
		}
	}

	return obj, nil
}

func (node *Node) destroyObject(id string) {
	node.objectsMu.Lock()
	object, ok := node.objects[id]
	if ok {
		delete(node.objects, id)
	}
	node.objectsMu.Unlock()
	node.logger.Infof("Destroyed object %s", object.String())
}

// DeleteObject removes an object from the node and deletes it from persistence if configured.
// This is a public method that properly handles both memory cleanup and persistence deletion.
// Returns error if object doesn't exist or if persistence deletion fails.
func (node *Node) DeleteObject(ctx context.Context, id string) error {
	// First check if object exists
	node.objectsMu.RLock()
	obj, exists := node.objects[id]
	node.objectsMu.RUnlock()

	if !exists {
		return fmt.Errorf("object %s not found", id)
	}

	// If persistence provider is configured, try to delete from persistence first
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider != nil {
		// Check if object supports persistence
		_, err := obj.ToData()
		if err == nil {
			// Object is persistent, delete from storage
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

	// Remove from memory
	node.objectsMu.Lock()
	delete(node.objects, id)
	node.objectsMu.Unlock()

	node.logger.Infof("Deleted object %s from node", id)
	return nil
}

func (node *Node) registerObjectWithInspector(object Object) error {
	if node.inspectorClient == nil {
		return nil
	}

	req := &inspector_pb.AddOrUpdateObjectRequest{
		Object: &inspector_pb.Object{
			Id:    object.Id(),
			Class: object.Type(),
		},
		NodeAddress: node.advertiseAddress,
	}

	_, err := node.inspectorClient.AddOrUpdateObject(context.Background(), req)
	if err != nil {
		node.logger.Warnf("Failed to register object with inspector: %v", err)
		return nil
	}

	node.logger.Infof("Registered object %s with inspector", object.String())
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

// ObjectInfo represents information about an object
type ObjectInfo struct {
	Type         string
	Id           string
	CreationTime time.Time
}

// PushMessageToClient sends a message to a client's message channel
// Returns nil if successful, error if client not found or not a ClientObject
func (node *Node) PushMessageToClient(clientID string, message proto.Message) error {
	node.objectsMu.RLock()
	obj, ok := node.objects[clientID]
	node.objectsMu.RUnlock()

	if !ok {
		return fmt.Errorf("client not found: %s", clientID)
	}

	clientObj, ok := obj.(ClientObject)
	if !ok {
		return fmt.Errorf("object %s is not a ClientObject", clientID)
	}

	// Send message to the client's channel
	select {
	case clientObj.MessageChan() <- message:
		node.logger.Infof("Pushed message to client %s", clientID)
		return nil
	default:
		return fmt.Errorf("client %s message channel is full or closed", clientID)
	}
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
	node.persistenceProviderMu.RLock()
	provider := node.persistenceProvider
	node.persistenceProviderMu.RUnlock()

	if provider == nil {
		return fmt.Errorf("no persistence provider configured")
	}

	node.objectsMu.RLock()
	objectsCopy := make([]Object, 0, len(node.objects))
	for _, obj := range node.objects {
		objectsCopy = append(objectsCopy, obj)
	}
	node.objectsMu.RUnlock()

	savedCount := 0
	nonPersistentCount := 0
	errorCount := 0

	for _, obj := range objectsCopy {
		// Object might have been deleted after we copied the list
		// Get object data
		data, err := obj.ToData()
		if err == object.ErrNotPersistent {
			// Object is not persistent, skip silently
			node.logger.Infof("Object %s is not persistent", obj)
			nonPersistentCount++
			continue
		}
		if err != nil {
			node.logger.Errorf("Failed to get data for object %s: %v", obj, err)
			errorCount++
			continue
		}

		// Save the object data
		err = object.SaveObject(ctx, provider, obj.Id(), obj.Type(), data)
		if err != nil {
			node.logger.Errorf("Failed to save object %s: %v", obj, err)
			errorCount++
		} else {
			savedCount++
		}
	}

	node.logger.Infof("Persistence summary: saved=%d, non-persistent=%d, errors=%d", savedCount, nonPersistentCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("Failed to save %d objects", errorCount)
	}

	return nil
}
