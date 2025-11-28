package gateconfig

import (
	"flag"
	"os"
	"path/filepath"
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

func TestLoaderConfigFileWithCLIOverrides(t *testing.T) {
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

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	loader := NewLoader(fs)

	// CLI flags should override config file values
	args := []string{
		"-config", configPath,
		"-gate-id", "gate-1",
		"-listen", ":60000",
		"-advertise", "override.local:60000",
	}

	cfg, err := loader.Load(args)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// These should be from CLI, not config file
	if cfg.ListenAddress != ":60000" {
		t.Errorf("expected ListenAddress :60000 (CLI override), got %s", cfg.ListenAddress)
	}
	if cfg.AdvertiseAddress != "override.local:60000" {
		t.Errorf("expected AdvertiseAddress override.local:60000 (CLI override), got %s", cfg.AdvertiseAddress)
	}
	// These should be from config file
	if cfg.EtcdAddress != "etcd-cluster:2379" {
		t.Errorf("expected EtcdAddress etcd-cluster:2379, got %s", cfg.EtcdAddress)
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
