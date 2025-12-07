// Package serverconfig handles command-line flags and config file loading
// for the node process, returning a ServerConfig.
package serverconfig

import (
	"flag"
	"fmt"
	"os"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/server"
)

const (
	DefaultListenAddr    = ":48000"
	DefaultAdvertiseAddr = "localhost:48000"
	DefaultEtcdAddr      = "localhost:2379"
	DefaultEtcdPrefix    = "/goverse"
	// Note: No default for inspector address - inspector is optional and defaults to disabled
)

// Loader handles parsing of command-line flags and config file loading.
// It can be instantiated with a custom FlagSet for testing.
type Loader struct {
	fs             *flag.FlagSet
	configPath     *string
	nodeID         *string
	listenAddr     *string
	advertiseAddr  *string
	httpListenAddr *string
	etcdAddr       *string
	etcdPrefix     *string
	inspectorAddr  *string
}

// NewLoader creates a new Loader with flags registered on the provided FlagSet.
// If fs is nil, the default flag.CommandLine is used.
func NewLoader(fs *flag.FlagSet) *Loader {
	if fs == nil {
		fs = flag.CommandLine
	}
	l := &Loader{fs: fs}
	l.configPath = fs.String("config", "", "Path to YAML config file")
	l.nodeID = fs.String("node-id", "", "Node ID (required if config is provided, defaults to advertise address otherwise)")
	l.listenAddr = fs.String("listen", DefaultListenAddr, "Node listen address (cannot be used with --config)")
	l.advertiseAddr = fs.String("advertise", DefaultAdvertiseAddr, "Node advertise address (cannot be used with --config)")
	l.httpListenAddr = fs.String("http-listen", "", "HTTP listen address for metrics (cannot be used with --config)")
	l.etcdAddr = fs.String("etcd", DefaultEtcdAddr, "Etcd address (cannot be used with --config)")
	l.etcdPrefix = fs.String("etcd-prefix", DefaultEtcdPrefix, "Etcd key prefix (cannot be used with --config)")
	l.inspectorAddr = fs.String("inspector-address", "", "Inspector service address (optional, cannot be used with --config)")
	return l
}

// Load parses the flags (if not already parsed) and returns a ServerConfig.
// When --config is provided, only --node-id is allowed; other flags are forbidden.
// When --config is not provided, CLI flags are used and --node-id defaults to advertise address.
// Returns an error if configuration is invalid.
func (l *Loader) Load(args []string) (*server.ServerConfig, error) {
	if !l.fs.Parsed() {
		if err := l.fs.Parse(args); err != nil {
			return nil, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	if *l.configPath != "" {
		// Config file mode: only --config and --node-id are allowed
		if *l.nodeID == "" {
			return nil, fmt.Errorf("--node-id is required when using --config")
		}

		// Check that other flags are not specified (still at their defaults)
		if *l.listenAddr != DefaultListenAddr {
			return nil, fmt.Errorf("--listen cannot be used with --config; configure in config file instead")
		}
		if *l.advertiseAddr != DefaultAdvertiseAddr {
			return nil, fmt.Errorf("--advertise cannot be used with --config; configure in config file instead")
		}
		if *l.httpListenAddr != "" {
			return nil, fmt.Errorf("--http-listen cannot be used with --config; configure in config file instead")
		}
		if *l.etcdAddr != DefaultEtcdAddr {
			return nil, fmt.Errorf("--etcd cannot be used with --config; configure in config file instead")
		}
		if *l.etcdPrefix != DefaultEtcdPrefix {
			return nil, fmt.Errorf("--etcd-prefix cannot be used with --config; configure in config file instead")
		}
		if *l.inspectorAddr != "" {
			return nil, fmt.Errorf("--inspector-address cannot be used with --config; configure in config file instead")
		}

		// Load config from file
		cfg, err := config.LoadConfig(*l.configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		nodeCfg, err := cfg.GetNodeByID(*l.nodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to find node config: %w", err)
		}

		listenAddr := nodeCfg.GRPCAddr
		advertiseAddr := nodeCfg.AdvertiseAddr
		if advertiseAddr == "" {
			advertiseAddr = nodeCfg.GRPCAddr
		}

		return &server.ServerConfig{
			ListenAddress:         listenAddr,
			AdvertiseAddress:      advertiseAddr,
			MetricsListenAddress:  nodeCfg.HTTPAddr,
			EtcdAddress:           cfg.GetEtcdAddress(),
			EtcdPrefix:            cfg.GetEtcdPrefix(),
			NumShards:             cfg.GetNumShards(),
			NodeStabilityDuration: cfg.GetClusterStateStabilityDuration(),
			InspectorAddress:      cfg.GetInspectorAdvertiseAddress(),
			AutoLoadObjects:       cfg.GetAutoLoadObjects(),
		}, nil
	}

	// CLI-only mode: use flag values
	// If node-id is not specified, use advertise address as default
	if *l.nodeID == "" {
		*l.nodeID = *l.advertiseAddr
	}

	return &server.ServerConfig{
		ListenAddress:        *l.listenAddr,
		AdvertiseAddress:     *l.advertiseAddr,
		MetricsListenAddress: *l.httpListenAddr,
		EtcdAddress:          *l.etcdAddr,
		EtcdPrefix:           *l.etcdPrefix,
		NumShards:            0, // Use default when not using config file
		InspectorAddress:     *l.inspectorAddr,
	}, nil
}

// GetNodeID returns the node ID after Load() has been called.
// In CLI-only mode, this defaults to the advertise address.
func (l *Loader) GetNodeID() string {
	return *l.nodeID
}

// Get is a convenience function that creates a Loader with default flags,
// parses os.Args[1:], and returns the ServerConfig.
// It panics on error.
func Get() *server.ServerConfig {
	loader := NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		panic(fmt.Sprintf("Failed to load server config: %v", err))
	}
	return cfg
}
