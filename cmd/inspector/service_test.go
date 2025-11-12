package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaonanln/goverse/cmd/inspector/graph"
	"github.com/xiaonanln/goverse/inspector"
	inspector_pb "github.com/xiaonanln/goverse/inspector/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestInspectorService_Ping(t *testing.T) {
	pg := graph.NewGoverseGraph()
	svc := inspector.NewService(pg)

	resp, err := svc.Ping(context.Background(), &inspector_pb.Empty{})
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	if resp == nil {
		t.Error("Ping() response should not be nil")
	}
}

func TestInspectorService_AddOrUpdateObject(t *testing.T) {
	tests := []struct {
		name         string
		req          *inspector_pb.AddOrUpdateObjectRequest
		registerNode bool
		wantErr      bool
		wantCode     codes.Code
		wantCount    int
		skipVerify   bool
	}{
		{
			name: "add valid object with registered node",
			req: &inspector_pb.AddOrUpdateObjectRequest{
				Object: &inspector_pb.Object{
					Id:    "obj1",
					Class: "TestClass",
				},
				NodeAddress: "node1",
			},
			registerNode: true,
			wantErr:      false,
			wantCount:    1,
		},
		{
			name: "add object with nil object and registered node",
			req: &inspector_pb.AddOrUpdateObjectRequest{
				Object:      nil,
				NodeAddress: "node1",
			},
			registerNode: true,
			wantErr:      false,
			wantCount:    0,
			skipVerify:   true,
		},
		{
			name: "add object with empty ID and registered node",
			req: &inspector_pb.AddOrUpdateObjectRequest{
				Object: &inspector_pb.Object{
					Id:    "",
					Class: "TestClass",
				},
				NodeAddress: "node1",
			},
			registerNode: true,
			wantErr:      false,
			wantCount:    0,
			skipVerify:   true,
		},
		{
			name: "update existing object with registered node",
			req: &inspector_pb.AddOrUpdateObjectRequest{
				Object: &inspector_pb.Object{
					Id:    "obj1",
					Class: "UpdatedClass",
				},
				NodeAddress: "node2",
			},
			registerNode: true,
			wantErr:      false,
			wantCount:    1,
		},
		{
			name: "add object with unregistered node",
			req: &inspector_pb.AddOrUpdateObjectRequest{
				Object: &inspector_pb.Object{
					Id:    "obj1",
					Class: "TestClass",
				},
				NodeAddress: "unregistered-node",
			},
			registerNode: false,
			wantErr:      true,
			wantCode:     codes.NotFound,
			skipVerify:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := graph.NewGoverseGraph()
			svc := inspector.NewService(pg)

			// Register node if needed
			if tt.registerNode {
				node := GoverseNode{
					ID:            tt.req.NodeAddress,
					Label:         "Test Node",
					AdvertiseAddr: tt.req.NodeAddress,
				}
				pg.AddOrUpdateNode(node)
			}

			resp, err := svc.AddOrUpdateObject(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddOrUpdateObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				// Verify error code
				st, ok := status.FromError(err)
				if !ok {
					t.Errorf("Expected gRPC status error, got %v", err)
					return
				}
				if st.Code() != tt.wantCode {
					t.Errorf("Expected error code %v, got %v", tt.wantCode, st.Code())
				}
			} else {
				if resp == nil {
					t.Error("AddOrUpdateObject() response should not be nil")
				}
			}

			if !tt.skipVerify {
				objects := pg.GetObjects()
				if len(objects) != tt.wantCount {
					t.Errorf("Expected %d objects, got %d", tt.wantCount, len(objects))
				}

				if tt.wantCount > 0 && objects[0].ID != tt.req.Object.Id {
					t.Errorf("Expected object ID %s, got %s", tt.req.Object.Id, objects[0].ID)
				}
			}
		})
	}
}

