package server

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// serverGoverseService implements the GoverseService interface
// by delegating to the cluster's object operations.
// This is the in-process implementation used by nodes.
type serverGoverseService struct {
	server *Server
}

// newServerGoverseService creates a new serverGoverseService
func newServerGoverseService(server *Server) *serverGoverseService {
	return &serverGoverseService{
		server: server,
	}
}

// CallObject calls a method on a distributed object via the cluster
func (s *serverGoverseService) CallObject(ctx context.Context, objType, objectID, method string, request proto.Message) (proto.Message, error) {
	return s.server.cluster.CallObject(ctx, objType, objectID, method, request)
}

// CreateObject creates a new distributed object via the cluster
func (s *serverGoverseService) CreateObject(ctx context.Context, objType, objectID string) (string, error) {
	return s.server.cluster.CreateObject(ctx, objType, objectID)
}

// DeleteObject deletes a distributed object via the cluster
func (s *serverGoverseService) DeleteObject(ctx context.Context, objectID string) error {
	return s.server.cluster.DeleteObject(ctx, objectID)
}
