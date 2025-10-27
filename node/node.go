package node

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xiaonanln/goverse/client"
	"github.com/xiaonanln/goverse/cluster/etcdmanager"
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
	advertiseAddress string
	objectTypes      map[string]reflect.Type
	objectTypesMu    sync.RWMutex
	clientObjectType string
	objects          map[string]Object
	objectsMu        sync.RWMutex
	inspectorClient  inspector_pb.InspectorServiceClient
	etcdManager      *etcdmanager.EtcdManager
	logger           *logger.Logger
	startupTime      time.Time
}

// NewNode creates a new Node instance
func NewNode(advertiseAddress string) *Node {
	return NewNodeWithEtcd(advertiseAddress, "")
}

// NewNodeWithEtcd creates a new Node instance with etcd address
func NewNodeWithEtcd(advertiseAddress string, etcdAddress string) *Node {
	node := &Node{
		advertiseAddress: advertiseAddress,
		objectTypes:      make(map[string]reflect.Type),
		objects:          make(map[string]Object),
		logger:           logger.NewLogger(fmt.Sprintf("Node@%s", advertiseAddress)),
	}

	// Initialize etcd manager if address is provided
	if etcdAddress == "" {
		etcdAddress = "localhost:2379" // default
	}

	etcdMgr, err := etcdmanager.NewEtcdManager(etcdAddress)
	if err != nil {
		node.logger.Errorf("Failed to create etcd manager: %v", err)
	} else {
		node.etcdManager = etcdMgr
	}

	return node
}

// Start starts the node and connects it to the inspector and etcd
func (node *Node) Start(ctx context.Context) error {
	node.startupTime = time.Now()

	// Connect to etcd
	if node.etcdManager != nil {
		if err := node.etcdManager.Connect(); err != nil {
			node.logger.Errorf("Failed to connect to etcd: %v", err)
			return fmt.Errorf("failed to connect to etcd: %w", err)
		}

		// Register this node with etcd
		if err := node.etcdManager.RegisterNode(ctx, node.advertiseAddress); err != nil {
			node.logger.Errorf("Failed to register node with etcd: %v", err)
			return fmt.Errorf("failed to register node with etcd: %w", err)
		}

		// Start watching for node changes
		if err := node.etcdManager.WatchNodes(ctx); err != nil {
			node.logger.Errorf("Failed to start watching nodes: %v", err)
			return fmt.Errorf("failed to start watching nodes: %w", err)
		}
	}

	return node.connectToInspector()
}

func (node *Node) IsStarted() bool {
	return !node.startupTime.IsZero()
}

// Stop stops the node and unregisters it from the inspector
func (node *Node) Stop(ctx context.Context) error {
	node.logger.Infof("Node stopping")

	// Unregister from etcd
	if node.etcdManager != nil {
		if err := node.etcdManager.UnregisterNode(ctx, node.advertiseAddress); err != nil {
			node.logger.Errorf("Failed to unregister node from etcd: %v", err)
		}

		// Close etcd connection
		if err := node.etcdManager.Close(); err != nil {
			node.logger.Errorf("Failed to close etcd connection: %v", err)
		}
	}

	return node.unregisterFromInspector()
}

// Connect to inspector service at localhost:8081
func (node *Node) connectToInspector() error {
	conn, err := grpc.NewClient("localhost:8081", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to inspector service: %w", err)
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
		return fmt.Errorf("failed to register node with inspector: %w", err)
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
		return fmt.Errorf("failed to unregister node from inspector: %w", err)
	}

	node.logger.Infof("Successfully unregistered node %s from inspector", node.advertiseAddress)
	return nil
}

// String returns a string representation of the node
func (node *Node) String() string {
	return fmt.Sprintf("Node@%s", node.advertiseAddress)
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
func (node *Node) RegisterClient() (ClientObject, error) {
	// Placeholder for client registration logic
	clientObj, err := node.newClientObject()
	if err != nil {
		return nil, err
	}

	return clientObj.(ClientObject), nil
}

func (node *Node) newClientObject() (Object, error) {
	if node.clientObjectType == "" {
		return nil, fmt.Errorf("client object type not registered")
	}

	clientId := node.advertiseAddress + "/" + uniqueid.UniqueId()
	clientObj, err := node.createObject(node.clientObjectType, clientId, nil)
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

	resp, err := node.CallObject(ctx, clientId, method, request)
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
func (node *Node) CallObject(ctx context.Context, id string, method string, request proto.Message) (proto.Message, error) {
	node.logger.Infof("CallObject received: id=%s, method=%s", id, method)
	node.objectsMu.RLock()
	obj, ok := node.objects[id]
	node.objectsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("object not found: %s", id)
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
func (node *Node) CreateObject(ctx context.Context, typ string, id string, initData proto.Message) (string, error) {
	node.logger.Infof("CreateObject received: type=%s", typ)
	obj, err := node.createObject(typ, id, initData)
	if err != nil {
		return "", err
	}
	return obj.Id(), nil
}

func (node *Node) createObject(typ string, id string, initData proto.Message) (Object, error) {
	node.objectTypesMu.RLock()
	objectType, ok := node.objectTypes[typ]
	node.objectTypesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown object type: %s", typ)
	}

	// Create a new instance of the object
	objectValue := reflect.New(objectType)
	object, ok := objectValue.Interface().(Object)
	if !ok {
		return nil, fmt.Errorf("type %s does not implement Object interface", typ)
	}

	object.OnInit(object, id, initData)
	id = object.Id()
	node.objectsMu.Lock()
	node.objects[id] = object
	node.objectsMu.Unlock()
	node.logger.Infof("Created object %s of type %s", id, typ)
	object.OnCreated()

	if node.IsStarted() {
		err := node.registerObjectWithInspector(object)
		if err != nil && !errors.Is(err, context.Canceled) {
			node.logger.Errorf("Failed to register object with inspector: %v", err)
		}
	}

	return object, nil
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

func (node *Node) registerObjectWithInspector(object Object) error {
	if node.inspectorClient == nil {
		return fmt.Errorf("inspector client not initialized")
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
		return fmt.Errorf("failed to register object with inspector: %w", err)
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

// GetEtcdManager returns the etcd manager
func (node *Node) GetEtcdManager() *etcdmanager.EtcdManager {
	return node.etcdManager
}

// GetNodes returns a list of all registered nodes
func (node *Node) GetNodes() []string {
	if node.etcdManager == nil {
		return []string{}
	}
	return node.etcdManager.GetNodes()
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
