package serverconfig

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoaderWithCLIFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-listen", ":50000",
		"-advertise", "myhost:50000",
		"-etcd", "etcd.local:2379",
		"-etcd-prefix", "/myapp",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ListenAddress != ":50000" {
		t.Errorf("expected ListenAddress :50000, got %s", cfg.ListenAddress)
	}
	if cfg.AdvertiseAddress != "myhost:50000" {
		t.Errorf("expected AdvertiseAddress myhost:50000, got %s", cfg.AdvertiseAddress)
	}
	if cfg.EtcdAddress != "etcd.local:2379" {
		t.Errorf("expected EtcdAddress etcd.local:2379, got %s", cfg.EtcdAddress)
	}
	if cfg.EtcdPrefix != "/myapp" {
		t.Errorf("expected EtcdPrefix /myapp, got %s", cfg.EtcdPrefix)
	}

	// In CLI-only mode, node-id should default to advertise address
	if loader.GetNodeID() != "myhost:50000" {
		t.Errorf("expected NodeID myhost:50000, got %s", loader.GetNodeID())
	}
}

func TestLoaderWithDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	cfg, err := loader.Load([]string{})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ListenAddress != DefaultListenAddr {
		t.Errorf("expected ListenAddress %s, got %s", DefaultListenAddr, cfg.ListenAddress)
	}
	if cfg.AdvertiseAddress != DefaultAdvertiseAddr {
		t.Errorf("expected AdvertiseAddress %s, got %s", DefaultAdvertiseAddr, cfg.AdvertiseAddress)
	}
	if cfg.EtcdAddress != DefaultEtcdAddr {
		t.Errorf("expected EtcdAddress %s, got %s", DefaultEtcdAddr, cfg.EtcdAddress)
	}
	if cfg.EtcdPrefix != DefaultEtcdPrefix {
		t.Errorf("expected EtcdPrefix %s, got %s", DefaultEtcdPrefix, cfg.EtcdPrefix)
	}

	// In CLI-only mode with defaults, node-id should default to advertise address
	if loader.GetNodeID() != DefaultAdvertiseAddr {
		t.Errorf("expected NodeID %s, got %s", DefaultAdvertiseAddr, loader.GetNodeID())
	}
}

func TestLoaderWithExplicitNodeID(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-node-id", "my-custom-node",
		"-advertise", "myhost:50000",
	}

	_, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Explicit node-id should be used
	if loader.GetNodeID() != "my-custom-node" {
		t.Errorf("expected NodeID my-custom-node, got %s", loader.GetNodeID())
	}
}

func TestLoaderWithConfigFile(t *testing.T) {
	// Create a temp config file
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
    advertise_addr: "gate-1.local:10001"
    http_addr: "0.0.0.0:8081"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ListenAddress != "0.0.0.0:9101" {
		t.Errorf("expected ListenAddress 0.0.0.0:9101, got %s", cfg.ListenAddress)
	}
	if cfg.AdvertiseAddress != "node-1.local:9101" {
		t.Errorf("expected AdvertiseAddress node-1.local:9101, got %s", cfg.AdvertiseAddress)
	}
	if cfg.MetricsListenAddress != "0.0.0.0:8101" {
		t.Errorf("expected MetricsListenAddress 0.0.0.0:8101, got %s", cfg.MetricsListenAddress)
	}
	if cfg.EtcdAddress != "etcd-cluster:2379" {
		t.Errorf("expected EtcdAddress etcd-cluster:2379, got %s", cfg.EtcdAddress)
	}
	if cfg.EtcdPrefix != "/goverse-test" {
		t.Errorf("expected EtcdPrefix /goverse-test, got %s", cfg.EtcdPrefix)
	}
	if cfg.NumShards != 4096 {
		t.Errorf("expected NumShards 4096, got %d", cfg.NumShards)
	}
}

