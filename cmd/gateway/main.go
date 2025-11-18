package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/anypb"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
)

// gatewayServer implements the GatewayService with empty handlers
type gatewayServer struct {
	gateway_pb.UnimplementedGatewayServiceServer
}

// Register implements the Register RPC (empty for now)
func (s *gatewayServer) Register(req *gateway_pb.Empty, stream grpc.ServerStreamingServer[anypb.Any]) error {
	log.Println("Register called (not implemented)")
	return nil
}

// CallObject implements the CallObject RPC (empty for now)
func (s *gatewayServer) CallObject(ctx context.Context, req *gateway_pb.CallObjectRequest) (*gateway_pb.CallObjectResponse, error) {
	log.Println("CallObject called (not implemented)")
	return &gateway_pb.CallObjectResponse{}, nil
}

// CreateObject implements the CreateObject RPC (empty for now)
func (s *gatewayServer) CreateObject(ctx context.Context, req *gateway_pb.CreateObjectRequest) (*gateway_pb.CreateObjectResponse, error) {
	log.Println("CreateObject called (not implemented)")
	return &gateway_pb.CreateObjectResponse{}, nil
}

// DeleteObject implements the DeleteObject RPC (empty for now)
func (s *gatewayServer) DeleteObject(ctx context.Context, req *gateway_pb.DeleteObjectRequest) (*gateway_pb.DeleteObjectResponse, error) {
	log.Println("DeleteObject called (not implemented)")
	return &gateway_pb.DeleteObjectResponse{}, nil
}

func serveGRPC(addr string, shutdownChan chan struct{}) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	g := grpc.NewServer()
	gateway_pb.RegisterGatewayServiceServer(g, &gatewayServer{})
	reflection.Register(g)
	log.Printf("Gateway gRPC server listening on %s", addr)

	// Handle graceful shutdown
	go func() {
		<-shutdownChan
		log.Println("Shutting down gRPC server...")
		g.GracefulStop()
	}()

	if err := g.Serve(l); err != nil {
		return err
	}
	return nil
}

func main() {
	// Create shutdown channel
	shutdownChan := make(chan struct{})

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start gRPC server
	serverDone := make(chan struct{})
	go func() {
		if err := serveGRPC(":8082", shutdownChan); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
		log.Println("gRPC server stopped")
		serverDone <- struct{}{}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal")
	close(shutdownChan)

	// Wait for server to stop with timeout
	select {
	case <-serverDone:
		log.Println("Server shutdown complete")
	case <-time.After(5 * time.Second):
		log.Println("Timeout waiting for server to shutdown")
	}

	log.Println("Gateway stopped")
}
