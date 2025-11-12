package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/inspector"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
)

func serveHTTP(pg *graph.GoverseGraph, addr string, shutdownChan chan struct{}) error {
	staticDir := "inspector/web"
	handler := CreateHTTPHandler(pg, staticDir)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("HTTP on %s (serving %s)", addr, filepath.Join(".", staticDir))

	// Handle graceful shutdown
	go func() {
		<-shutdownChan
		log.Println("Shutting down HTTP server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func serveGRPC(pg *graph.GoverseGraph, addr string, shutdownChan chan struct{}) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	g := grpc.NewServer()
	inspector_pb.RegisterInspectorServiceServer(g, inspector.NewService(pg))
	reflection.Register(g)
	log.Printf("gRPC on %s", addr)

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
	pg := graph.NewGoverseGraph()

	// Create shutdown channel
	shutdownChan := make(chan struct{})

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Track server completion
	serversDone := make(chan struct{}, 2)

	// Start gRPC server
	go func() {
		if err := serveGRPC(pg, ":8081", shutdownChan); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
		log.Println("gRPC server stopped")
		serversDone <- struct{}{}
	}()

	// Start HTTP server in a goroutine
	go func() {
		if err := serveHTTP(pg, ":8080", shutdownChan); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
		log.Println("HTTP server stopped")
		serversDone <- struct{}{}
	}()

	// Wait for shutdown signal
	<-sigChan
	log.Println("Received shutdown signal")
	close(shutdownChan)

	// Wait for both servers to stop with timeout
	timeout := time.After(5 * time.Second)
	serversShutdown := 0
	for serversShutdown < 2 {
		select {
		case <-serversDone:
			serversShutdown++
		case <-timeout:
			log.Println("Timeout waiting for servers to shutdown")
			return
		}
	}

	log.Println("Inspector stopped")
}