func TestLoaderConfigFileWithCLIOverridesForbidden(t *testing.T) {
	// Create a temp config file
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
    advertise_addr: "gate-1.local:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "listen flag with config",
			args:    []string{"-config", configPath, "-node-id", "node-1", "-listen", ":60000"},
			wantErr: "--listen cannot be used with --config",
		},
		{
			name:    "advertise flag with config",
			args:    []string{"-config", configPath, "-node-id", "node-1", "-advertise", "override.local:60000"},
			wantErr: "--advertise cannot be used with --config",
		},
		{
			name:    "http-listen flag with config",
			args:    []string{"-config", configPath, "-node-id", "node-1", "-http-listen", ":8080"},
			wantErr: "--http-listen cannot be used with --config",
		},
		{
			name:    "etcd flag with config",
			args:    []string{"-config", configPath, "-node-id", "node-1", "-etcd", "other:2379"},
			wantErr: "--etcd cannot be used with --config",
		},
		{
			name:    "etcd-prefix flag with config",
			args:    []string{"-config", configPath, "-node-id", "node-1", "-etcd-prefix", "/other"},
			wantErr: "--etcd-prefix cannot be used with --config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			loader := NewLoader(fs)

			_, err := loader.Load(tt.args)
			if err == nil {
				t.Fatalf("expected error but got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestLoaderMissingNodeID(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		// Missing -node-id
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error for missing node-id")
	}
}

func TestLoaderInvalidNodeID(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "nonexistent-node",
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error for invalid node-id")
	}
}

func TestLoaderWithInspectorAddress(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-inspector-address", "inspector.local:8081",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.InspectorAddress != "inspector.local:8081" {
		t.Errorf("expected InspectorAddress inspector.local:8081, got %s", cfg.InspectorAddress)
	}
}

func TestLoaderWithConfigFileAndInspector(t *testing.T) {
	// Create a temp config file with inspector section
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"

inspector:
  grpc_addr: "10.0.3.10:8081"
  http_addr: "10.0.3.10:8080"
  advertise_addr: "inspector.cluster.example.com:8081"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.InspectorAddress != "inspector.cluster.example.com:8081" {
		t.Errorf("expected InspectorAddress inspector.cluster.example.com:8081, got %s", cfg.InspectorAddress)
	}
}

func TestLoaderInspectorFlagWithConfigForbidden(t *testing.T) {
	// Create a temp config file
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
		"-inspector-address", "override:8081",
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
	if !strings.Contains(err.Error(), "--inspector-address cannot be used with --config") {
		t.Errorf("expected error containing '--inspector-address cannot be used with --config', got %q", err.Error())
	}
}

func TestLoaderWithClusterStateStabilityDuration(t *testing.T) {
	// Create a temp config file with cluster_state_stability_duration
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"
  cluster_state_stability_duration: 15s

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify cluster_state_stability_duration is passed through correctly
	expectedDuration := 15 * time.Second
	if cfg.NodeStabilityDuration != expectedDuration {
		t.Errorf("expected NodeStabilityDuration %v, got %v", expectedDuration, cfg.NodeStabilityDuration)
	}
}

func TestLoaderWithoutClusterStateStabilityDuration(t *testing.T) {
	// Create a temp config file without cluster_state_stability_duration
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "etcd-cluster:2379"
    prefix: "/goverse-test"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:9101"
    advertise_addr: "node-1.local:9101"
    http_addr: "0.0.0.0:8101"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// When not specified, NodeStabilityDuration should be 0 (will use default in cluster.Config)
	if cfg.NodeStabilityDuration != 0 {
		t.Errorf("expected NodeStabilityDuration 0 (default), got %v", cfg.NodeStabilityDuration)
	}
}

func TestLoaderWithAutoLoadObjects(t *testing.T) {
	// Create a temp config file with auto_load_objects
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"
  
  auto_load_objects:
    - type: "ChatRoomMgr"
      id: "ChatRoomMgr0"
    - type: "GameManager"
      id: "GameManager-Global"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:50051"
    advertise_addr: "localhost:50051"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify auto_load_objects is passed through correctly
	if len(cfg.AutoLoadObjects) != 2 {
		t.Fatalf("expected 2 auto-load objects, got %d", len(cfg.AutoLoadObjects))
	}

	// Check first object
	if cfg.AutoLoadObjects[0].Type != "ChatRoomMgr" {
		t.Errorf("expected first object type ChatRoomMgr, got %s", cfg.AutoLoadObjects[0].Type)
	}
	if cfg.AutoLoadObjects[0].ID != "ChatRoomMgr0" {
		t.Errorf("expected first object ID ChatRoomMgr0, got %s", cfg.AutoLoadObjects[0].ID)
	}

	// Check second object
	if cfg.AutoLoadObjects[1].Type != "GameManager" {
		t.Errorf("expected second object type GameManager, got %s", cfg.AutoLoadObjects[1].Type)
	}
	if cfg.AutoLoadObjects[1].ID != "GameManager-Global" {
		t.Errorf("expected second object ID GameManager-Global, got %s", cfg.AutoLoadObjects[1].ID)
	}
}

