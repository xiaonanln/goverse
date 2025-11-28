// Package gateconfig handles command-line flags and config file loading
// for the gate process, returning a GateServerConfig.
package gateconfig

import (
	"flag"
	"fmt"
	"os"

	"github.com/xiaonanln/goverse/config"
	"github.com/xiaonanln/goverse/gate/gateserver"
)

const (
	DefaultListenAddr    = ":49000"
	DefaultAdvertiseAddr = "localhost:49000"
	DefaultEtcdAddr      = "localhost:2379"
	DefaultEtcdPrefix    = "/goverse"
)

// Loader handles parsing of command-line flags and config file loading.
// It can be instantiated with a custom FlagSet for testing.
type Loader struct {
	fs             *flag.FlagSet
	configPath     *string
	gateID         *string
	listenAddr     *string
	advertiseAddr  *string
	httpListenAddr *string
	etcdAddr       *string
	etcdPrefix     *string
}

// NewLoader creates a new Loader with flags registered on the provided FlagSet.
// If fs is nil, the default flag.CommandLine is used.
func NewLoader(fs *flag.FlagSet) *Loader {
	if fs == nil {
		fs = flag.CommandLine
	}
	l := &Loader{fs: fs}
	l.configPath = fs.String("config", "", "Path to YAML config file")
	l.gateID = fs.String("gate-id", "", "Gate ID (required if config is provided, defaults to advertise address otherwise)")
	l.listenAddr = fs.String("listen", DefaultListenAddr, "Gate listen address (cannot be used with --config)")
	l.advertiseAddr = fs.String("advertise", DefaultAdvertiseAddr, "Gate advertise address (cannot be used with --config)")
	l.httpListenAddr = fs.String("http-listen", "", "HTTP listen address for REST API and metrics (cannot be used with --config)")
	l.etcdAddr = fs.String("etcd", DefaultEtcdAddr, "Etcd address (cannot be used with --config)")
	l.etcdPrefix = fs.String("etcd-prefix", DefaultEtcdPrefix, "Etcd key prefix (cannot be used with --config)")
	return l
}

// Load parses the flags (if not already parsed) and returns a GateServerConfig.
// When --config is provided, only --gate-id is allowed; other flags are forbidden.
// When --config is not provided, CLI flags are used and --gate-id defaults to advertise address.
// Returns an error if configuration is invalid.
func (l *Loader) Load(args []string) (*gateserver.GateServerConfig, error) {
	if !l.fs.Parsed() {
		if err := l.fs.Parse(args); err != nil {
			return nil, fmt.Errorf("failed to parse flags: %w", err)
		}
	}

	if *l.configPath != "" {
		// Config file mode: only --config and --gate-id are allowed
		if *l.gateID == "" {
			return nil, fmt.Errorf("--gate-id is required when using --config")
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

		// Load config from file
		cfg, err := config.LoadConfig(*l.configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		gateCfg, err := cfg.GetGateByID(*l.gateID)
		if err != nil {
			return nil, fmt.Errorf("failed to find gate config: %w", err)
		}

		listenAddr := gateCfg.GRPCAddr
		advertiseAddr := gateCfg.AdvertiseAddr
		if advertiseAddr == "" {
			advertiseAddr = gateCfg.GRPCAddr
		}

		return &gateserver.GateServerConfig{
			ListenAddress:     listenAddr,
			AdvertiseAddress:  advertiseAddr,
			HTTPListenAddress: gateCfg.HTTPAddr,
			EtcdAddress:       cfg.GetEtcdAddress(),
			EtcdPrefix:        cfg.GetEtcdPrefix(),
			NumShards:         cfg.GetNumShards(),
		}, nil
	}

	// CLI-only mode: use flag values
	// If gate-id is not specified, use advertise address as default
	if *l.gateID == "" {
		*l.gateID = *l.advertiseAddr
	}

	return &gateserver.GateServerConfig{
		ListenAddress:     *l.listenAddr,
		AdvertiseAddress:  *l.advertiseAddr,
		HTTPListenAddress: *l.httpListenAddr,
		EtcdAddress:       *l.etcdAddr,
		EtcdPrefix:        *l.etcdPrefix,
		NumShards:         0, // Use default when not using config file
	}, nil
}

// GetGateID returns the gate ID after Load() has been called.
// In CLI-only mode, this defaults to the advertise address.
func (l *Loader) GetGateID() string {
	return *l.gateID
}

// Get is a convenience function that creates a Loader with default flags,
// parses os.Args[1:], and returns the GateServerConfig.
// It panics on error.
func Get() *gateserver.GateServerConfig {
	loader := NewLoader(nil)
	cfg, err := loader.Load(os.Args[1:])
	if err != nil {
		panic(fmt.Sprintf("Failed to load gate config: %v", err))
	}
	return cfg
}
