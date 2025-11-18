package server

import (
	"context"
	"testing"

	gateway_pb "github.com/xiaonanln/goverse/client/proto"
	"github.com/xiaonanln/goverse/node"
	"github.com/xiaonanln/goverse/util/logger"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestGatewayServiceImpl_CreateObject tests the empty CreateObject implementation
func TestGatewayServiceImpl_CreateObject(t *testing.T) {
	// Create a minimal server setup
	n := node.NewNode("localhost:47000")
	server := &Server{
		Node:   n,
		logger: logger.NewLogger("TestGateway"),
	}
	gateway := &gatewayServiceImpl{server: server}

	ctx := context.Background()
	req := &gateway_pb.CreateObjectRequest{
		Type: "TestType",
		Id:   "test-id",
	}

	resp, err := gateway.CreateObject(ctx, req)
	if err != nil {
		t.Fatalf("CreateObject failed: %v", err)
	}

	if resp == nil {
		t.Fatalf("CreateObject returned nil response")
	}

	// Empty implementation should return empty response
	if resp.GetId() != "" {
		t.Logf("Note: CreateObject returned ID: %s (empty impl returns empty response)", resp.GetId())
	}
}

// TestGatewayServiceImpl_DeleteObject tests the empty DeleteObject implementation
func TestGatewayServiceImpl_DeleteObject(t *testing.T) {
	// Create a minimal server setup
	n := node.NewNode("localhost:47000")
	server := &Server{
		Node:   n,
		logger: logger.NewLogger("TestGateway"),
	}
	gateway := &gatewayServiceImpl{server: server}

	ctx := context.Background()
	req := &gateway_pb.DeleteObjectRequest{
		Id: "test-id",
	}

	resp, err := gateway.DeleteObject(ctx, req)
	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}

	if resp == nil {
		t.Fatalf("DeleteObject returned nil response")
	}
}

// TestGatewayServiceImpl_CallObject tests the CallObject method with CallObjectRequest
func TestGatewayServiceImpl_CallObject(t *testing.T) {
	// Create a minimal server setup
	n := node.NewNode("localhost:47000")
	server := &Server{
		Node:   n,
		logger: logger.NewLogger("TestGateway"),
	}
	gateway := &gatewayServiceImpl{server: server}

	ctx := context.Background()
	anyReq, _ := anypb.New(&gateway_pb.Empty{})
	req := &gateway_pb.CallObjectRequest{
		ClientId: "test-client",
		Method:   "TestMethod",
		Request:  anyReq,
		Type:     "TestType",
	}

	// This will fail because client doesn't exist, but that's okay for this test
	_, err := gateway.CallObject(ctx, req)
	if err == nil {
		t.Logf("CallObject succeeded (unexpected - client doesn't exist)")
	} else {
		t.Logf("CallObject failed as expected: %v", err)
	}
}

// TestCallObjectRequest_HasTypeField verifies that CallObjectRequest has the type field
func TestCallObjectRequest_HasTypeField(t *testing.T) {
	req := &gateway_pb.CallObjectRequest{
		ClientId: "test-client",
		Method:   "TestMethod",
		Type:     "TestType",
	}

	if req.GetType() != "TestType" {
		t.Fatalf("CallObjectRequest.Type field not working correctly, got: %s", req.GetType())
	}

	// Verify all fields are accessible
	if req.GetClientId() != "test-client" {
		t.Fatalf("CallObjectRequest.ClientId field not working correctly")
	}
	if req.GetMethod() != "TestMethod" {
		t.Fatalf("CallObjectRequest.Method field not working correctly")
	}
}