func TestInspectorService_RegisterNode(t *testing.T) {
	tests := []struct {
		name          string
		req           *inspector_pb.RegisterNodeRequest
		wantErr       bool
		wantNodeCount int
		wantObjCount  int
	}{
		{
			name: "register node with objects",
			req: &inspector_pb.RegisterNodeRequest{
				AdvertiseAddress: "localhost:47000",
				Objects: []*inspector_pb.Object{
					{Id: "obj1", Class: "Class1"},
					{Id: "obj2", Class: "Class2"},
				},
			},
			wantErr:       false,
			wantNodeCount: 1,
			wantObjCount:  2,
		},
		{
			name: "register node without objects",
			req: &inspector_pb.RegisterNodeRequest{
				AdvertiseAddress: "localhost:47001",
				Objects:          []*inspector_pb.Object{},
			},
			wantErr:       false,
			wantNodeCount: 1,
			wantObjCount:  0,
		},
		{
			name: "register node with empty address",
			req: &inspector_pb.RegisterNodeRequest{
				AdvertiseAddress: "",
				Objects:          []*inspector_pb.Object{},
			},
			wantErr:       true,
			wantNodeCount: 0,
			wantObjCount:  0,
		},
		{
			name: "register node with nil objects",
			req: &inspector_pb.RegisterNodeRequest{
				AdvertiseAddress: "localhost:47002",
				Objects: []*inspector_pb.Object{
					nil,
					{Id: "obj3", Class: "Class3"},
					{Id: "", Class: "EmptyID"},
				},
			},
			wantErr:       false,
			wantNodeCount: 1,
			wantObjCount:  1, // Only obj3 should be added
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pg := graph.NewGoverseGraph()
			svc := inspector.NewService(pg)

			resp, err := svc.RegisterNode(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegisterNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && resp == nil {
				t.Error("RegisterNode() response should not be nil")
			}

			nodes := pg.GetNodes()
			if len(nodes) != tt.wantNodeCount {
				t.Errorf("Expected %d nodes, got %d", tt.wantNodeCount, len(nodes))
			}

			objects := pg.GetObjects()
			if len(objects) != tt.wantObjCount {
				t.Errorf("Expected %d objects, got %d", tt.wantObjCount, len(objects))
			}
		})
	}
}

func TestInspectorService_RegisterNode_RemoveStale(t *testing.T) {
	pg := graph.NewGoverseGraph()
	svc := inspector.NewService(pg)

	// First registration with obj1 and obj2
	_, err := svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects: []*inspector_pb.Object{
			{Id: "obj1", Class: "Class1"},
			{Id: "obj2", Class: "Class2"},
		},
	})
	if err != nil {
		t.Fatalf("First RegisterNode() error = %v", err)
	}

	objects := pg.GetObjects()
	if len(objects) != 2 {
		t.Fatalf("Expected 2 objects after first registration, got %d", len(objects))
	}

	// Second registration with only obj1 - obj2 should be removed as stale
	_, err = svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects: []*inspector_pb.Object{
			{Id: "obj1", Class: "Class1"},
		},
	})
	if err != nil {
		t.Fatalf("Second RegisterNode() error = %v", err)
	}

	objects = pg.GetObjects()
	if len(objects) != 1 {
		t.Errorf("Expected 1 object after removing stale, got %d", len(objects))
	}

	if len(objects) > 0 && objects[0].ID != "obj1" {
		t.Errorf("Expected remaining object to be obj1, got %s", objects[0].ID)
	}
}

func TestInspectorService_UnregisterNode(t *testing.T) {
	pg := graph.NewGoverseGraph()
	svc := inspector.NewService(pg)

	// Register a node first
	_, err := svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
		Objects: []*inspector_pb.Object{
			{Id: "obj1", Class: "Class1"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}

	// Verify node and object exist
	if len(pg.GetNodes()) != 1 {
		t.Fatal("Node should exist after registration")
	}
	if len(pg.GetObjects()) != 1 {
		t.Fatal("Object should exist after registration")
	}

	// Unregister the node
	resp, err := svc.UnregisterNode(context.Background(), &inspector_pb.UnregisterNodeRequest{
		AdvertiseAddress: "localhost:47000",
	})
	if err != nil {
		t.Fatalf("UnregisterNode() error = %v", err)
	}
	if resp == nil {
		t.Error("UnregisterNode() response should not be nil")
	}

	// Verify node is removed
	nodes := pg.GetNodes()
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes after unregistration, got %d", len(nodes))
	}

	// Verify objects are also removed (cascade)
	objects := pg.GetObjects()
	if len(objects) != 0 {
		t.Errorf("Expected 0 objects after node unregistration, got %d", len(objects))
	}
}

