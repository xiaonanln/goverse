package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	client_pb "github.com/simonlingoogle/pulse/client/proto"
	"github.com/simonlingoogle/pulse/cluster"
	"github.com/simonlingoogle/pulse/node"
	"github.com/simonlingoogle/pulse/util/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	pulse_pb "github.com/simonlingoogle/pulse/proto"
)

type ServerConfig struct {
	ListenAddress       string
	AdvertiseAddress    string
	ClientListenAddress string
	EtcdAddress         string
}

type Server struct {
	pulse_pb.UnimplementedPulseServer
	client_pb.UnimplementedClientServiceServer
	config *ServerConfig
	Node   *node.Node
	ctx    context.Context
	cancel context.CancelFunc
	logger *logger.Logger
}

func NewServer(config *ServerConfig) *Server {
	if err := validateServerConfig(config); err != nil {
		log.Fatalf("Invalid server configuration: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		config: config,
		Node:   node.NewNodeWithEtcd(config.AdvertiseAddress, config.EtcdAddress),
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewLogger(fmt.Sprintf("Server(%s)", config.AdvertiseAddress)),
	}
	cluster.Get().SetThisNode(server.Node)
	return server
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

	pulseServiceListener, err := net.Listen("tcp", server.config.ListenAddress)
	if err != nil {
		return err
	}
	defer pulseServiceListener.Close()

	clientServiceListener, err := net.Listen("tcp", server.config.ClientListenAddress)
	if err != nil {
		return err
	}
	defer clientServiceListener.Close()

	node := server.Node

	grpcServer := grpc.NewServer()
	pulse_pb.RegisterPulseServer(grpcServer, server)
	client_pb.RegisterClientServiceServer(grpcServer, server)
	reflection.Register(grpcServer)
	server.logger.Infof("gRPC server listening on %s", pulseServiceListener.Addr().String())

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if err := node.Start(server.ctx); err != nil {
		server.logger.Errorf("Failed to start node: %v", err)
		return err
	}

	go func() {
		<-sigChan
		server.logger.Infof("Received shutdown signal, stopping gRPC server...")
		grpcServer.GracefulStop()
	}()

	go func() {
		server.logger.Infof("Client gRPC server listening on %s", clientServiceListener.Addr().String())
		clientGrpcServer := grpc.NewServer()
		client_pb.RegisterClientServiceServer(clientGrpcServer, server)
		reflection.Register(clientGrpcServer)
		if err := clientGrpcServer.Serve(clientServiceListener); err != nil {
			server.logger.Errorf("Client gRPC server error: %v", err)
		}
		server.logger.Infof("Client gRPC server stopped")
	}()

	err = grpcServer.Serve(pulseServiceListener)
	if err != nil {
		server.logger.Errorf("gRPC server error: %v", err)
	}

	err = node.Stop(server.ctx)
	if err != nil {
		server.logger.Errorf("Failed to stop node: %v", err)
	}

	server.logger.Infof("gRPC server stopped")
	return err
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
	client, err := server.Node.RegisterClient()
	if err != nil {
		return fmt.Errorf("failed to register client: %v", err)
	}
	defer server.Node.UnregisterClient(client.Id())

	resp := client_pb.RegisterResponse{
		ClientId: client.Id(),
	}
	if err := sendMessageToStream(stream, &resp); err != nil {
		return fmt.Errorf("failed to send RegisterResponse: %v", err)
	}

	for msg := range client.MessageChan() {
		// Convert message to client_pb format as needed
		if err := sendMessageToStream(stream, msg); err != nil {
			return fmt.Errorf("failed to send message to client %s: %v", client.Id(), err)
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

func (server *Server) CallObject(ctx context.Context, req *pulse_pb.CallObjectRequest) (*pulse_pb.CallObjectResponse, error) {
	server.logRPC("CallObject", req)

	resp, err := server.Node.CallObject(ctx, req.GetId(), req.GetMethod(), req.GetRequest())
	if err != nil {
		return nil, err
	}

	var respAny anypb.Any
	err = respAny.MarshalFrom(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	response := &pulse_pb.CallObjectResponse{
		Response: &respAny,
	}
	return response, nil
}

func (server *Server) CreateObject(ctx context.Context, req *pulse_pb.CreateObjectRequest) (*pulse_pb.CreateObjectResponse, error) {
	server.logRPC("CreateObject", req)

	id, err := server.Node.CreateObject(ctx, req.GetType(), "", req.GetInitData())
	if err != nil {
		return nil, err
	}
	response := &pulse_pb.CreateObjectResponse{
		Id: id,
	}
	return response, nil
}

func (server *Server) Status(ctx context.Context, req *pulse_pb.Empty) (*pulse_pb.StatusResponse, error) {
	server.logRPC("Status", req)

	response := &pulse_pb.StatusResponse{
		AdvertiseAddr: server.config.AdvertiseAddress,
		NumObjects:    int64(server.Node.NumObjects()),
		UptimeSeconds: server.Node.UptimeSeconds(),
	}
	return response, nil
}

func (server *Server) ListObjects(ctx context.Context, req *pulse_pb.Empty) (*pulse_pb.ListObjectsResponse, error) {
	server.logRPC("ListObjects", req)

	objects := server.Node.ListObjects()
	objectInfos := make([]*pulse_pb.ObjectInfo, 0, len(objects))
	for _, obj := range objects {
		objectInfos = append(objectInfos, &pulse_pb.ObjectInfo{
			Type:         obj.Type,
			Id:           obj.Id,
			CreationTime: obj.CreationTime.Unix(),
		})
	}

	response := &pulse_pb.ListObjectsResponse{
		Objects: objectInfos,
	}
	return response, nil
}