func TestLoaderWithPerNodeAutoLoadObjects(t *testing.T) {
	// Create a temp config file with both cluster and node-specific auto_load_objects
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"
  
  auto_load_objects:
    - type: "ClusterManager"
      id: "GlobalClusterMgr"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:50051"
    advertise_addr: "localhost:50051"
    auto_load_objects:
      - type: "NodeService"
        id: "Node1Service"
      - type: "NodeCache"
        id: "Node1Cache"
        per_shard: true

  - id: "node-2"
    grpc_addr: "0.0.0.0:50052"
    advertise_addr: "localhost:50052"
    auto_load_objects:
      - type: "NodeService"
        id: "Node2Service"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Test node-1
	fs1 := flag.NewFlagSet("test1", flag.ContinueOnError)
	loader1 := NewLoader(fs1)
	args1 := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}
	cfg1, err := loader1.Load(args1)
	if err != nil {
		t.Fatalf("Load failed for node-1: %v", err)
	}

	// Verify node-1 gets cluster + node-specific auto-load objects
	if len(cfg1.AutoLoadObjects) != 3 {
		t.Fatalf("expected 3 auto-load objects for node-1, got %d", len(cfg1.AutoLoadObjects))
	}
	if cfg1.AutoLoadObjects[0].Type != "ClusterManager" {
		t.Errorf("expected first object type ClusterManager, got %s", cfg1.AutoLoadObjects[0].Type)
	}
	if cfg1.AutoLoadObjects[1].Type != "NodeService" {
		t.Errorf("expected second object type NodeService, got %s", cfg1.AutoLoadObjects[1].Type)
	}
	if cfg1.AutoLoadObjects[1].ID != "Node1Service" {
		t.Errorf("expected Node1Service, got %s", cfg1.AutoLoadObjects[1].ID)
	}
	if cfg1.AutoLoadObjects[2].Type != "NodeCache" {
		t.Errorf("expected third object type NodeCache, got %s", cfg1.AutoLoadObjects[2].Type)
	}
	if !cfg1.AutoLoadObjects[2].PerShard {
		t.Error("expected Node1Cache to be per-shard")
	}

	// Test node-2
	fs2 := flag.NewFlagSet("test2", flag.ContinueOnError)
	loader2 := NewLoader(fs2)
	args2 := []string{
		"-config", configPath,
		"-node-id", "node-2",
	}
	cfg2, err := loader2.Load(args2)
	if err != nil {
		t.Fatalf("Load failed for node-2: %v", err)
	}

	// Verify node-2 gets cluster + its own node-specific auto-load objects
	if len(cfg2.AutoLoadObjects) != 2 {
		t.Fatalf("expected 2 auto-load objects for node-2, got %d", len(cfg2.AutoLoadObjects))
	}
	if cfg2.AutoLoadObjects[0].Type != "ClusterManager" {
		t.Errorf("expected first object type ClusterManager, got %s", cfg2.AutoLoadObjects[0].Type)
	}
	if cfg2.AutoLoadObjects[1].Type != "NodeService" {
		t.Errorf("expected second object type NodeService, got %s", cfg2.AutoLoadObjects[1].Type)
	}
	if cfg2.AutoLoadObjects[1].ID != "Node2Service" {
		t.Errorf("expected Node2Service, got %s", cfg2.AutoLoadObjects[1].ID)
	}
}

func TestLoaderWithoutAutoLoadObjects(t *testing.T) {
	// Create a temp config file without auto_load_objects
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

nodes:
  - id: "node-1"
    grpc_addr: "0.0.0.0:50051"
    advertise_addr: "localhost:50051"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-node-id", "node-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// When not specified, AutoLoadObjects should be empty
	if len(cfg.AutoLoadObjects) != 0 {
		t.Errorf("expected 0 auto-load objects, got %d", len(cfg.AutoLoadObjects))
	}
}