func TestCreateHTTPHandler(t *testing.T) {
	pg := graph.NewGoverseGraph()
	handler := CreateHTTPHandler(pg, "inspector/web")

	if handler == nil {
		t.Fatal("CreateHTTPHandler() should not return nil")
	}

	t.Run("graph endpoint returns empty data", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/graph", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}

		contentType := w.Header().Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		var result struct {
			GoverseNodes   []interface{} `json:"goverse_nodes"`
			GoverseObjects []interface{} `json:"goverse_objects"`
		}

		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(result.GoverseNodes) != 0 {
			t.Errorf("Expected 0 nodes, got %d", len(result.GoverseNodes))
		}

		if len(result.GoverseObjects) != 0 {
			t.Errorf("Expected 0 objects, got %d", len(result.GoverseObjects))
		}
	})

	t.Run("graph endpoint returns populated data", func(t *testing.T) {
		// Add a node
		svc := inspector.NewService(pg)
		_, err := svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
			AdvertiseAddress: "localhost:47000",
			Objects: []*inspector_pb.Object{
				{Id: "obj1", Class: "Class1"},
			},
		})
		if err != nil {
			t.Fatalf("RegisterNode() error = %v", err)
		}

		req := httptest.NewRequest("GET", "/graph", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status OK, got %d", w.Code)
		}

		var result struct {
			GoverseNodes   []interface{} `json:"goverse_nodes"`
			GoverseObjects []interface{} `json:"goverse_objects"`
		}

		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if len(result.GoverseNodes) != 1 {
			t.Errorf("Expected 1 node, got %d", len(result.GoverseNodes))
		}

		if len(result.GoverseObjects) != 1 {
			t.Errorf("Expected 1 object, got %d", len(result.GoverseObjects))
		}
	})
}

func TestInspectorService_Integration(t *testing.T) {
	// Integration test covering multiple operations
	pg := graph.NewGoverseGraph()
	svc := inspector.NewService(pg)

	// Test ping
	_, err := svc.Ping(context.Background(), &inspector_pb.Empty{})
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	// Register first node
	_, err = svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "node1:47000",
		Objects: []*inspector_pb.Object{
			{Id: "obj1", Class: "Class1"},
			{Id: "obj2", Class: "Class2"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterNode(node1) error = %v", err)
	}

	// Register second node
	_, err = svc.RegisterNode(context.Background(), &inspector_pb.RegisterNodeRequest{
		AdvertiseAddress: "node2:47000",
		Objects: []*inspector_pb.Object{
			{Id: "obj3", Class: "Class3"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterNode(node2) error = %v", err)
	}

	// Verify state
	if len(pg.GetNodes()) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(pg.GetNodes()))
	}
	if len(pg.GetObjects()) != 3 {
		t.Errorf("Expected 3 objects, got %d", len(pg.GetObjects()))
	}

	// Add object to node1
	_, err = svc.AddOrUpdateObject(context.Background(), &inspector_pb.AddOrUpdateObjectRequest{
		Object: &inspector_pb.Object{
			Id:    "obj4",
			Class: "Class4",
		},
		NodeAddress: "node1:47000",
	})
	if err != nil {
		t.Fatalf("AddOrUpdateObject() error = %v", err)
	}

	if len(pg.GetObjects()) != 4 {
		t.Errorf("Expected 4 objects after adding, got %d", len(pg.GetObjects()))
	}

	// Unregister node1
	_, err = svc.UnregisterNode(context.Background(), &inspector_pb.UnregisterNodeRequest{
		AdvertiseAddress: "node1:47000",
	})
	if err != nil {
		t.Fatalf("UnregisterNode(node1) error = %v", err)
	}

	// Verify node1 and its objects are removed
	if len(pg.GetNodes()) != 1 {
		t.Errorf("Expected 1 node after unregistration, got %d", len(pg.GetNodes()))
	}

	// Only obj3 from node2 should remain
	objects := pg.GetObjects()
	if len(objects) != 1 {
		t.Errorf("Expected 1 object after node1 removal, got %d", len(objects))
	}
	if len(objects) > 0 && objects[0].ID != "obj3" {
		t.Errorf("Expected remaining object to be obj3, got %s", objects[0].ID)
	}
}
