package gateconfig

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	// In CLI-only mode, gate-id should default to advertise address
	if loader.GetGateID() != "myhost:50000" {
		t.Errorf("expected GateID myhost:50000, got %s", loader.GetGateID())
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

	// In CLI-only mode with defaults, gate-id should default to advertise address
	if loader.GetGateID() != DefaultAdvertiseAddr {
		t.Errorf("expected GateID %s, got %s", DefaultAdvertiseAddr, loader.GetGateID())
	}
}

func TestLoaderWithExplicitGateID(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-gate-id", "my-custom-gate",
		"-advertise", "myhost:50000",
	}

	_, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Explicit gate-id should be used
	if loader.GetGateID() != "my-custom-gate" {
		t.Errorf("expected GateID my-custom-gate, got %s", loader.GetGateID())
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
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-gate-id", "gate-1",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ListenAddress != "0.0.0.0:10001" {
		t.Errorf("expected ListenAddress 0.0.0.0:10001, got %s", cfg.ListenAddress)
	}
	if cfg.AdvertiseAddress != "gate-1.local:10001" {
		t.Errorf("expected AdvertiseAddress gate-1.local:10001, got %s", cfg.AdvertiseAddress)
	}
	if cfg.HTTPListenAddress != "0.0.0.0:8081" {
		t.Errorf("expected HTTPListenAddress 0.0.0.0:8081, got %s", cfg.HTTPListenAddress)
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
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "listen flag with config",
			args:    []string{"-config", configPath, "-gate-id", "gate-1", "-listen", ":60000"},
			wantErr: "--listen cannot be used with --config",
		},
		{
			name:    "advertise flag with config",
			args:    []string{"-config", configPath, "-gate-id", "gate-1", "-advertise", "override.local:60000"},
			wantErr: "--advertise cannot be used with --config",
		},
		{
			name:    "http-listen flag with config",
			args:    []string{"-config", configPath, "-gate-id", "gate-1", "-http-listen", ":8080"},
			wantErr: "--http-listen cannot be used with --config",
		},
		{
			name:    "etcd flag with config",
			args:    []string{"-config", configPath, "-gate-id", "gate-1", "-etcd", "other:2379"},
			wantErr: "--etcd cannot be used with --config",
		},
		{
			name:    "etcd-prefix flag with config",
			args:    []string{"-config", configPath, "-gate-id", "gate-1", "-etcd-prefix", "/other"},
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

func TestLoaderMissingGateID(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		// Missing -gate-id
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error for missing gate-id")
	}
}

func TestLoaderInvalidGateID(t *testing.T) {
	configContent := `
version: 1

cluster:
  shards: 4096
  provider: "etcd"
  etcd:
    endpoints:
      - "localhost:2379"
    prefix: "/goverse"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-gate-id", "nonexistent-gate",
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error for invalid gate-id")
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
  connect_addr: "inspector.cluster.example.com:8081"

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
    advertise_addr: "gate-1.local:10001"
    http_addr: "0.0.0.0:8081"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-gate-id", "gate-1",
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

gates:
  - id: "gate-1"
    grpc_addr: "0.0.0.0:10001"
    advertise_addr: "gate-1.local:10001"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	args := []string{
		"-config", configPath,
		"-gate-id", "gate-1",
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
