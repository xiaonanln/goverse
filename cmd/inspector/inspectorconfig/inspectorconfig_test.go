package inspectorconfig

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
		"-grpc-addr", ":9081",
		"-http-addr", ":9080",
		"-static-dir", "/custom/web",
		"-etcd-addr", "etcd.local:2379",
		"-etcd-prefix", "/myapp",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.GRPCAddr != ":9081" {
		t.Errorf("expected GRPCAddr :9081, got %s", cfg.GRPCAddr)
	}
	if cfg.HTTPAddr != ":9080" {
		t.Errorf("expected HTTPAddr :9080, got %s", cfg.HTTPAddr)
	}
	if cfg.StaticDir != "/custom/web" {
		t.Errorf("expected StaticDir /custom/web, got %s", cfg.StaticDir)
	}
	if cfg.EtcdAddr != "etcd.local:2379" {
		t.Errorf("expected EtcdAddr etcd.local:2379, got %s", cfg.EtcdAddr)
	}
	if cfg.EtcdPrefix != "/myapp" {
		t.Errorf("expected EtcdPrefix /myapp, got %s", cfg.EtcdPrefix)
	}
}

func TestLoaderWithDefaults(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	cfg, err := loader.Load([]string{})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.GRPCAddr != DefaultGRPCAddr {
		t.Errorf("expected GRPCAddr %s, got %s", DefaultGRPCAddr, cfg.GRPCAddr)
	}
	if cfg.HTTPAddr != DefaultHTTPAddr {
		t.Errorf("expected HTTPAddr %s, got %s", DefaultHTTPAddr, cfg.HTTPAddr)
	}
	if cfg.StaticDir != DefaultStaticDir {
		t.Errorf("expected StaticDir %s, got %s", DefaultStaticDir, cfg.StaticDir)
	}
	if cfg.EtcdAddr != "" {
		t.Errorf("expected EtcdAddr empty, got %s", cfg.EtcdAddr)
	}
	if cfg.EtcdPrefix != DefaultEtcdPrefix {
		t.Errorf("expected EtcdPrefix %s, got %s", DefaultEtcdPrefix, cfg.EtcdPrefix)
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

inspector:
  grpc_addr: "10.0.3.10:8081"
  http_addr: "10.0.3.10:8080"
  advertise_addr: "inspector.cluster.example.com:8081"

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
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.GRPCAddr != "10.0.3.10:8081" {
		t.Errorf("expected GRPCAddr 10.0.3.10:8081, got %s", cfg.GRPCAddr)
	}
	if cfg.HTTPAddr != "10.0.3.10:8080" {
		t.Errorf("expected HTTPAddr 10.0.3.10:8080, got %s", cfg.HTTPAddr)
	}
	if cfg.StaticDir != DefaultStaticDir {
		t.Errorf("expected StaticDir %s, got %s", DefaultStaticDir, cfg.StaticDir)
	}
	if cfg.EtcdAddr != "etcd-cluster:2379" {
		t.Errorf("expected EtcdAddr etcd-cluster:2379, got %s", cfg.EtcdAddr)
	}
	if cfg.EtcdPrefix != "/goverse-test" {
		t.Errorf("expected EtcdPrefix /goverse-test, got %s", cfg.EtcdPrefix)
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

inspector:
  grpc_addr: "10.0.3.10:8081"
  http_addr: "10.0.3.10:8080"

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
			name:    "grpc-addr flag with config",
			args:    []string{"-config", configPath, "-grpc-addr", ":9999"},
			wantErr: "--grpc-addr cannot be used with --config",
		},
		{
			name:    "http-addr flag with config",
			args:    []string{"-config", configPath, "-http-addr", ":9998"},
			wantErr: "--http-addr cannot be used with --config",
		},
		{
			name:    "static-dir flag with config",
			args:    []string{"-config", configPath, "-static-dir", "/other/web"},
			wantErr: "--static-dir cannot be used with --config",
		},
		{
			name:    "etcd-addr flag with config",
			args:    []string{"-config", configPath, "-etcd-addr", "other:2379"},
			wantErr: "--etcd-addr cannot be used with --config",
		},
		{
			name:    "etcd-prefix flag with config",
			args:    []string{"-config", configPath, "-etcd-prefix", "/other"},
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

func TestLoaderMissingInspectorInConfig(t *testing.T) {
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
	}

	_, err := loader.Load(args)
	if err == nil {
		t.Fatal("expected error for missing inspector configuration")
	}
	if !strings.Contains(err.Error(), "inspector configuration is missing") {
		t.Errorf("expected error about missing inspector configuration, got %q", err.Error())
	}
}

func TestLoaderWithOptionalEtcdAddr(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	// Without etcd-addr flag (should use empty string)
	cfg, err := loader.Load([]string{})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.EtcdAddr != "" {
		t.Errorf("expected EtcdAddr empty when not specified, got %s", cfg.EtcdAddr)
	}
}
