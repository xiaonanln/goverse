package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaonanln/goverse/config"
)

func TestGenerateConfig(t *testing.T) {
	tests := []struct {
		name          string
		numNodes      int
		numGates      int
		withInspector bool
		wantErr       bool
	}{
		{
			name:          "single node with gate and inspector",
			numNodes:      1,
			numGates:      1,
			withInspector: true,
			wantErr:       false,
		},
		{
			name:          "multiple nodes and gates",
			numNodes:      3,
			numGates:      2,
			withInspector: true,
			wantErr:       false,
		},
		{
			name:          "no gates",
			numNodes:      2,
			numGates:      0,
			withInspector: false,
			wantErr:       false,
		},
		{
			name:          "many nodes",
			numNodes:      5,
			numGates:      3,
			withInspector: true,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary file
			tmpfile := filepath.Join(t.TempDir(), "test-config.yml")

			// Generate config
			err := generateConfig(tmpfile, tt.numNodes, tt.numGates, tt.withInspector)
			if (err != nil) != tt.wantErr {
				t.Fatalf("generateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			// Verify file was created
			if _, err := os.Stat(tmpfile); os.IsNotExist(err) {
				t.Fatalf("config file was not created")
			}

			// Verify config can be loaded and is valid
			cfg, err := config.LoadConfig(tmpfile)
			if err != nil {
				t.Fatalf("failed to load generated config: %v", err)
			}

			// Verify basic properties
			if cfg.Version != 1 {
				t.Errorf("expected version 1, got %d", cfg.Version)
			}

			if cfg.Cluster.Shards != defaultShards {
				t.Errorf("expected %d shards, got %d", defaultShards, cfg.Cluster.Shards)
			}

			if cfg.Cluster.Provider != "etcd" {
				t.Errorf("expected provider 'etcd', got '%s'", cfg.Cluster.Provider)
			}

			if len(cfg.Nodes) != tt.numNodes {
				t.Errorf("expected %d nodes, got %d", tt.numNodes, len(cfg.Nodes))
			}

			if len(cfg.Gates) != tt.numGates {
				t.Errorf("expected %d gates, got %d", tt.numGates, len(cfg.Gates))
			}

			// Verify inspector configuration
			if tt.withInspector {
				if cfg.Inspector.AdvertiseAddr == "" {
					t.Error("expected inspector to be configured, but advertise_addr is empty")
				}
			}

			// Verify node IDs are unique and properly formatted
			nodeIDs := make(map[string]bool)
			for i, node := range cfg.Nodes {
				expectedID := nodeID(i + 1)
				if node.ID != expectedID {
					t.Errorf("node %d: expected ID '%s', got '%s'", i, expectedID, node.ID)
				}
				if nodeIDs[node.ID] {
					t.Errorf("duplicate node ID: %s", node.ID)
				}
				nodeIDs[node.ID] = true

				// Verify required fields are set
				if node.GRPCAddr == "" {
					t.Errorf("node %s: grpc_addr is empty", node.ID)
				}
				if node.AdvertiseAddr == "" {
					t.Errorf("node %s: advertise_addr is empty", node.ID)
				}
			}

			// Verify gate IDs are unique and properly formatted
			gateIDs := make(map[string]bool)
			for i, gate := range cfg.Gates {
				expectedID := gateID(i + 1)
				if gate.ID != expectedID {
					t.Errorf("gate %d: expected ID '%s', got '%s'", i, expectedID, gate.ID)
				}
				if gateIDs[gate.ID] {
					t.Errorf("duplicate gate ID: %s", gate.ID)
				}
				gateIDs[gate.ID] = true

				// Verify required fields are set
				if gate.GRPCAddr == "" {
					t.Errorf("gate %s: grpc_addr is empty", gate.ID)
				}
			}
		})
	}
}

func TestGenerateConfigFileCreationError(t *testing.T) {
	// Try to create a file in a non-existent directory
	err := generateConfig("/nonexistent/path/config.yml", 1, 1, true)
	if err == nil {
		t.Fatal("expected error when creating file in non-existent directory")
	}
}

// Helper functions matching the pattern used in main.go
func nodeID(i int) string {
	return fmt.Sprintf("node-%d", i)
}

func gateID(i int) string {
	return fmt.Sprintf("gate-%d", i)
}
