package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	client_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	goverse_pb "github.com/xiaonanln/goverse/proto"
)

type ServerConfig struct {
	ListenAddress           string
	AdvertiseAddress        string
	ClientListenAddress     string
	EtcdAddress             string
	EtcdPrefix              string        // Optional: etcd key prefix for this cluster (default: "/goverse")
	MinQuorum               int           // Optional: minimal number of nodes required for cluster to be considered stable (default: 1)
	NodeStabilityDuration   time.Duration // Optional: duration to wait for cluster state to stabilize before updating shard mapping (default: 10s)
}

type Server struct {
	goverse_pb.UnimplementedGoverseServer
	client_pb.UnimplementedClientServiceServer
	config  *ServerConfig
	Node    *node.Node
	ctx     context.Context
	cancel  context.CancelFunc
	logger  *logger.Logger
	cluster *cluster.Cluster
}

func NewServer(config *ServerConfig) (*Server, error) {
	if err := validateServerConfig(config); err != nil {
		return nil, fmt.Errorf("invalid server configuration: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	node := node.NewNode(config.AdvertiseAddress)

	// Initialize cluster with etcd connection
	c, err := cluster.NewCluster(node, config.EtcdAddress, config.EtcdPrefix)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize cluster: %w", err)
	}

	cluster.SetThis(c)

	// Set minimum quorum configuration
	if config.MinQuorum > 0 {
		c.SetMinQuorum(config.MinQuorum)
	}

	// Set node stability duration configuration
	if config.NodeStabilityDuration > 0 {
		c.SetNodeStabilityDuration(config.NodeStabilityDuration)
	}

	server := &Server{
		config:  config,
		Node:    node,
		ctx:     ctx,
		cancel:  cancel,
		logger:  logger.NewLogger(fmt.Sprintf("Server(%s)", config.AdvertiseAddress)),
		cluster: c,
	}

	return server, nil
}

func validateServerConfig(config *ServerConfig) error {
	// Add validation logic as needed
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.ListenAddress == "" {
		return fmt.Errorf("ListenAddress cannot be empty")
	}
	if config.AdvertiseAddress == "" {
		return fmt.Errorf("AdvertiseAddress cannot be empty")
	}

	// Set default etcd address if not provided
	if config.EtcdAddress == "" {
		config.EtcdAddress = "localhost:2379"
	}

	return nil
}

func (server *Server) Run() error {
	// Ensure context is canceled when the server stops
	defer server.cancel()

	goverseServiceListener, err := net.Listen("tcp", server.config.ListenAddress)
	if err != nil {
		return err
	}
	defer goverseServiceListener.Close()

	clientServiceListener, err := net.Listen("tcp", server.config.ClientListenAddress)
	if err != nil {
		return err
	}
	defer clientServiceListener.Close()

	node := server.Node

	grpcServer := grpc.NewServer()
	goverse_pb.RegisterGoverseServer(grpcServer, server)
	client_pb.RegisterClientServiceServer(grpcServer, server)
	reflection.Register(grpcServer)
	server.logger.Infof("gRPC server listening on %s", goverseServiceListener.Addr().String())

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := node.Start(server.ctx); err != nil {
		server.logger.Errorf("Failed to start node: %v", err)
		return err
	}

	// Set this node on the cluster, and start cluster services
	server.cluster.Start(server.ctx, server.Node)

	// Handle both signals and context cancellation for graceful shutdown
	clientGrpcServer := grpc.NewServer()
	go func() {
		select {
		case <-sigChan:
			server.logger.Infof("Received shutdown signal, stopping gRPC servers...")
		case <-server.ctx.Done():
			server.logger.Infof("Context cancelled, stopping gRPC servers...")
		}
		grpcServer.GracefulStop()
		clientGrpcServer.GracefulStop()
	}()

	go func() {
		server.logger.Infof("Client gRPC server listening on %s", clientServiceListener.Addr().String())
		client_pb.RegisterClientServiceServer(clientGrpcServer, server)
		reflection.Register(clientGrpcServer)
		if err := clientGrpcServer.Serve(clientServiceListener); err != nil {
			server.logger.Errorf("Client gRPC server error: %v", err)
		}
		server.logger.Infof("Client gRPC server stopped")
	}()

	err = grpcServer.Serve(goverseServiceListener)
	if err != nil {
		server.logger.Errorf("gRPC server error: %v", err)
	}

	err = server.cluster.Stop(server.ctx)
	if err != nil {
		server.logger.Errorf("Failed to stop cluster: %v", err)
	}

	err = node.Stop(server.ctx)
	if err != nil {
		server.logger.Errorf("Failed to stop node: %v", err)
	}

	server.logger.Infof("gRPC server stopped")
	return nil
}

func (server *Server) logRPC(method string, req proto.Message) {
	server.logger.Infof("RPC <<< %s(request=%v)", method, req)
}

// Helper function to send a proto.Message to the stream
func sendMessageToStream(stream client_pb.ClientService_RegisterServer, msg proto.Message) error {
	var anyMsg anypb.Any
	if err := anyMsg.MarshalFrom(msg); err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}
	if err := stream.Send(&anyMsg); err != nil {
		return fmt.Errorf("failed to send message to stream: %v", err)
	}
	return nil
}

