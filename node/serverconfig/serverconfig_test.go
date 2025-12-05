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
