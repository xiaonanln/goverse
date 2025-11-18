package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	gateway_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	goverse_pb "github.com/xiaonanln/goverse/proto"
)

type ServerConfig struct {
	ListenAddress             string
	AdvertiseAddress          string
	ClientListenAddress       string
	MetricsListenAddress      string // Optional: HTTP address for Prometheus metrics (e.g., ":9090")
	EtcdAddress               string
	EtcdPrefix                string        // Optional: etcd key prefix for this cluster (default: "/goverse")
	MinQuorum                 int           // Optional: minimal number of nodes required for cluster to be considered stable (default: 1)
	NodeStabilityDuration     time.Duration // Optional: duration to wait for cluster state to stabilize before updating shard mapping (default: 10s)
	ShardMappingCheckInterval time.Duration // Optional: how often to check if shard mapping needs updating (default: 5s)
}

type Server struct {
	goverse_pb.UnimplementedGoverseServer
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

	// Create cluster configuration from server config
	clusterCfg := cluster.DefaultConfig()
	clusterCfg.EtcdAddress = config.EtcdAddress
	clusterCfg.EtcdPrefix = config.EtcdPrefix

	// Override defaults with server config values if provided
	if config.MinQuorum > 0 {
		clusterCfg.MinQuorum = config.MinQuorum
	}
	if config.NodeStabilityDuration > 0 {
		clusterCfg.ClusterStateStabilityDuration = config.NodeStabilityDuration
	}
	if config.ShardMappingCheckInterval > 0 {
		clusterCfg.ShardMappingCheckInterval = config.ShardMappingCheckInterval
	}

	// Initialize cluster with etcd connection
	c, err := cluster.NewCluster(clusterCfg, node)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize cluster: %w", err)
	}

	cluster.SetThis(c)

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
	gateway_pb.RegisterGatewayServiceServer(grpcServer, &gatewayServiceImpl{server: server})
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

	// Start metrics HTTP server if configured
	var metricsServer *http.Server
	if server.config.MetricsListenAddress != "" {
		metricsServer = &http.Server{
			Addr:              server.config.MetricsListenAddress,
			Handler:           promhttp.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			server.logger.Infof("Metrics HTTP server listening on %s", server.config.MetricsListenAddress)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				server.logger.Errorf("Metrics HTTP server error: %v", err)
			}
			server.logger.Infof("Metrics HTTP server stopped")
		}()
	}

	// Handle both signals and context cancellation for graceful shutdown
	clientGrpcServer := grpc.NewServer()
	go func() {
		select {
		case <-sigChan:
			server.logger.Infof("Received shutdown signal, stopping servers...")
		case <-server.ctx.Done():
			server.logger.Infof("Context cancelled, stopping servers...")
		}
		grpcServer.GracefulStop()
		clientGrpcServer.GracefulStop()
		if metricsServer != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				server.logger.Errorf("Metrics server shutdown error: %v", err)
			}
		}
	}()

	go func() {
		server.logger.Infof("Client gRPC server listening on %s", clientServiceListener.Addr().String())
		gateway_pb.RegisterGatewayServiceServer(clientGrpcServer, &gatewayServiceImpl{server: server})
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
func sendMessageToStream(stream gateway_pb.GatewayService_RegisterServer, msg proto.Message) error {
	var anyMsg anypb.Any
	if err := anyMsg.MarshalFrom(msg); err != nil {
		return fmt.Errorf("failed to marshal message: %v", err)
	}
	if err := stream.Send(&anyMsg); err != nil {
		return fmt.Errorf("failed to send message to stream: %v", err)
	}
	return nil
}

// validateObjectShardOwnership validates that an object should be on this node
// by checking both the routing result and the shard mapping's TargetNode.
// This prevents race conditions where operations arrive during shard transitions.
func (server *Server) validateObjectShardOwnership(ctx context.Context, objectID string) error {
	clusterInstance := cluster.This()
	if clusterInstance == nil || clusterInstance.GetThisNode() == nil {
		return nil // No cluster configured, allow operation
	}

	// Check with cluster that this ID is sharded to this node
	targetNode, err := clusterInstance.GetCurrentNodeForObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("failed to determine target node for object %s: %w", objectID, err)
	}

	thisNodeAddr := clusterInstance.GetThisNode().GetAdvertiseAddress()
	if targetNode != thisNodeAddr {
		return fmt.Errorf("object %s is sharded to node %s, not this node %s", objectID, targetNode, thisNodeAddr)
	}

	// GetCurrentNodeForObject already validates that shards are not in migration state
	// (i.e., TargetNode == CurrentNode), so no additional validation needed here

	return nil
}

