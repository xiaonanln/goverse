package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xiaonanln/goverse/cluster"
	"github.com/xiaonanln/goverse/cluster/sharding"
	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/callcontext"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	goverse_pb "github.com/xiaonanln/goverse/proto"
)

type ServerConfig struct {
	ListenAddress             string
	AdvertiseAddress          string
	MetricsListenAddress      string // Optional: HTTP address for Prometheus metrics and pprof (e.g., ":9090")
	EtcdAddress               string
	EtcdPrefix                string                        // Optional: etcd key prefix for this cluster (default: "/goverse")
	MinQuorum                 int                           // Optional: minimal number of nodes required for cluster to be considered stable (default: 1)
	NodeStabilityDuration     time.Duration                 // Optional: duration to wait for cluster state to stabilize before updating shard mapping (default: 10s)
	ShardMappingCheckInterval time.Duration                 // Optional: how often to check if shard mapping needs updating (default: 5s)
	NumShards                 int                           // Optional: number of shards in the cluster (default: 8192)
	InspectorAddress          string                        // Optional: address of the inspector service (if empty, inspector is disabled)
	AccessValidator           *config.AccessValidator       // Optional: access validator for node access control
	LifecycleValidator        *config.LifecycleValidator    // Optional: lifecycle validator for CREATE/DELETE control
	AutoLoadObjects           []config.AutoLoadObjectConfig // Optional: objects to auto-load when node starts and claims shards
	ConfigFile                *config.Config                // Optional: loaded config file (if config file was used)
}

type Server struct {
	goverse_pb.UnimplementedGoverseServer
	config          *ServerConfig
	Node            *node.Node
	ctx             context.Context
	cancel          context.CancelFunc
	logger          *logger.Logger
	cluster         *cluster.Cluster
	accessValidator *config.AccessValidator
}

