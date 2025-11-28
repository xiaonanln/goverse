package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ClusterConfig holds cluster-level configuration
type ClusterConfig struct {
	Shards   int        `yaml:"shards"`
	Provider string     `yaml:"provider"`
	Etcd     EtcdConfig `yaml:"etcd"`
}

// EtcdConfig holds etcd-specific configuration
type EtcdConfig struct {
	Endpoints []string `yaml:"endpoints"`
	Prefix    string   `yaml:"prefix"`
}

// NodeConfig holds configuration for a single node
type NodeConfig struct {
	ID            string `yaml:"id"`
	GRPCAddr      string `yaml:"grpc_addr"`
	AdvertiseAddr string `yaml:"advertise_addr"`
	HTTPAddr      string `yaml:"http_addr"`
}

// GateConfig holds configuration for a single gate
type GateConfig struct {
	ID       string `yaml:"id"`
	GRPCAddr string `yaml:"grpc_addr"`
}

// Config is the root configuration structure
type Config struct {
	Version int           `yaml:"version"`
	Cluster ClusterConfig `yaml:"cluster"`
	Nodes   []NodeConfig  `yaml:"nodes"`
	Gates   []GateConfig  `yaml:"gates"`
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version: %d (expected 1)", c.Version)
	}

	if c.Cluster.Provider == "" {
		return fmt.Errorf("cluster provider is required")
	}

	if c.Cluster.Provider != "etcd" {
		return fmt.Errorf("unsupported cluster provider: %s (only 'etcd' is supported)", c.Cluster.Provider)
	}

	if len(c.Cluster.Etcd.Endpoints) == 0 {
		return fmt.Errorf("at least one etcd endpoint is required")
	}

	if c.Cluster.Etcd.Prefix == "" {
		return fmt.Errorf("etcd prefix is required")
	}

	// Validate nodes
	nodeIDs := make(map[string]bool)
	for i, node := range c.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node %d: id is required", i)
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node id: %s", node.ID)
		}
		nodeIDs[node.ID] = true

		if node.GRPCAddr == "" {
			return fmt.Errorf("node %s: grpc_addr is required", node.ID)
		}
		if node.AdvertiseAddr == "" {
			return fmt.Errorf("node %s: advertise_addr is required", node.ID)
		}
	}

	// Validate gates
	gateIDs := make(map[string]bool)
	for i, gate := range c.Gates {
		if gate.ID == "" {
			return fmt.Errorf("gate %d: id is required", i)
		}
		if gateIDs[gate.ID] {
			return fmt.Errorf("duplicate gate id: %s", gate.ID)
		}
		gateIDs[gate.ID] = true

		if gate.GRPCAddr == "" {
			return fmt.Errorf("gate %s: grpc_addr is required", gate.ID)
		}
	}

	return nil
}

// GetNodeByID finds a node configuration by its ID
func (c *Config) GetNodeByID(id string) (*NodeConfig, error) {
	for i := range c.Nodes {
		if c.Nodes[i].ID == id {
			return &c.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("node with id %q not found", id)
}

// GetGateByID finds a gate configuration by its ID
func (c *Config) GetGateByID(id string) (*GateConfig, error) {
	for i := range c.Gates {
		if c.Gates[i].ID == id {
			return &c.Gates[i], nil
		}
	}
	return nil, fmt.Errorf("gate with id %q not found", id)
}

// GetEtcdAddress returns the first etcd endpoint address
func (c *Config) GetEtcdAddress() string {
	if len(c.Cluster.Etcd.Endpoints) > 0 {
		return c.Cluster.Etcd.Endpoints[0]
	}
	return ""
}

// GetEtcdPrefix returns the etcd prefix
func (c *Config) GetEtcdPrefix() string {
	return c.Cluster.Etcd.Prefix
}

// GetNumShards returns the number of shards, defaulting to 8192 if not specified
func (c *Config) GetNumShards() int {
	if c.Cluster.Shards <= 0 {
		return 8192
	}
	return c.Cluster.Shards
}