// Register implements the GatewayService Register method for client registration
func (server *Server) registerImpl(req *gateway_pb.Empty, stream gateway_pb.GatewayService_RegisterServer) error {
	server.logRPC("Register", req)
	clientId, messageChan, err := server.Node.RegisterClient(stream.Context())
	if err != nil {
		return fmt.Errorf("failed to register client: %v", err)
	}

	// Record client connection
	metrics.RecordClientConnected(server.config.AdvertiseAddress, "grpc")

	defer func() {
		server.Node.UnregisterClient(server.ctx, clientId)
		// Record client disconnection
		metrics.RecordClientDisconnected(server.config.AdvertiseAddress, "grpc")
	}()

	resp := gateway_pb.RegisterResponse{
		ClientId: clientId,
	}
	if err := sendMessageToStream(stream, &resp); err != nil {
		return fmt.Errorf("failed to send RegisterResponse: %v", err)
	}

	for msg := range messageChan {
		// Convert message to gateway_pb format as needed
		if err := sendMessageToStream(stream, msg); err != nil {
			return fmt.Errorf("failed to send message to client %s: %v", clientId, err)
		}
	}
	return nil
}

// gatewayServiceImpl wraps Server to implement GatewayService methods
// This separate type is needed because Go doesn't support method overloading,
// and both GatewayService and Goverse service have CreateObject/DeleteObject methods
// with different parameter types
type gatewayServiceImpl struct {
	gateway_pb.UnimplementedGatewayServiceServer
	server *Server
}

// Register implements the GatewayService Register method
func (g *gatewayServiceImpl) Register(req *gateway_pb.Empty, stream gateway_pb.GatewayService_RegisterServer) error {
	return g.server.registerImpl(req, stream)
}

// Call implements the GatewayService Call method for client RPC calls
func (g *gatewayServiceImpl) Call(ctx context.Context, req *gateway_pb.CallObjectRequest) (*gateway_pb.CallResponse, error) {
	g.server.logRPC("Call", req)
	resp, err := g.server.Node.CallClient(ctx, req.GetClientId(), req.GetMethod(), req.GetRequest())
	if err != nil {
		return nil, err
	}

	response := &gateway_pb.CallResponse{
		Response: resp,
	}
	return response, nil
}

// CreateObject implements the GatewayService CreateObject method (empty implementation)
func (g *gatewayServiceImpl) CreateObject(ctx context.Context, req *gateway_pb.CreateObjectRequest) (*gateway_pb.CreateObjectResponse, error) {
	g.server.logRPC("Gateway.CreateObject", req)
	// Empty implementation - to be filled in later
	return &gateway_pb.CreateObjectResponse{}, nil
}

// DeleteObject implements the GatewayService DeleteObject method (empty implementation)
func (g *gatewayServiceImpl) DeleteObject(ctx context.Context, req *gateway_pb.DeleteObjectRequest) (*gateway_pb.DeleteObjectResponse, error) {
	g.server.logRPC("Gateway.DeleteObject", req)
	// Empty implementation - to be filled in later
	return &gateway_pb.DeleteObjectResponse{}, nil
}

func (server *Server) CallObject(ctx context.Context, req *goverse_pb.CallObjectRequest) (*goverse_pb.CallObjectResponse, error) {
	server.logRPC("CallObject", req)

	// Validate that this object should be on this node
	if err := server.validateObjectShardOwnership(ctx, req.GetId()); err != nil {
		return nil, err
	}

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

	// Validate that this object should be on this node
	if err := server.validateObjectShardOwnership(ctx, req.GetId()); err != nil {
		return nil, err
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
		targetNode, err := clusterInstance.GetCurrentNodeForObject(ctx, req.GetId())
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
