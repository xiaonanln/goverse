package server

import (
	"context"
	"testing"

	"github.com/xiaonanln/goverse/node"
	goverse_pb "github.com/xiaonanln/goverse/proto"
	"github.com/xiaonanln/goverse/util/logger"
	"github.com/xiaonanln/goverse/util/protohelper"
	"github.com/xiaonanln/goverse/util/testutil"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestReliableCallObject_ValidationErrors verifies that ReliableCallObject returns
// a valid response with SKIPPED status for validation errors instead of nil, err
func TestReliableCallObject_ValidationErrors(t *testing.T) {
	// This is a unit test for validation logic only - no etcd needed
	// Create a minimal server structure for testing validation
	nodeAddr := testutil.GetFreeAddress()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	n := node.NewNode(nodeAddr, testutil.TestNumShards)

	server := &Server{
		Node:   n,
		ctx:    ctx,
		cancel: cancel,
		logger: logger.NewLogger("TestServer"),
		config: &ServerConfig{
			ListenAddress:    nodeAddr,
			AdvertiseAddress: nodeAddr,
			NumShards:        testutil.TestNumShards,
		},
	}

	// Create a test request Any
	testRequestAny, _ := protohelper.MsgToAny(&emptypb.Empty{})

	tests := []struct {
		name          string
		req           *goverse_pb.ReliableCallObjectRequest
		expectedError string
	}{
		{
			name: "missing call_id",
			req: &goverse_pb.ReliableCallObjectRequest{
				CallId:     "",
				ObjectType: "TestObject",
				ObjectId:   "test-id",
				MethodName: "TestMethod",
				Request:    testRequestAny,
			},
			expectedError: "call_id must be specified in ReliableCallObject request",
		},
		{
			name: "missing object_type",
			req: &goverse_pb.ReliableCallObjectRequest{
				CallId:     "test-call-id",
				ObjectType: "",
				ObjectId:   "test-id",
				MethodName: "TestMethod",
				Request:    testRequestAny,
			},
			expectedError: "object_type must be specified in ReliableCallObject request",
		},
		{
			name: "missing object_id",
			req: &goverse_pb.ReliableCallObjectRequest{
				CallId:     "test-call-id",
				ObjectType: "TestObject",
				ObjectId:   "",
				MethodName: "TestMethod",
				Request:    testRequestAny,
			},
			expectedError: "object_id must be specified in ReliableCallObject request",
		},
		{
			name: "missing method_name",
			req: &goverse_pb.ReliableCallObjectRequest{
				CallId:     "test-call-id",
				ObjectType: "TestObject",
				ObjectId:   "test-id",
				MethodName: "",
				Request:    testRequestAny,
			},
			expectedError: "method_name must be specified in ReliableCallObject request",
		},
		{
			name: "missing request",
			req: &goverse_pb.ReliableCallObjectRequest{
				CallId:     "test-call-id",
				ObjectType: "TestObject",
				ObjectId:   "test-id",
				MethodName: "TestMethod",
				Request:    nil,
			},
			expectedError: "request must be specified in ReliableCallObject request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := server.ReliableCallObject(ctx, tt.req)

			// Verify that err is nil (function should not return gRPC error)
			if err != nil {
				t.Fatalf("Expected nil gRPC error, got: %v", err)
			}

			// Verify that response is not nil
			if resp == nil {
				t.Fatal("Expected non-nil response")
			}

			// Verify status is SKIPPED
			if resp.Status != goverse_pb.ReliableCallStatus_SKIPPED {
				t.Fatalf("Expected status SKIPPED, got: %v", resp.Status)
			}

			// Verify error message
			if resp.Error != tt.expectedError {
				t.Fatalf("Expected error %q, got %q", tt.expectedError, resp.Error)
			}

			// Verify result is nil
			if resp.Result != nil {
				t.Fatalf("Expected nil result, got: %v", resp.Result)
			}
		})
	}
}
