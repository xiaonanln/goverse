package callcontext

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

const grpcBufSize = 1 << 20 // 1 MB in-memory buffer

// newBufconnPair starts an in-memory gRPC server that calls ExtractCallerFromIncoming
// on every request and captures the resulting CallerIdentity. Returns a client
// connection and a pointer-to-pointer that is populated after each RPC.
func newBufconnPair(t *testing.T) (*grpc.ClientConn, **CallerIdentity) {
	t.Helper()

	lis := bufconn.Listen(grpcBufSize)

	var captured *CallerIdentity

	svc := &grpc.ServiceDesc{
		ServiceName: "callcontext.test.Capture",
		HandlerType: (*interface{})(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Ping",
			Handler: func(_ interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
				var req emptypb.Empty
				if err := dec(&req); err != nil {
					return nil, err
				}
				ctx = ExtractCallerFromIncoming(ctx)
				captured = GetCallerIdentity(ctx)
				return &emptypb.Empty{}, nil
			},
		}},
		Streams: []grpc.StreamDesc{},
	}

	srv := grpc.NewServer()
	srv.RegisterService(svc, struct{}{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn, &captured
}

// ping makes a real gRPC call to the capture server.
func ping(ctx context.Context, conn *grpc.ClientConn) error {
	return conn.Invoke(ctx, "/callcontext.test.Capture/Ping", &emptypb.Empty{}, &emptypb.Empty{})
}

// TestGRPC_InjectExtract_UserIDAndRoles verifies that InjectCallerToOutgoing +
// real gRPC transport + ExtractCallerFromIncoming preserves the full CallerIdentity.
func TestGRPC_InjectExtract_UserIDAndRoles(t *testing.T) {
	conn, captured := newBufconnPair(t)

	original := &CallerIdentity{UserID: "alice", Roles: []string{"admin", "editor"}}
	ctx := InjectCallerToOutgoing(WithCallerIdentity(context.Background(), original))

	if err := ping(ctx, conn); err != nil {
		t.Fatalf("RPC failed: %v", err)
	}

	id := *captured
	if id == nil {
		t.Fatal("Expected CallerIdentity on server side, got nil")
	}
	if id.UserID != original.UserID {
		t.Errorf("UserID: want %q, got %q", original.UserID, id.UserID)
	}
	if len(id.Roles) != len(original.Roles) {
		t.Fatalf("Roles len: want %d, got %d", len(original.Roles), len(id.Roles))
	}
	for i, r := range original.Roles {
		if id.Roles[i] != r {
			t.Errorf("Roles[%d]: want %q, got %q", i, r, id.Roles[i])
		}
	}
}

// TestGRPC_InjectExtract_UserIDOnly verifies that a CallerIdentity with no roles
// arrives with an empty Roles slice (not nil causing a panic).
func TestGRPC_InjectExtract_UserIDOnly(t *testing.T) {
	conn, captured := newBufconnPair(t)

	ctx := InjectCallerToOutgoing(WithCallerIdentity(context.Background(), &CallerIdentity{UserID: "bob"}))
	if err := ping(ctx, conn); err != nil {
		t.Fatalf("RPC failed: %v", err)
	}

	id := *captured
	if id == nil {
		t.Fatal("Expected CallerIdentity on server side, got nil")
	}
	if id.UserID != "bob" {
		t.Errorf("UserID: want %q, got %q", "bob", id.UserID)
	}
	if len(id.Roles) != 0 {
		t.Errorf("Expected no roles, got %v", id.Roles)
	}
}

// TestGRPC_NoIdentity_NotPropagated verifies that a context with no CallerIdentity
// results in nil on the server side — nothing is fabricated in transit.
func TestGRPC_NoIdentity_NotPropagated(t *testing.T) {
	conn, captured := newBufconnPair(t)

	ctx := InjectCallerToOutgoing(context.Background()) // no identity
	if err := ping(ctx, conn); err != nil {
		t.Fatalf("RPC failed: %v", err)
	}

	if id := *captured; id != nil {
		t.Errorf("Expected nil CallerIdentity on server side, got %+v", id)
	}
}

// TestGRPC_MultiHop simulates a two-hop propagation: gate→nodeA→nodeB.
// Each hop: inject on outgoing, extract on incoming, re-inject on the next outgoing.
func TestGRPC_MultiHop(t *testing.T) {
	// Hop 1: simulate gate injecting and nodeA extracting + re-injecting
	conn, captured := newBufconnPair(t)

	original := &CallerIdentity{UserID: "carol", Roles: []string{"viewer"}}

	// Hop 1: gate side — inject into outgoing
	hopCtx := InjectCallerToOutgoing(WithCallerIdentity(context.Background(), original))
	if err := ping(hopCtx, conn); err != nil {
		t.Fatalf("Hop 1 RPC failed: %v", err)
	}
	nodeAIdentity := *captured
	if nodeAIdentity == nil {
		t.Fatal("Hop 1: expected CallerIdentity on server, got nil")
	}

	// Hop 2: nodeA side — re-inject the restored identity into a new outgoing context
	conn2, captured2 := newBufconnPair(t)
	hop2Ctx := InjectCallerToOutgoing(WithCallerIdentity(context.Background(), nodeAIdentity))
	if err := ping(hop2Ctx, conn2); err != nil {
		t.Fatalf("Hop 2 RPC failed: %v", err)
	}
	nodeBIdentity := *captured2
	if nodeBIdentity == nil {
		t.Fatal("Hop 2: expected CallerIdentity on server, got nil")
	}

	if nodeBIdentity.UserID != original.UserID {
		t.Errorf("Hop 2 UserID: want %q, got %q", original.UserID, nodeBIdentity.UserID)
	}
	if len(nodeBIdentity.Roles) != 1 || nodeBIdentity.Roles[0] != "viewer" {
		t.Errorf("Hop 2 Roles: want [viewer], got %v", nodeBIdentity.Roles)
	}
}