func NewServer(config *ServerConfig) (*Server, error) {
	if err := validateServerConfig(config); err != nil {
		return nil, fmt.Errorf("invalid server configuration: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	// Determine number of shards (default to 8192 if not specified)
	numShards := config.NumShards
	if numShards <= 0 {
		numShards = 8192
	}

	n := node.NewNode(config.AdvertiseAddress, numShards)

	// Set inspector address on the node if configured
	if config.InspectorAddress != "" {
		n.SetInspectorAddress(config.InspectorAddress)
	}

	// Set lifecycle validator on the node if configured
	if config.LifecycleValidator != nil {
		n.SetLifecycleValidator(config.LifecycleValidator)
	}

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
	if config.NumShards > 0 {
		clusterCfg.NumShards = config.NumShards
	}
	if len(config.AutoLoadObjects) > 0 {
		clusterCfg.AutoLoadObjects = config.AutoLoadObjects
	}
	if config.ConfigFile != nil {
		clusterCfg.ConfigFile = config.ConfigFile
	}

	// Initialize cluster with etcd connection
	c, err := cluster.NewClusterWithNode(clusterCfg, n)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize cluster: %w", err)
	}

	cluster.SetThis(c)

	server := &Server{
		config:          config,
		Node:            n,
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger.NewLogger(fmt.Sprintf("Server(%s)", config.AdvertiseAddress)),
		cluster:         c,
		accessValidator: config.AccessValidator,
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

func (server *Server) Run(ctx context.Context) error {
	// Ensure context is canceled when the server stops
	defer server.cancel()

	goverseServiceListener, err := net.Listen("tcp", server.config.ListenAddress)
	if err != nil {
		return err
	}
	defer goverseServiceListener.Close()

	node := server.Node

	grpcServer := grpc.NewServer()
	goverse_pb.RegisterGoverseServer(grpcServer, server)
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
		mux := http.NewServeMux()

		// Prometheus metrics endpoint
		mux.Handle("/metrics", promhttp.Handler())

		// Add pprof endpoints (always enabled when HTTP server is enabled)
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		// Add handlers for named profiles that pprof.Index shows
		mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
		mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
		mux.Handle("/debug/pprof/block", pprof.Handler("block"))
		mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
		mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
		server.logger.Infof("pprof endpoints enabled at /debug/pprof/")

		metricsServer = &http.Server{
			Addr:              server.config.MetricsListenAddress,
			Handler:           mux,
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

	// Handle signals, context cancellation, or passed-in context for graceful shutdown
	go func() {
		select {
		case <-sigChan:
			server.logger.Infof("Received shutdown signal, stopping servers...")
		case <-server.ctx.Done():
			server.logger.Infof("Context cancelled, stopping servers...")
		case <-ctx.Done():
			server.logger.Infof("Run context done, stopping servers...")
		}
		grpcServer.GracefulStop()
		if metricsServer != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				server.logger.Errorf("Metrics server shutdown error: %v", err)
			}
		}
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

func (server *Server) CallObject(ctx context.Context, req *goverse_pb.CallObjectRequest) (*goverse_pb.CallObjectResponse, error) {
	server.logRPC("CallObject", req)

	// Validate that this object should be on this node
	if err := server.validateObjectShardOwnership(ctx, req.GetId()); err != nil {
		return nil, err
	}

	// Check node access if access validator is configured.
	// This validates access using node-level rules (ALLOW or INTERNAL).
	// Defense in depth: Both gates and nodes validate access. Gates check client
	// access (ALLOW/EXTERNAL), nodes check node access (ALLOW/INTERNAL).
	// A request that passes gate validation might be denied here if the access
	// level is EXTERNAL (client-only). This ensures consistent enforcement.
	if server.accessValidator != nil {
		if err := server.accessValidator.CheckNodeAccess(req.GetType(), req.GetId(), req.GetMethod()); err != nil {
			server.logger.Warnf("Access denied for node call: type=%s, id=%s, method=%s: %v", req.GetType(), req.GetId(), req.GetMethod(), err)
			return nil, status.Errorf(codes.PermissionDenied, "%v", err)
		}
	}

	// Inject client_id into context if present in the request
	if req.ClientId != "" {
		ctx = callcontext.WithClientID(ctx, req.ClientId)
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

	// Record metrics on success
	numShards := server.config.NumShards
	if numShards <= 0 {
		numShards = 8192
	}
	shardID := sharding.GetShardID(req.GetId(), numShards)
	metrics.RecordObjectCreated(server.Node.GetAdvertiseAddress(), req.GetType(), shardID)

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

	// Record metrics on success (we don't have object type here, so skip metrics)

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

func (server *Server) RegisterGate(req *goverse_pb.RegisterGateRequest, stream goverse_pb.Goverse_RegisterGateServer) error {
	server.logRPC("RegisterGate", req)

	gateAddr := req.GetGateAddr()
	if gateAddr == "" {
		return fmt.Errorf("gate_addr must be specified in RegisterGate request")
	}

	server.logger.Infof("Gate %s registered, starting message stream", gateAddr)

	// Send RegisterGateResponse as the first message
	registerResponse := &goverse_pb.GateMessage{
		Message: &goverse_pb.GateMessage_RegisterGateResponse{
			RegisterGateResponse: &goverse_pb.RegisterGateResponse{},
		},
	}
	if err := stream.Send(registerResponse); err != nil {
		server.logger.Errorf("Failed to send RegisterGateResponse to gate %s: %v", gateAddr, err)
		return err
	}

	cluster := server.cluster
	// Acquire message channel from cluster
	msgChan, err := cluster.RegisterGateConnection(gateAddr)
	if err != nil {
		server.logger.Errorf("Failed to register gate connection for %s: %v", gateAddr, err)
		return err
	}
	defer cluster.UnregisterGateConnection(gateAddr, msgChan)

	// Loop to forward messages from channel to stream
	for {
		select {
		case <-stream.Context().Done():
			server.logger.Infof("Gate %s stream closed: %v", gateAddr, stream.Context().Err())
			return nil
		case msg, ok := <-msgChan:
			if !ok {
				server.logger.Infof("Gate %s message channel closed, stopping stream", gateAddr)
				return nil
			}
			
			var gateMsg *goverse_pb.GateMessage
			
			// Handle both ClientMessageEnvelope and BroadcastMessageEnvelope
			switch m := msg.(type) {
			case *goverse_pb.ClientMessageEnvelope:
				gateMsg = &goverse_pb.GateMessage{
					Message: &goverse_pb.GateMessage_ClientMessage{
						ClientMessage: m,
					},
				}
			case *goverse_pb.BroadcastMessageEnvelope:
				gateMsg = &goverse_pb.GateMessage{
					Message: &goverse_pb.GateMessage_BroadcastMessage{
						BroadcastMessage: m,
					},
				}
			default:
				server.logger.Errorf("Received unknown message type for gate %s: %T", gateAddr, msg)
				continue
			}

			if err := stream.Send(gateMsg); err != nil {
				server.logger.Errorf("Failed to send message to gate %s: %v", gateAddr, err)
				return err
			}
		}
	}
}

func (server *Server) ReliableCallObject(ctx context.Context, req *goverse_pb.ReliableCallObjectRequest) (*goverse_pb.ReliableCallObjectResponse, error) {
	server.logRPC("ReliableCallObject", req)

	// Validate request parameters - require the new fields
	// Return SKIPPED status for validation errors since method was never executed
	if req.GetCallId() == "" {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  "call_id must be specified in ReliableCallObject request",
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}
	if req.GetObjectType() == "" {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  "object_type must be specified in ReliableCallObject request",
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}
	if req.GetObjectId() == "" {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  "object_id must be specified in ReliableCallObject request",
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}
	if req.GetMethodName() == "" {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  "method_name must be specified in ReliableCallObject request",
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}
	if req.GetRequest() == nil {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  "request must be specified in ReliableCallObject request",
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}

	// Validate that this object should be on this node
	if err := server.validateObjectShardOwnership(ctx, req.GetObjectId()); err != nil {
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  err.Error(),
			Status: goverse_pb.ReliableCallStatus_SKIPPED,
		}, nil
	}

	// Route to node handler with new parameters
	resultAny, status, err := server.Node.ReliableCallObject(
		ctx,
		req.GetCallId(),
		req.GetObjectType(),
		req.GetObjectId(),
		req.GetMethodName(),
		req.GetRequest(),
	)
	if err != nil {
		// Return error with status information
		return &goverse_pb.ReliableCallObjectResponse{
			Result: nil,
			Error:  err.Error(),
			Status: status,
		}, nil
	}

	response := &goverse_pb.ReliableCallObjectResponse{
		Result: resultAny,
		Error:  "",
		Status: status,
	}
	return response, nil
}