// Register implements the ClientService Register method for client registration
func (server *Server) Register(req *client_pb.Empty, stream client_pb.ClientService_RegisterServer) error {
	server.logRPC("Register", req)
	clientId, messageChan, err := server.Node.RegisterClient(stream.Context())
	if err != nil {
		return fmt.Errorf("failed to register client: %v", err)
	}
	defer server.Node.UnregisterClient(clientId)

	resp := client_pb.RegisterResponse{
		ClientId: clientId,
	}
	if err := sendMessageToStream(stream, &resp); err != nil {
		return fmt.Errorf("failed to send RegisterResponse: %v", err)
	}

	for msg := range messageChan {
		// Convert message to client_pb format as needed
		if err := sendMessageToStream(stream, msg); err != nil {
			return fmt.Errorf("failed to send message to client %s: %v", clientId, err)
		}
	}
	return nil
}

// Call implements the ClientService Call method for client RPC calls
func (server *Server) Call(ctx context.Context, req *client_pb.CallRequest) (*client_pb.CallResponse, error) {
	server.logRPC("Call", req)
	resp, err := server.Node.CallClient(ctx, req.GetClientId(), req.GetMethod(), req.GetRequest())
	if err != nil {
		return nil, err
	}

	response := &client_pb.CallResponse{
		Response: resp,
	}
	return response, nil
}

func (server *Server) CallObject(ctx context.Context, req *goverse_pb.CallObjectRequest) (*goverse_pb.CallObjectResponse, error) {
	server.logRPC("CallObject", req)

	// Unmarshal the Any request to concrete proto.Message
	var requestMsg proto.Message
	var err error
	if req.Request != nil {
		requestMsg, err = req.Request.UnmarshalNew()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %w", err)
		}
	}

	resp, err := server.Node.CallObject(ctx, req.GetType(), req.GetId(), req.GetMethod(), requestMsg)
	if err != nil {
		return nil, err
	}

	var respAny anypb.Any
	err = respAny.MarshalFrom(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	response := &goverse_pb.CallObjectResponse{
		Response: &respAny,
	}
	return response, nil
}

