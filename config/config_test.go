package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "127.0.0.1:2379"
    prefix: "/goverse"

postgres:
  host: "localhost"
  port: 5432
  user: "goverse"
  password: "goverse"
  database: "goverse"
  sslmode: "disable"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"

  - id: "node-2"
    grpc_addr: "0.0.0.0:9102"
    advertise_addr: "node-2.local:9102"
    http_addr: "0.0.0.0:8102"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	if cfg.Cluster.Shards != 8192 {
		t.Errorf("expected 8192 shards, got %d", cfg.Cluster.Shards)
	}

	if cfg.Cluster.Provider != "etcd" {
		t.Errorf("expected provider etcd, got %s", cfg.Cluster.Provider)
	}

	if len(cfg.Cluster.Etcd.Endpoints) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(cfg.Cluster.Etcd.Endpoints))
	}

	if cfg.Cluster.Etcd.Prefix != "/goverse" {
		t.Errorf("expected prefix /goverse, got %s", cfg.Cluster.Etcd.Prefix)
	}

	// Validate postgres config
	if cfg.Postgres.Host != "localhost" {
		t.Errorf("expected postgres host localhost, got %s", cfg.Postgres.Host)
	}
	if cfg.Postgres.Port != 5432 {
		t.Errorf("expected postgres port 5432, got %d", cfg.Postgres.Port)
	}
	if cfg.Postgres.User != "goverse" {
		t.Errorf("expected postgres user goverse, got %s", cfg.Postgres.User)
	}
	if cfg.Postgres.Password != "goverse" {
		t.Errorf("expected postgres password goverse, got %s", cfg.Postgres.Password)
	}
	if cfg.Postgres.Database != "goverse" {
		t.Errorf("expected postgres database goverse, got %s", cfg.Postgres.Database)
	}
	if cfg.Postgres.SSLMode != "disable" {
		t.Errorf("expected postgres sslmode disable, got %s", cfg.Postgres.SSLMode)
	}

	if len(cfg.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(cfg.Nodes))
	}

	if len(cfg.Gates) != 1 {
		t.Errorf("expected 1 gate, got %d", len(cfg.Gates))
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yml")
	if err := os.WriteFile(configPath, []byte("invalid: [yaml: content"), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "unsupported version",
			config:  Config{Version: 2},
			wantErr: true,
			errMsg:  "unsupported config version",
		},
		{
			name: "missing provider",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{},
			},
			wantErr: true,
			errMsg:  "cluster provider is required",
		},
		{
			name: "unsupported provider",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{Provider: "consul", Shards: 8192},
			},
			wantErr: true,
			errMsg:  "unsupported cluster provider",
		},
		{
			name: "missing shards",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
			},
			wantErr: true,
			errMsg:  "cluster shards must be specified",
		},
		{
			name: "zero shards",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   0,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
			},
			wantErr: true,
			errMsg:  "cluster shards must be specified",
		},
		{
			name: "negative shards",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   -1,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
			},
			wantErr: true,
			errMsg:  "cluster shards must be specified",
		},
		{
			name: "missing etcd endpoints",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Prefix: "/goverse"},
				},
			},
			wantErr: true,
			errMsg:  "at least one etcd endpoint is required",
		},
		{
			name: "missing etcd prefix",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}},
				},
			},
			wantErr: true,
			errMsg:  "etcd prefix is required",
		},
		{
			name: "missing node id",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Nodes: []NodeConfig{{GRPCAddr: ":9000", AdvertiseAddr: "localhost:9000"}},
			},
			wantErr: true,
			errMsg:  "id is required",
		},
		{
			name: "duplicate node id",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Nodes: []NodeConfig{
					{ID: "node-1", GRPCAddr: ":9000", AdvertiseAddr: "localhost:9000"},
					{ID: "node-1", GRPCAddr: ":9001", AdvertiseAddr: "localhost:9001"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate node id",
		},
		{
			name: "missing node grpc_addr",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Nodes: []NodeConfig{{ID: "node-1", AdvertiseAddr: "localhost:9000"}},
			},
			wantErr: true,
			errMsg:  "grpc_addr is required",
		},
		{
			name: "missing node advertise_addr",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Nodes: []NodeConfig{{ID: "node-1", GRPCAddr: ":9000"}},
			},
			wantErr: true,
			errMsg:  "advertise_addr is required",
		},
		{
			name: "missing gate id",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Gates: []GateConfig{{GRPCAddr: ":10000"}},
			},
			wantErr: true,
			errMsg:  "id is required",
		},
		{
			name: "duplicate gate id",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Gates: []GateConfig{
					{ID: "gate-1", GRPCAddr: ":10000"},
					{ID: "gate-1", GRPCAddr: ":10001"},
				},
			},
			wantErr: true,
			errMsg:  "duplicate gate id",
		},
		{
			name: "missing gate grpc_addr",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Gates: []GateConfig{{ID: "gate-1"}},
			},
			wantErr: true,
			errMsg:  "grpc_addr is required",
		},
		{
			name: "valid config",
			config: Config{
				Version: 1,
				Cluster: ClusterConfig{
					Shards:   8192,
					Provider: "etcd",
					Etcd:     EtcdConfig{Endpoints: []string{"localhost:2379"}, Prefix: "/goverse"},
				},
				Nodes: []NodeConfig{
					{ID: "node-1", GRPCAddr: ":9000", AdvertiseAddr: "localhost:9000"},
				},
				Gates: []GateConfig{
					{ID: "gate-1", GRPCAddr: ":10000"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestGetNodeByID(t *testing.T) {
	cfg := &Config{
		Nodes: []NodeConfig{
			{ID: "node-1", GRPCAddr: ":9001", AdvertiseAddr: "localhost:9001"},
			{ID: "node-2", GRPCAddr: ":9002", AdvertiseAddr: "localhost:9002"},
		},
	}

	node, err := cfg.GetNodeByID("node-1")
	if err != nil {
		t.Fatalf("GetNodeByID failed: %v", err)
	}
	if node.ID != "node-1" {
		t.Errorf("expected node-1, got %s", node.ID)
	}
	if node.GRPCAddr != ":9001" {
		t.Errorf("expected :9001, got %s", node.GRPCAddr)
	}

	node, err = cfg.GetNodeByID("node-2")
	if err != nil {
		t.Fatalf("GetNodeByID failed: %v", err)
	}
	if node.ID != "node-2" {
		t.Errorf("expected node-2, got %s", node.ID)
	}

	_, err = cfg.GetNodeByID("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}
}

func TestGetGateByID(t *testing.T) {
	cfg := &Config{
		Gates: []GateConfig{
			{ID: "gate-1", GRPCAddr: ":10001"},
			{ID: "gate-2", GRPCAddr: ":10002"},
		},
	}

	gate, err := cfg.GetGateByID("gate-1")
	if err != nil {
		t.Fatalf("GetGateByID failed: %v", err)
	}
	if gate.ID != "gate-1" {
		t.Errorf("expected gate-1, got %s", gate.ID)
	}
	if gate.GRPCAddr != ":10001" {
		t.Errorf("expected :10001, got %s", gate.GRPCAddr)
	}

	gate, err = cfg.GetGateByID("gate-2")
	if err != nil {
		t.Fatalf("GetGateByID failed: %v", err)
	}
	if gate.ID != "gate-2" {
		t.Errorf("expected gate-2, got %s", gate.ID)
	}

	_, err = cfg.GetGateByID("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent gate")
	}
}

func TestGetEtcdAddress(t *testing.T) {
	cfg := &Config{
		Cluster: ClusterConfig{
			Etcd: EtcdConfig{
				Endpoints: []string{"localhost:2379", "localhost:2380"},
			},
		},
	}

	if addr := cfg.GetEtcdAddress(); addr != "localhost:2379" {
		t.Errorf("expected localhost:2379, got %s", addr)
	}

	cfg.Cluster.Etcd.Endpoints = nil
	if addr := cfg.GetEtcdAddress(); addr != "" {
		t.Errorf("expected empty string, got %s", addr)
	}
}

func TestGetNumShards(t *testing.T) {
	cfg := &Config{
		Cluster: ClusterConfig{Shards: 4096},
	}

	if shards := cfg.GetNumShards(); shards != 4096 {
		t.Errorf("expected 4096, got %d", shards)
	}

	cfg.Cluster.Shards = 8192
	if shards := cfg.GetNumShards(); shards != 8192 {
		t.Errorf("expected 8192, got %d", shards)
	}
}

func TestLoadExampleConfigs(t *testing.T) {
	tests := []struct {
		name               string
		file               string
		expectedNodes      int
		expectedGates      int
		expectedShards     int
		expectedRulesCount int // Access rules count, 0 if none
	}{
		{
			name:               "single-node",
			file:               "examples/single-node.yaml",
			expectedNodes:      1,
			expectedGates:      0,
			expectedShards:     8192,
			expectedRulesCount: 0,
		},
		{
			name:               "multi-node",
			file:               "examples/multi-node.yaml",
			expectedNodes:      2,
			expectedGates:      1,
			expectedShards:     8192,
			expectedRulesCount: 0,
		},
		{
			name:               "multi-node-with-access-control",
			file:               "examples/multi-node-with-access-control.yaml",
			expectedNodes:      2,
			expectedGates:      1,
			expectedShards:     8192,
			expectedRulesCount: 8, // 8 access rules in the example
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(tt.file)
			if err != nil {
				t.Fatalf("LoadConfig(%s) failed: %v", tt.file, err)
			}

			if len(cfg.Nodes) != tt.expectedNodes {
				t.Errorf("expected %d nodes, got %d", tt.expectedNodes, len(cfg.Nodes))
			}

			if len(cfg.Gates) != tt.expectedGates {
				t.Errorf("expected %d gates, got %d", tt.expectedGates, len(cfg.Gates))
			}

			if cfg.Cluster.Shards != tt.expectedShards {
				t.Errorf("expected %d shards, got %d", tt.expectedShards, cfg.Cluster.Shards)
			}

			if cfg.Version != 1 {
				t.Errorf("expected version 1, got %d", cfg.Version)
			}

			if cfg.Cluster.Provider != "etcd" {
				t.Errorf("expected provider etcd, got %s", cfg.Cluster.Provider)
			}

			if len(cfg.AccessRules) != tt.expectedRulesCount {
				t.Errorf("expected %d access rules, got %d", tt.expectedRulesCount, len(cfg.AccessRules))
			}

			// If access rules are present, verify we can create a validator
			if tt.expectedRulesCount > 0 {
				v, err := cfg.NewAccessValidator()
				if err != nil {
					t.Errorf("failed to create access validator: %v", err)
				}
				if v == nil {
					t.Error("expected non-nil access validator for config with access rules")
				}
			}
		})
	}
}

func TestInspectorConfig(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "127.0.0.1:2379"
    prefix: "/goverse"

inspector:
  grpc_addr: "127.0.0.1:8081"
  http_addr: "127.0.0.1:8080"
  connect_addr: "localhost:8081"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Validate inspector configuration
	if cfg.Inspector.GRPCAddr != "127.0.0.1:8081" {
		t.Errorf("expected inspector grpc_addr 127.0.0.1:8081, got %s", cfg.Inspector.GRPCAddr)
	}
	if cfg.Inspector.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("expected inspector http_addr 127.0.0.1:8080, got %s", cfg.Inspector.HTTPAddr)
	}
	if cfg.Inspector.ConnectAddr != "localhost:8081" {
		t.Errorf("expected inspector connect_addr localhost:8081, got %s", cfg.Inspector.ConnectAddr)
	}

	// Test helper method
	if addr := cfg.GetInspectorConnectAddress(); addr != "localhost:8081" {
		t.Errorf("expected GetInspectorConnectAddress() to return localhost:8081, got %s", addr)
	}
}

func TestInspectorConfigOptional(t *testing.T) {
	// Test that inspector configuration is optional
	configContent := `
version: 1

cluster:
  shards: 8192
  provider: "etcd"
  etcd:
    endpoints:
      - "127.0.0.1:2379"
    prefix: "/goverse"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Inspector fields should be empty
	if cfg.Inspector.GRPCAddr != "" {
		t.Errorf("expected empty inspector grpc_addr, got %s", cfg.Inspector.GRPCAddr)
	}
	if cfg.Inspector.HTTPAddr != "" {
		t.Errorf("expected empty inspector http_addr, got %s", cfg.Inspector.HTTPAddr)
	}
	if cfg.Inspector.ConnectAddr != "" {
		t.Errorf("expected empty inspector connect_addr, got %s", cfg.Inspector.ConnectAddr)
	}

	// GetInspectorConnectAddress should return empty string
	if addr := cfg.GetInspectorConnectAddress(); addr != "" {
		t.Errorf("expected GetInspectorConnectAddress() to return empty string, got %s", addr)
	}
}

func TestGetInspectorConnectAddress(t *testing.T) {
	tests := []struct {
		name         string
		inspectorCfg InspectorConfig
		expectedAddr string
	}{
		{
			name: "with connect address",
			inspectorCfg: InspectorConfig{
				GRPCAddr:    "127.0.0.1:8081",
				HTTPAddr:    "127.0.0.1:8080",
				ConnectAddr: "inspector.example.com:8081",
			},
			expectedAddr: "inspector.example.com:8081",
		},
		{
			name:         "empty inspector config",
			inspectorCfg: InspectorConfig{},
			expectedAddr: "",
		},
		{
			name: "with grpc and http but no connect",
			inspectorCfg: InspectorConfig{
				GRPCAddr: "127.0.0.1:8081",
				HTTPAddr: "127.0.0.1:8080",
			},
			expectedAddr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Inspector: tt.inspectorCfg,
			}

			addr := cfg.GetInspectorConnectAddress()
			if addr != tt.expectedAddr {
				t.Errorf("expected %q, got %q", tt.expectedAddr, addr)
			}
		})
	}
}