func (server *Server) CreateObject(ctx context.Context, req *goverse_pb.CreateObjectRequest) (*goverse_pb.CreateObjectResponse, error) {
	server.logRPC("CreateObject", req)

	// ID must be specified in the request
	if req.GetId() == "" {
		return nil, fmt.Errorf("object ID must be specified in CreateObject request")
	}

	// Check with cluster that this ID is sharded to this node
	clusterInstance := cluster.This()
	if clusterInstance != nil && clusterInstance.GetThisNode() != nil {
		targetNode, err := clusterInstance.GetNodeForObject(ctx, req.GetId())
		if err != nil {
			return nil, fmt.Errorf("failed to determine target node for object %s: %w", req.GetId(), err)
		}

		thisNodeAddr := clusterInstance.GetThisNode().GetAdvertiseAddress()
		if targetNode != thisNodeAddr {
			return nil, fmt.Errorf("object %s is sharded to node %s, not this node %s", req.GetId(), targetNode, thisNodeAddr)
		}
	}

	id, err := server.Node.CreateObject(ctx, req.GetType(), req.GetId())
	if err != nil {
		server.logger.Errorf("CreateObject failed: %v", err)
		return nil, err
	}
	response := &goverse_pb.CreateObjectResponse{
		Id: id,
	}
	return response, nil
}

func (server *Server) DeleteObject(ctx context.Context, req *goverse_pb.DeleteObjectRequest) (*goverse_pb.DeleteObjectResponse, error) {
	server.logRPC("DeleteObject", req)

	// ID must be specified in the request
	if req.GetId() == "" {
		return nil, fmt.Errorf("object ID must be specified in DeleteObject request")
	}

	// Check with cluster that this ID is sharded to this node
	clusterInstance := cluster.This()
	if clusterInstance != nil && clusterInstance.GetThisNode() != nil {
		targetNode, err := clusterInstance.GetNodeForObject(ctx, req.GetId())
		if err != nil {
			return nil, fmt.Errorf("failed to determine target node for object %s: %w", req.GetId(), err)
		}

		thisNodeAddr := clusterInstance.GetThisNode().GetAdvertiseAddress()
		if targetNode != thisNodeAddr {
			return nil, fmt.Errorf("object %s is sharded to node %s, not this node %s", req.GetId(), targetNode, thisNodeAddr)
		}
	}

	err := server.Node.DeleteObject(ctx, req.GetId())
	if err != nil {
		server.logger.Errorf("DeleteObject failed: %v", err)
		return nil, err
	}
	response := &goverse_pb.DeleteObjectResponse{}
	return response, nil
}

func (server *Server) Status(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.StatusResponse, error) {
	server.logRPC("Status", req)

	response := &goverse_pb.StatusResponse{
		AdvertiseAddr: server.config.AdvertiseAddress,
		NumObjects:    int64(server.Node.NumObjects()),
		UptimeSeconds: server.Node.UptimeSeconds(),
	}
	return response, nil
}

func (server *Server) ListObjects(ctx context.Context, req *goverse_pb.Empty) (*goverse_pb.ListObjectsResponse, error) {
	server.logRPC("ListObjects", req)

	objects := server.Node.ListObjects()
	objectInfos := make([]*goverse_pb.ObjectInfo, 0, len(objects))
	for _, obj := range objects {
		objectInfos = append(objectInfos, &goverse_pb.ObjectInfo{
			Type:         obj.Type,
			Id:           obj.Id,
			CreationTime: obj.CreationTime.Unix(),
		})
	}

	response := &goverse_pb.ListObjectsResponse{
		Objects: objectInfos,
	}
	return response, nil
}

func (server *Server) PushMessageToClient(ctx context.Context, req *goverse_pb.PushMessageToClientRequest) (*goverse_pb.PushMessageToClientResponse, error) {
	server.logRPC("PushMessageToClient", req)

	// Unmarshal the Any message to concrete proto.Message
	var message proto.Message
	var err error
	if req.Message != nil {
		message, err = req.Message.UnmarshalNew()
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", err)
		}
	}

	// Push the message to the client on this node
	err = server.Node.PushMessageToClient(req.GetClientId(), message)
	if err != nil {
		return nil, err
	}

	return &goverse_pb.PushMessageToClientResponse{}, nil
}
